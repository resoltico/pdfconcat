// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

type processSampler struct {
	directory *os.File
}

var errDescriptorDirectory = errors.New("owned process descriptor directory unavailable")

func prepareProcessSampler() (*processSampler, error) { return &processSampler{}, nil }

// attach opens this owned child's proc inode before the command wait may reap it. Holding this
// directory avoids new pathname permission checks and prevents PID reuse from changing its identity.
func (s *processSampler) attach(_ context.Context, pid int) error {
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/fd")
	if err != nil {
		return fmt.Errorf("open owned process descriptor directory: %w", err)
	}

	s.directory = file

	return nil
}

func (s *processSampler) release() error {
	if s.directory == nil {
		return nil
	}

	file := s.directory
	s.directory = nil

	if err := file.Close(); err != nil {
		return fmt.Errorf("close owned process descriptor directory: %w", err)
	}

	return nil
}

func (s *processSampler) sample(ctx context.Context) processSample {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return descriptorReadFailure(started, err)
	}

	if s.directory == nil {
		return descriptorReadFailure(started, errDescriptorDirectory)
	}

	if _, err := s.directory.Seek(0, io.SeekStart); err != nil {
		return descriptorReadFailure(started, err)
	}

	names, err := s.directory.Readdirnames(-1)
	if err != nil {
		sample := descriptorReadFailure(started, err)
		if errors.Is(err, os.ErrNotExist) && ctx.Err() == nil {
			sample.terminal = true
			sample.failures[0].Phase = phaseTerminal
			sample.failures[0].Probe = "owned_proc_inode_task_gone: ENOENT"
		}

		return sample
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return descriptorReadFailure(started, ctxErr)
	}

	return processSample{descriptors: int64(len(names)), descriptorAt: time.Now()}
}

func descriptorReadFailure(started time.Time, err error) processSample {
	failure := ReadingFailure{
		Metric:       metricDescriptors,
		Phase:        phaseLiveError,
		Cause:        err.Error(),
		ReadStarted:  started,
		ReadFinished: time.Now(),
	}

	return processSample{descriptors: -1, failures: []ReadingFailure{failure}}
}

func (*processSampler) retryEvidence() ObserverRetryEvidence { return ObserverRetryEvidence{} }
