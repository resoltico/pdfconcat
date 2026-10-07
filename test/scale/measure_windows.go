// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package scale

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS from psapi.h.
type (
	processMemoryCounters struct {
		Cb                         uint32
		PageFaultCount             uint32
		PeakWorkingSetSize         uintptr
		WorkingSetSize             uintptr
		QuotaPeakPagedPoolUsage    uintptr
		QuotaPagedPoolUsage        uintptr
		QuotaPeakNonPagedPoolUsage uintptr
		QuotaNonPagedPoolUsage     uintptr
		PagefileUsage              uintptr
		PeakPagefileUsage          uintptr
	}
	processSampler struct{ handle windows.Handle }
)

const sampleInterval = 5 * time.Millisecond

var errProcessWait = errors.New("unexpected process wait result")

// startProcess starts the command. Windows has no descriptor limit to impose, so the limit is ignored
// and handle counts are only sampled.
func startProcess(command *exec.Cmd, _ uint64) error {
	err := command.Start()
	if err != nil {
		return fmt.Errorf("launch process: %w", err)
	}

	return nil
}

func prepareProcessSampler() (*processSampler, error) { return &processSampler{}, nil }

// The exec.Process still owns the process object while this extra synchronized query handle is acquired.
func (s *processSampler) attach(_ context.Context, pid int) error {
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ|windows.SYNCHRONIZE,
		false,
		uint32(pid&math.MaxUint32),
	)
	if err != nil {
		return fmt.Errorf("open owned process sampling handle: %w", err)
	}

	s.handle = handle

	return nil
}

func (s *processSampler) release() error {
	if s.handle == 0 {
		return nil
	}

	handle := s.handle

	s.handle = 0
	if err := windows.CloseHandle(handle); err != nil {
		return fmt.Errorf("close owned process sampling handle: %w", err)
	}

	return nil
}

func (s *processSampler) sample(ctx context.Context) processSample {
	started := time.Now()
	if ctx.Err() != nil {
		return failedWindowsCollector(started, ctx.Err())
	}

	waited, waitErr := windows.WaitForSingleObject(s.handle, 0)
	if waitErr != nil {
		return failedWindowsCollector(started, waitErr)
	}

	if waited == windows.WAIT_OBJECT_0 {
		return processSample{
			descriptors: -1,
			terminal:    true,
			failures: []ReadingFailure{
				{
					Metric:       metricCollector,
					Phase:        phaseTerminal,
					Cause:        "owned process handle signaled before query",
					Probe:        "WAIT_OBJECT_0",
					ReadStarted:  started,
					ReadFinished: time.Now(),
				},
			},
		}
	}

	if waited != uint32(windows.WAIT_TIMEOUT) {
		return failedWindowsCollector(started, fmt.Errorf("%w: %d", errProcessWait, waited))
	}

	sample := s.queryMetrics(started)
	if len(sample.failures) != 0 && ctx.Err() == nil && eligibleTerminatedQueryError(sample) {
		waited, waitErr = windows.WaitForSingleObject(s.handle, 0)
		if waitErr == nil && waited == windows.WAIT_OBJECT_0 {
			sample.terminal = true
			for index := range sample.failures {
				sample.failures[index].Phase = phaseTerminal
				sample.failures[index].Probe = "WAIT_OBJECT_0"
			}
		}
	}

	return sample
}

func failedWindowsCollector(started time.Time, err error) processSample {
	return processSample{descriptors: -1, rssError: true, failures: []ReadingFailure{windowsReadingFailure(metricCollector, started, err)}}
}

func windowsReadingFailure(metric string, started time.Time, err error) ReadingFailure {
	failure := ReadingFailure{Metric: metric, Phase: phaseLiveError, Cause: err.Error(), ReadStarted: started, ReadFinished: time.Now()}
	if native, found := errors.AsType[syscall.Errno](err); found {
		failure.NativeCode = uint64(native)
	}

	return failure
}

func (s *processSampler) queryMetrics(started time.Time) processSample {
	sample := processSample{descriptors: -1, rssError: true}

	var count uint32

	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	succeeded, _, descriptorErr := kernel32.NewProc("GetProcessHandleCount").Call(uintptr(s.handle), uintptr(unsafe.Pointer(&count)))
	if succeeded != 0 {
		sample.descriptors = int64(count)
		sample.descriptorAt = time.Now()
	} else {
		sample.failures = append(sample.failures, windowsReadingFailure(metricDescriptors, started, descriptorErr))
	}

	counters := processMemoryCounters{Cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	psapi := syscall.NewLazyDLL("psapi.dll")

	succeeded, _, memoryErr := psapi.NewProc("GetProcessMemoryInfo").
		Call(uintptr(s.handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Cb))
	if succeeded != 0 {
		sample.rssBytes = int64(counters.PeakWorkingSetSize & math.MaxInt64)
		sample.rssError = false
	} else {
		sample.failures = append(sample.failures, windowsReadingFailure("rss", started, memoryErr))
	}

	return sample
}

// Only disappearance/partial-copy errors are eligible; access, handle, and context faults remain errors.
func eligibleTerminatedQueryError(sample processSample) bool {
	for index := range sample.failures {
		if sample.failures[index].NativeCode != uint64(windows.ERROR_INVALID_PARAMETER) &&
			sample.failures[index].NativeCode != uint64(windows.ERROR_PARTIAL_COPY) {
			return false
		}
	}

	return len(sample.failures) != 0
}

// peakResidentBytes returns the largest peak working set sampled while the process ran; it is the best
// available figure, and a lower bound if growth happened between the last sample and exit.
func peakResidentBytes(_ *os.ProcessState, sampled int64) int64 {
	return sampled
}

func (*processSampler) retryEvidence() ObserverRetryEvidence { return ObserverRetryEvidence{} }
