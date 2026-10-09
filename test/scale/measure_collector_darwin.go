// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

type (
	descriptorCollectorReply struct {
		count       int64
		nativeError uint64
		growth      uint64
	}
	processSampler struct {
		collector string
		directory string
		identity  *DescriptorCollectorIdentity
		exit      processExitObserver
		pid       int
	}
)

const (
	descriptorCollectorInitialSlots  = 16
	descriptorCollectorMaximumGrowth = 12
	descriptorCollectorMaximumSlots  = descriptorCollectorInitialSlots << descriptorCollectorMaximumGrowth
)

func prepareProcessSampler(ctx context.Context) (*processSampler, error) {
	return compileDescriptorCollector(ctx)
}

func (s *processSampler) attach(ctx context.Context, pid int) error {
	s.pid = pid
	observer, err := observeProcessExit(ctx, pid)
	s.exit = observer

	return err
}

func (s *processSampler) release() error {
	exitErr := s.exit.release()
	directory := s.directory

	s.directory = ""
	if directory == "" {
		return exitErr
	}

	return errors.Join(exitErr, os.RemoveAll(directory))
}

// sample requires a positive process-gone probe before classifying a failed query as terminal.
func (s *processSampler) sample(ctx context.Context) processSample {
	started := time.Now()

	proof, exited, probeErr := s.exit.confirm(ctx, s.pid)
	if probeErr != nil {
		return collectorObservation(started, time.Now(), "", proof, probeErr.Error(), phaseLiveError)
	}

	if exited {
		return collectorObservation(started, time.Now(), "", proof, "owned process unobservable before descriptor query", phaseTerminal)
	}

	output, stderr, err := s.queryDescriptorCollector(ctx)

	finished := time.Now()
	if err != nil {
		reading := s.failedCollectorRead(ctx, started, finished, output, err)
		reading.failures[0].Stderr = stderr

		return reading
	}

	if stderr != "" {
		reading := collectorObservation(
			started,
			finished,
			string(output),
			"",
			"descriptor collector wrote diagnostic stderr",
			phaseLiveError,
		)
		reading.failures[0].Stderr = stderr

		return reading
	}

	reply, valid := parseDescriptorCollectorReply(string(output), s.pid)
	if !valid {
		return collectorObservation(
			started,
			finished,
			string(output),
			"",
			"collector returned malformed descriptor protocol",
			phaseLiveError,
		)
	}

	return s.attributeDescriptorReply(ctx, started, finished, string(output), reply)
}

func (s *processSampler) attributeDescriptorReply(
	ctx context.Context, started, finished time.Time, output string, reply descriptorCollectorReply,
) processSample {
	if reply.nativeError != 0 {
		return s.failedNativeDescriptorRead(ctx, started, finished, output, reply.nativeError)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return collectorObservation(started, finished, output, "", ctxErr.Error(), phaseLiveError)
	}

	proof, exited, probeErr := s.exit.confirm(ctx, s.pid)
	if probeErr != nil {
		return collectorObservation(started, finished, output, proof, probeErr.Error(), phaseLiveError)
	}

	if exited {
		return collectorObservation(
			started,
			finished,
			output,
			proof,
			"collector reply cannot be attributed after owned process exited",
			phaseTerminal,
		)
	}

	return processSample{descriptors: reply.count, descriptorAt: finished}
}

func (s *processSampler) queryDescriptorCollector(ctx context.Context) ([]byte, string, error) {
	query := exectest.Command(ctx, s.collector, "-p", strconv.Itoa(s.pid))

	var stderr bytes.Buffer

	query.Stderr = &stderr
	output, err := query.Output()

	return output, stderr.String(), err
}

func collectorObservation(started, finished time.Time, stdout, proof, cause, phase string) processSample {
	failure := ReadingFailure{
		Metric:       metricDescriptors,
		Phase:        phase,
		Cause:        cause,
		Stdout:       stdout,
		Probe:        proof,
		ReadStarted:  started,
		ReadFinished: finished,
	}

	return processSample{descriptors: -1, terminal: phase == phaseTerminal, failures: []ReadingFailure{failure}}
}

