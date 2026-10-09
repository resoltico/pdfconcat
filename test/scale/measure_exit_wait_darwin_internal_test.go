// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOwnedExitProofWaitReceivesLaterMatchingNativeEvent(t *testing.T) {
	t.Parallel()
	fixture := startCollectorProcessFixture(t)

	_, exited, err := fixture.sampler.exit.confirm(t.Context(), fixture.command.Process.Pid)
	if err != nil || exited {
		t.Fatalf("live owned child prerequisite: %v %v", exited, err)
	}

	killed := make(chan error, 1)

	timer := time.AfterFunc(20*time.Millisecond, func() { killed <- fixture.killAndWait() })

	joined := false
	defer func() {
		if !joined && !timer.Stop() {
			if cleanupErr := <-killed; cleanupErr != nil {
				t.Error(cleanupErr)
			}
		}
	}()

	proof, terminal, waitErr := fixture.sampler.exit.awaitExitProof(t.Context(), fixture.command.Process.Pid)
	killErr := <-killed
	joined = true

	if killErr != nil {
		t.Fatal(killErr)
	}

	if waitErr != nil || !terminal || proof != ownedExitProof {
		t.Fatalf("later native exit not accepted: %s %v %v", proof, terminal, waitErr)
	}
}

func TestOwnedExitProofWaitBudgetExpiresForLiveChild(t *testing.T) {
	t.Parallel()
	fixture := startCollectorProcessFixture(t)
	started := time.Now()

	proof, terminal, err := fixture.sampler.exit.awaitExitProof(t.Context(), fixture.command.Process.Pid)
	if terminal || !errors.Is(err, errOwnedExitProofDeadline) || proof != "owned_exit_proof_deadline" {
		t.Fatalf("live child became terminal: %s %v %v", proof, terminal, err)
	}

	if time.Since(started) < ownedExitProofBudget {
		t.Fatal("absolute native wait budget not exercised")
	}
}

func TestOwnedExitProofWaitCancellationRemainsFailure(t *testing.T) {
	t.Parallel()
	fixture := startCollectorProcessFixture(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()

	_, terminal, err := fixture.sampler.exit.awaitExitProof(ctx, fixture.command.Process.Pid)
	if terminal || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled native wait accepted: %v %v", terminal, err)
	}
}

func TestOwnedExitProofWaitRejectsReleasedNativeQueue(t *testing.T) {
	t.Parallel()

	fixture := startCollectorProcessFixture(t)
	if err := fixture.sampler.exit.release(); err != nil {
		t.Fatal(err)
	}

	_, terminal, err := fixture.sampler.exit.awaitExitProof(t.Context(), fixture.command.Process.Pid)
	if terminal || !errors.Is(err, errExitObserver) {
		t.Fatalf("closed native wait accepted: %v %v", terminal, err)
	}
}

func TestOwnedExitProofWaitRejectsUnrelatedNativeEvent(t *testing.T) {
	t.Parallel()
	fixture := startCollectorProcessFixture(t)

	event := unix.Kevent_t{Ident: 1, Filter: unix.EVFILT_USER, Flags: unix.EV_ADD | unix.EV_ENABLE, Fflags: unix.NOTE_TRIGGER}
	if _, err := unix.Kevent(fixture.sampler.exit.queue, []unix.Kevent_t{event}, nil, &unix.Timespec{}); err != nil {
		t.Fatal(err)
	}

	_, terminal, err := fixture.sampler.exit.awaitExitProof(t.Context(), fixture.command.Process.Pid)
	if terminal || !errors.Is(err, errExitObserver) {
		t.Fatalf("unrelated native wait accepted: %v %v", terminal, err)
	}
}

func TestOwnedExitProofTimeoutClampsParentAndNeverRenewsBudget(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(-time.Nanosecond)
	if _, err := exitEventTimeout(t.Context(), deadline); !errors.Is(err, errOwnedExitProofDeadline) {
		t.Fatal("expired absolute budget renewed")
	}

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(5*time.Millisecond))
	defer cancel()

	timeout, err := exitEventTimeout(ctx, time.Now().Add(time.Second))
	if err != nil || timeout.Nano() > int64(5*time.Millisecond) {
		t.Fatalf("parent deadline not clamped: %+v %v", timeout, err)
	}
}
