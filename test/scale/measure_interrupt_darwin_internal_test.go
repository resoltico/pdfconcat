// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build darwin

package scale

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInterruptedExitObservationRetriesUntilRealOwnedKernelEvent(t *testing.T) {
	t.Parallel()

	fixture := startCollectorProcessFixture(t)
	if err := fixture.killAndWait(); err != nil {
		t.Fatal(err)
	}

	events := make([]unix.Kevent_t, 1)
	before := fixture.sampler.exit.retries.Count
	calls := int64(0)

	count, err := retryExitSyscall(t.Context(), &fixture.sampler.exit.retries, func() (int, error) {
		calls++
		if calls == 1 {
			return 0, unix.EINTR
		}

		return unix.Kevent(fixture.sampler.exit.queue, nil, events, &unix.Timespec{})
	})
	if err != nil || count != 1 || !ownedExitEvent(events[0], fixture.sampler.pid) {
		t.Fatalf("retry invented a kernel proof: count=%d event=%+v err=%v", count, events[0], err)
	}

	evidence := fixture.sampler.retryEvidence()
	if evidence.Count-before != calls-1 || evidence.First.IsZero() || evidence.Last.Before(evidence.First) ||
		!strings.HasPrefix(evidence.Cause, "EINTR:") {
		t.Fatalf("bounded retry evidence lost: %+v calls=%d", evidence, calls)
	}
}

func TestInterruptedExitObservationStopsWhenCallerCancels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var evidence ObserverRetryEvidence

	calls := 0

	_, err := retryExitSyscall(ctx, &evidence, func() (int, error) { calls++; cancel(); return 0, unix.EINTR })
	if !errors.Is(err, context.Canceled) || calls != 1 || evidence.Count != 1 {
		t.Fatalf("interrupted observation ignored caller cancellation: %v calls=%d %+v", err, calls, evidence)
	}
}

func TestExitObservationDoesNotRetryBadDescriptorOrPermission(t *testing.T) {
	t.Parallel()

	for _, fault := range []error{unix.EBADF, unix.EACCES} {
		var evidence ObserverRetryEvidence

		calls := 0

		_, err := retryExitSyscall(t.Context(), &evidence, func() (int, error) { calls++; return -1, fault })
		if !errors.Is(err, fault) || calls != 1 || evidence.Count != 0 {
			t.Fatalf("unknown fault retried: %v calls=%d %+v", err, calls, evidence)
		}
	}
}
