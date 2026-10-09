// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

type (
	// RunSpec describes one measured process execution.
	RunSpec struct {
		// Binary is the executable to run.
		Binary string
		// Dir is the working directory.
		Dir string
		// ScratchDir is a directory whose total file size is sampled while the process runs; the executable
		// puts its private workspace beside the output, so this is the output directory.
		ScratchDir string
		// Stdin, when not nil, is the process's standard input.
		Stdin io.Reader
		// Env is the process environment; nil inherits this process's.
		Env []string
		// OnSample, when not nil, is called from the sampling goroutine each time a reading of the running
		// process, and of ScratchDir, is complete. A caller that keeps the process alive until it has been
		// read counts the calls instead of guessing how long a reading takes.
		OnSample func()
		// Args are the command-line arguments after the executable's name.
		Args []string
		// DescriptorLimit, when not zero, caps the process's open descriptors on Unix, where exceeding it
		// makes its opens fail; Windows only samples handles.
		DescriptorLimit uint64
	}

	// Measurement is what one execution cost.
	Measurement struct {
		DescriptorCollector *DescriptorCollectorIdentity `json:"descriptor_collector,omitempty"`
		DescriptorCeiling   *DescriptorCeiling           `json:"descriptor_ceiling,omitempty"`
		ObserverRetries     ObserverRetryEvidence
		Stdout              string
		Stderr              string
		// RSSState is valid, unavailable, or error; an error includes any failed intermediate RSS sample.
		RSSState string
		// DescriptorState is valid only when every attempted live-process sample succeeds.
		DescriptorState string
		ReadingFailures []ReadingFailure
		Wall            time.Duration
		PeakRSSBytes    int64
		MaxDescriptors  int64
		// DescriptorSamples counts successful readings. SampleAttempts also includes failures.
		TerminalSamples           int64
		DescriptorTerminalSamples int64
		DescriptorSamples         int64
		SampleAttempts            int64
		// DescriptorCoverage is the observed temporal span divided by launch-to-exit wall time, not exhaustive coverage.
		DescriptorCoverage float64
		PeakScratchBytes   int64
		ExitCode           int
	}

	// ObserverRetryEvidence records bounded evidence of interrupted native exit-observer calls.
	ObserverRetryEvidence struct {
		First time.Time `json:"first,omitzero"`
		Last  time.Time `json:"last,omitzero"`
		Cause string    `json:"cause,omitempty"`
		Count int64     `json:"count"`
	}

	// ReadingFailure preserves the cause and phase of an unsuccessful process observation.
	ReadingFailure struct {
		ProofWaitStarted  time.Time `json:"proof_wait_started,omitzero"`
		ProofWaitFinished time.Time `json:"proof_wait_finished,omitzero"`
		ReadStarted       time.Time `json:"read_started"`
		ReadFinished      time.Time `json:"read_finished"`
		Metric            string    `json:"metric"`
		Phase             string    `json:"phase"`
		Cause             string    `json:"cause"`
		Stderr            string    `json:"stderr,omitempty"`
		Stdout            string    `json:"stdout,omitempty"`
		Probe             string    `json:"probe,omitempty"`
		ExitCode          int       `json:"exit_code"`
		NativeCode        uint64    `json:"native_code,omitempty"`
	}

	// processSample is one reading of a running process.
	processSample struct {
		descriptorAt time.Time
		failures     []ReadingFailure
		descriptors  int64 // -1 when unknown
		rssBytes     int64 // 0 when the platform reports peak memory only at exit
		terminal     bool
		rssError     bool
	}

	// processPeaks are the largest readings seen while a process ran.
	processPeaks struct {
		firstDescriptor           time.Time
		lastDescriptor            time.Time
		failures                  []ReadingFailure
		descriptorTerminalSamples int64
		terminalSamples           int64
		descriptors               int64
		rssBytes                  int64
		scratchBytes              int64
		sampleAttempts            int64
		descriptorSamples         int64
		rssErrors                 int64
	}

	// processWatch samples a running process until it is finished.
	processWatch struct {
		scratchErr error
		sampler    *processSampler
		stop       chan struct{}
		peaks      processPeaks
		running    sync.WaitGroup
	}
)

const (
	metricDescriptors = "descriptors"
	metricCollector   = "collector"
	metricScratch     = "scratch"
	phaseLiveError    = "live_error"
	phaseTerminal     = "terminal"
)

// Measure runs the command and measures it. A nonzero exit status is reported in the Measurement, not as
// an error. Returned errors identify process, scratch-inspection or collector-cleanup failures.
func Measure(ctx context.Context, spec RunSpec) (Measurement, error) {
	sampler, prepareErr := prepareProcessSampler(ctx)
	if prepareErr != nil {
		return unavailableMeasurement(), prepareErr
	}

	command := measuredCommand(ctx, spec)

	launch, launchErr := prepareDescriptorLaunch(ctx, command, spec.DescriptorLimit)
	if launchErr != nil {
		return unavailableMeasurement(), errors.Join(launchErr, sampler.release())
	}

	measured, err := measurePreparedCommand(ctx, spec, command, sampler, launch)

	return measured, errors.Join(err, sampler.release(), launch.unchangedBinary(), launch.release())
}