// parseDescriptorCollectorReply accepts exactly one PID/count/native-errno/growth frame.
// An aligned libproc result is an observed lower bound, not a simultaneous inventory.
func parseDescriptorCollectorReply(output string, pid int) (descriptorCollectorReply, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) != 5 || lines[4] != "" || lines[0] != "p"+strconv.Itoa(pid) || pid <= 0 {
		return descriptorCollectorReply{}, false
	}

	count, countOK := collectorUnsignedField(lines[1], "c")
	nativeError, errorOK := collectorUnsignedField(lines[2], "e")

	growth, growthOK := collectorUnsignedField(lines[3], "g")
	if !countOK || count >= descriptorCollectorMaximumSlots || !errorOK || !growthOK {
		return descriptorCollectorReply{}, false
	}

	reply := descriptorCollectorReply{count: int64(count), nativeError: nativeError, growth: growth}

	if !reply.valid() {
		return descriptorCollectorReply{}, false
	}

	return reply, true
}

func (reply descriptorCollectorReply) valid() bool {
	return reply.nativeError <= 2147483647 && reply.growth <= descriptorCollectorMaximumGrowth &&
		reply.count < descriptorCollectorInitialSlots<<reply.growth && (reply.nativeError == 0 || reply.count == 0)
}

func collectorUnsignedField(line, prefix string) (uint64, bool) {
	field, found := strings.CutPrefix(line, prefix)
	if !found || field == "" {
		return 0, false
	}

	for _, digit := range field {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}

	number, err := strconv.ParseUint(field, 10, 64)

	return number, err == nil
}

func (s *processSampler) failedNativeDescriptorRead(
	ctx context.Context,
	started, finished time.Time,
	output string,
	nativeError uint64,
) processSample {
	proofStarted := time.Now()

	proof, exited, probeErr := s.exit.confirm(ctx, s.pid)
	if nativeError == uint64(unix.ESRCH) && !exited && probeErr == nil {
		proof, exited, probeErr = s.exit.awaitExitProof(ctx, s.pid)
	}

	proofFinished := time.Now()

	phase := phaseLiveError
	if nativeError == uint64(unix.ESRCH) && exited && probeErr == nil && ctx.Err() == nil {
		phase = phaseTerminal
	}

	reading := collectorObservation(started, finished, output, proof, fmt.Sprintf("native descriptor query errno %d", nativeError), phase)

	reading.failures[0].NativeCode = nativeError
	if nativeError == uint64(unix.ESRCH) {
		reading.failures[0].ProofWaitStarted = proofStarted
		reading.failures[0].ProofWaitFinished = proofFinished
	}

	if probeErr != nil {
		reading.failures[0].Cause = fmt.Sprintf("native descriptor query errno %d; %v", nativeError, probeErr)
	}

	return reading
}

func (s *processSampler) failedCollectorRead(ctx context.Context, started, finished time.Time, output []byte, err error) processSample {
	failure := ReadingFailure{
		Metric:       metricDescriptors,
		Phase:        phaseLiveError,
		Cause:        err.Error(),
		Stdout:       string(output),
		ReadStarted:  started,
		ReadFinished: finished,
	}
	exit, exited := errors.AsType[*exec.ExitError](err)

	if exited {
		failure.ExitCode = exit.ExitCode()

		failure.Stderr = string(exit.Stderr)
	}

	// A later owned exit records lifecycle information, but cannot establish why
	// this earlier query failed while the process was still observable.
	proof, _, probeErr := s.exit.confirm(ctx, s.pid)
	failure.Probe = proof

	if probeErr != nil {
		failure.Cause = errors.Join(err, probeErr).Error()
	}

	return processSample{descriptors: -1, failures: []ReadingFailure{failure}}
}

func (s *processSampler) retryEvidence() ObserverRetryEvidence { return s.exit.retries }

func (s *processSampler) collectorIdentity() *DescriptorCollectorIdentity { return s.identity }
