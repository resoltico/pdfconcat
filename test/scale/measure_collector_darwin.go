// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

type processSampler struct {
	collector string
	exit      processExitObserver
	pid       int
}

func prepareProcessSampler() (*processSampler, error) {
	sampler := &processSampler{}

	collector, err := exec.LookPath("lsof")
	if err != nil {
		return nil, fmt.Errorf("required descriptor collector lsof unavailable: %w", err)
	}

	sampler.collector = collector

	return sampler, nil
}

func (s *processSampler) attach(ctx context.Context, pid int) error {
	s.pid = pid
	observer, err := observeProcessExit(ctx, pid)
	s.exit = observer

	return err
}

func (s *processSampler) release() error { return s.exit.release() }

// sample requires a positive process-gone probe before classifying a failed query as terminal.
func (s *processSampler) sample(ctx context.Context) processSample {
	started := time.Now()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return collectorObservation(started, time.Now(), "", "", ctxErr.Error(), phaseLiveError)
	}

	proof, exited, probeErr := s.exit.confirm(ctx, s.pid)
	if probeErr != nil {
		return collectorObservation(started, time.Now(), "", proof, probeErr.Error(), phaseLiveError)
	}

	if exited {
		return collectorObservation(started, time.Now(), "", proof, "owned process exited before descriptor query", phaseTerminal)
	}

	output, err := exectest.Command(ctx, s.collector, "-p", strconv.Itoa(s.pid), "-Ff").Output()

	finished := time.Now()
	if err != nil {
		return s.failedCollectorRead(ctx, started, finished, output, err)
	}

	count, valid := collectorDescriptorCount(string(output), s.pid)
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

	if ctxErr := ctx.Err(); ctxErr != nil {
		return collectorObservation(started, finished, string(output), "", ctxErr.Error(), phaseLiveError)
	}

	proof, exited, probeErr = s.exit.confirm(ctx, s.pid)
	if probeErr != nil {
		return collectorObservation(started, finished, string(output), proof, probeErr.Error(), phaseLiveError)
	}

	if exited {
		return collectorObservation(
			started,
			finished,
			string(output),
			proof,
			"collector reply cannot be attributed after owned process exited",
			phaseTerminal,
		)
	}

	return processSample{descriptors: count, descriptorAt: finished}
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

// collectorDescriptorCount accepts one complete -Ff process record. Named mappings are not
// numbered descriptors; a well-formed record containing only mappings is a known zero reading.
func collectorDescriptorCount(output string, pid int) (int64, bool) {
	if !strings.HasSuffix(output, "\n") {
		return 0, false
	}

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "p"+strconv.Itoa(pid) {
		return 0, false
	}

	descriptors := make(map[uint64]struct{})

	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "f") || len(line) < 2 {
			return 0, false
		}

		field := line[1:]
		if collectorMapping(field) {
			continue
		}

		number, valid := unsignedDescriptor(field)
		if !valid {
			return 0, false
		}

		if _, duplicate := descriptors[number]; duplicate {
			return 0, false
		}

		descriptors[number] = struct{}{}
	}

	return int64(len(descriptors)), true
}

func collectorMapping(field string) bool {
	switch field {
	case "cwd", "txt", "mem", "rtd":
		return true
	default:
		return false
	}
}

func unsignedDescriptor(field string) (uint64, bool) {
	for _, digit := range field {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}

	number, err := strconv.ParseUint(field, 10, 64)

	return number, err == nil
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
	terminal := false

	if exited {
		failure.ExitCode = exit.ExitCode()

		failure.Stderr = string(exit.Stderr)
		terminal = s.confirmTerminalCollector(ctx, exit, &failure)
	}

	if terminal {
		failure.Phase = phaseTerminal
	}

	return processSample{descriptors: -1, terminal: terminal, failures: []ReadingFailure{failure}}
}

func (s *processSampler) confirmTerminalCollector(ctx context.Context, exit *exec.ExitError, failure *ReadingFailure) bool {
	if exit.ExitCode() != 1 || len(exit.Stderr) != 0 || failure.Stdout != "" || ctx.Err() != nil {
		return false
	}

	proof, exited, err := s.exit.confirm(ctx, s.pid)

	failure.Probe = proof
	if err != nil {
		failure.Cause = errors.Join(exit, err).Error()
		return false
	}

	return exited
}

func (s *processSampler) retryEvidence() ObserverRetryEvidence { return s.exit.retries }