func measurePreparedCommand(
	ctx context.Context,
	spec RunSpec,
	command *exec.Cmd,
	sampler *processSampler,
	launch *descriptorLaunch,
) (Measurement, error) {
	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	started := time.Now()

	err := startProcess(command)
	if err != nil {
		return unavailableMeasurement(), fmt.Errorf("start %s: %w", spec.Binary, err)
	}

	ceiling, capErr := launch.observe(ctx, command.Process.Pid)
	if capErr != nil {
		killErr := command.Process.Kill()
		waitErr := command.Wait()
		measured := measurementFacts(command, sampler, processPeaks{}, time.Since(started), stdout.String(), stderr.String())

		return measured, errors.Join(capErr, killErr, waitErr, sampler.release())
	}

	if attachErr := sampler.attach(ctx, command.Process.Pid); attachErr != nil {
		killErr := command.Process.Kill()
		waitErr := command.Wait()

		measurement := measurementFacts(command, sampler, processPeaks{}, time.Since(started), stdout.String(), stderr.String())

		return measurement, errors.Join(attachErr, killErr, waitErr, sampler.release())
	}

	watch := watchProcess(ctx, sampler, spec.ScratchDir, spec.OnSample)
	waitErr := command.Wait()
	wall := time.Since(started)
	peaks, observerErr := watch.finish()

	exitCode, err := exitCodeOf(command, waitErr)

	measurement := measurementFacts(command, sampler, peaks, wall, stdout.String(), stderr.String())

	measurement.ExitCode = exitCode
	if ceiling.PID != 0 {
		measurement.DescriptorCeiling = &ceiling
	}

	if err != nil {
		return measurement, fmt.Errorf("wait for %s: %w", spec.Binary, errors.Join(err, observerErr))
	}

	return measurement, observerErr
}

// exitCodeOf returns the exit status of a waited command. Only a failure to wait is an error: a
// process that ran and exited nonzero is a result.
func exitCodeOf(command *exec.Cmd, waitErr error) (int, error) {
	code := -1
	if command.ProcessState != nil {
		code = command.ProcessState.ExitCode()
	}

	if waitErr == nil {
		return code, nil
	}

	exitErr, exited := errors.AsType[*exec.ExitError](waitErr)
	if exited && exitErr.ProcessState != nil {
		return code, nil
	}

	return code, waitErr
}

// watchProcess starts sampling the process and, when scratchDir is not empty, that directory's size.
// onSample, when not nil, is called after each complete sample.
func watchProcess(ctx context.Context, sampler *processSampler, scratchDir string, onSample func()) *processWatch {
	watch := &processWatch{stop: make(chan struct{}), sampler: sampler}

	watch.running.Go(func() {
		for {
			if !watch.sample(ctx, scratchDir, onSample) {
				return
			}

			select {
			case <-watch.stop:
				return
			case <-time.After(sampleInterval):
			}
		}
	})

	return watch
}

// finish stops sampling and returns the largest readings.
func (w *processWatch) finish() (processPeaks, error) {
	close(w.stop)
	w.running.Wait()

	releaseStarted := time.Now()

	closeErr := w.sampler.release()
	if closeErr != nil {
		w.peaks.failures = append(
			w.peaks.failures,
			ReadingFailure{
				Metric:       metricCollector,
				Phase:        "cleanup_error",
				Cause:        closeErr.Error(),
				ReadStarted:  releaseStarted,
				ReadFinished: time.Now(),
			},
		)
	}

	return w.peaks, errors.Join(w.scratchErr, closeErr)
}

// directoryBytes returns the total size of regular files under dir; files that vanish while walking
// (the executable deletes its workspace) are ignored.
func directoryBytes(dir string) (int64, error) {
	var total int64

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}

		if walkErr != nil {
			return fmt.Errorf("inspect scratch namespace %s: %w", path, walkErr)
		}

		if entry.IsDir() {
			return nil
		}

		info, infoErr := entry.Info()
		if errors.Is(infoErr, os.ErrNotExist) {
			return nil
		}

		if infoErr != nil {
			return fmt.Errorf("inspect scratch file %s: %w", path, infoErr)
		}

		if info.Mode().IsRegular() {
			total += info.Size()
		}

		return nil
	})
	if err != nil {
		return total, fmt.Errorf("walk scratch directory %s: %w", dir, err)
	}

	return total, nil
}

// FileSize returns the size in bytes of the file at path.
func FileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", path, err)
	}

	return info.Size(), nil
}

// WriteFile writes data to a new or existing private file at path.
func WriteFile(path string, data []byte) error {
	err := os.WriteFile(path, data, fileMode)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// DirectorySize totals the regular files under dir, for input and output byte accounting.
func DirectorySize(dir string) (int64, error) {
	var total int64

	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("inspect %s: %w", entry.Name(), infoErr)
		}

		total += info.Size()

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measure %s: %w", dir, err)
	}

	return total, nil
}

// record retains successful sample counts separately from attempts; missing samples invalidate acceptance.
func (p *processPeaks) record(sample processSample) {
	p.sampleAttempts++
	p.failures = append(p.failures, sample.failures...)
	p.descriptors = max(p.descriptors, sample.descriptors)

	p.rssBytes = max(p.rssBytes, sample.rssBytes)
	if sample.terminal {
		p.terminalSamples++
		if sample.descriptors < 0 {
			p.descriptorTerminalSamples++
		}
	} else if sample.rssError {
		p.rssErrors++
	}

	if sample.descriptors >= 0 {
		p.descriptorSamples++

		observed := sample.descriptorAt
		if observed.IsZero() {
			observed = time.Now()
		}

		if p.firstDescriptor.IsZero() {
			p.firstDescriptor = observed
		}

		p.lastDescriptor = observed
	}
}

func (p *processPeaks) states(rss int64) (string, string) {
	rssState := "valid"
	if p.rssErrors != 0 {
		rssState = "error"
	} else if rss <= 0 {
		rssState = "unavailable"
	}

	descriptorState := "valid"
	if p.descriptorSamples+p.descriptorTerminalSamples != p.sampleAttempts {
		descriptorState = "error"
	} else if p.descriptorSamples == 0 || p.descriptors <= 0 {
		descriptorState = "unavailable"
	}

	return rssState, descriptorState
}

func (w *processWatch) sample(ctx context.Context, scratchDir string, onSample func()) bool {
	sample := w.sampler.sample(ctx)
	w.peaks.record(sample)

	if scratchDir != "" && !w.sampleScratch(scratchDir) {
		return false
	}

	if sample.terminal {
		return false
	}

	if onSample != nil {
		onSample()
	}

	return true
}

// sampleScratch stops at the first inspection error; partial bytes remain a known lower bound,
// while finish returns the error so a completed executable cannot make this measurement acceptable.
func (w *processWatch) sampleScratch(dir string) bool {
	started := time.Now()
	total, err := directoryBytes(dir)
	w.peaks.scratchBytes = max(w.peaks.scratchBytes, total)

	if err == nil {
		return true
	}

	w.scratchErr = err
	w.peaks.failures = append(w.peaks.failures, ReadingFailure{
		Metric: metricScratch, Phase: phaseLiveError, Cause: err.Error(), ReadStarted: started, ReadFinished: time.Now(),
	})

	return false
}

func measuredCommand(ctx context.Context, spec RunSpec) *exec.Cmd {
	command := exectest.Command(ctx, spec.Binary, spec.Args...)
	if spec.Env != nil {
		command.Env = spec.Env
	}

	command.Dir = spec.Dir
	command.Stdin = spec.Stdin

	return command
}

func measurementFacts(
	command *exec.Cmd,
	sampler *processSampler,
	peaks processPeaks,
	wall time.Duration,
	stdout, stderr string,
) Measurement {
	rss := peaks.rssBytes
	if command.ProcessState != nil {
		rss = peakResidentBytes(command.ProcessState, peaks.rssBytes)
	}

	rssState, descriptorState := peaks.states(rss)

	coverage := max(0, min(1, peaks.lastDescriptor.Sub(peaks.firstDescriptor).Seconds()/wall.Seconds()))

	measurement := Measurement{
		DescriptorCollector: sampler.collectorIdentity(),
		RSSState:            rssState, DescriptorState: descriptorState, ObserverRetries: sampler.retryEvidence(),
		TerminalSamples: peaks.terminalSamples, DescriptorTerminalSamples: peaks.descriptorTerminalSamples, ReadingFailures: peaks.failures,
		DescriptorSamples: peaks.descriptorSamples, SampleAttempts: peaks.sampleAttempts, DescriptorCoverage: coverage,
		Stdout:           stdout,
		Stderr:           stderr,
		Wall:             wall,
		PeakRSSBytes:     rss,
		MaxDescriptors:   peaks.descriptors,
		PeakScratchBytes: peaks.scratchBytes,
		ExitCode:         -1,
	}
	if command.ProcessState != nil {
		measurement.ExitCode = command.ProcessState.ExitCode()
	}

	return measurement
}

func unavailableMeasurement() Measurement {
	rss, descriptors := (&processPeaks{}).states(0)
	return Measurement{RSSState: rss, DescriptorState: descriptors, ExitCode: -1}
}
