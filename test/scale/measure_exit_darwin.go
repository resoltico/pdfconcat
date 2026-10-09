// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

type processExitObserver struct {
	exitCause string
	retries   ObserverRetryEvidence
	queue     int
	attached  bool
}

const (
	exitObservationErrorFormat = "observe owned process exit: %w"
	ownedExitProofBudget       = time.Second
	exitEventWaitQuantum       = 10 * time.Millisecond
)

var (
	errOwnedPID               = errors.New("owned process PID must be positive")
	errExitObserver           = errors.New("owned process exit observer unavailable")
	errOwnedExitProofDeadline = errors.New("owned process exit proof deadline exceeded")
)

// observeProcessExit runs before the command wait may reap this owned child. At this boundary its PID
// cannot be reused; registration ESRCH means it is unobservable or exiting. Command.Wait independently proves completion.
func observeProcessExit(ctx context.Context, pid int) (processExitObserver, error) {
	if pid <= 0 {
		return processExitObserver{}, fmt.Errorf("%w: %d", errOwnedPID, pid)
	}

	queue, err := unix.Kqueue()
	if err != nil {
		return processExitObserver{}, fmt.Errorf("create owned process exit queue: %w", err)
	}

	observer := processExitObserver{queue: queue, attached: true}
	if _, flagErr := unix.FcntlInt(uintptr(queue), unix.F_SETFD, unix.FD_CLOEXEC); flagErr != nil {
		return processExitObserver{}, errors.Join(fmt.Errorf("protect exit queue from child inheritance: %w", flagErr), observer.release())
	}

	event := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}

	_, registerErr := retryExitSyscall(
		ctx,
		&observer.retries,
		func() (int, error) { return unix.Kevent(queue, []unix.Kevent_t{event}, nil, &unix.Timespec{}) },
	)
	if registerErr == nil {
		return observer, nil
	}

	closeErr := observer.release()
	if errors.Is(registerErr, unix.ESRCH) && ctx.Err() == nil && closeErr == nil {
		observer.exitCause = "owned_exit_unobservable_at_registration: ESRCH"
		return observer, nil
	}

	return observer, errors.Join(fmt.Errorf("register owned process exit: %w", registerErr), closeErr)
}

func (o *processExitObserver) release() error {
	if !o.attached {
		return nil
	}

	o.attached = false
	if err := unix.Close(o.queue); err != nil {
		return fmt.Errorf("close owned process exit queue: %w", err)
	}

	return nil
}

func (o *processExitObserver) confirm(ctx context.Context, pid int) (string, bool, error) {
	return o.readExitEvent(ctx, pid, time.Time{})
}

// awaitExitProof is used only after an actual native ESRCH, with one monotonic budget.
func (o *processExitObserver) awaitExitProof(ctx context.Context, pid int) (string, bool, error) {
	deadline := time.Now().Add(ownedExitProofBudget)
	for {
		proof, terminal, err := o.readExitEvent(ctx, pid, deadline)
		if err != nil || terminal {
			return proof, terminal, err
		}
	}
}

func (o *processExitObserver) readExitEvent(ctx context.Context, pid int, deadline time.Time) (string, bool, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "owned_exit_observation_canceled", false, fmt.Errorf(exitObservationErrorFormat, ctxErr)
	}

	if o.exitCause != "" {
		return o.exitCause, true, nil
	}

	if !o.attached {
		return "owned_exit_observer_unavailable", false, errExitObserver
	}

	events := make([]unix.Kevent_t, 1)

	count, err := retryExitSyscall(ctx, &o.retries, func() (int, error) {
		timeout, timeoutErr := exitEventTimeout(ctx, deadline)
		if timeoutErr != nil {
			return 0, timeoutErr
		}

		return unix.Kevent(o.queue, nil, events, &timeout)
	})
	if err != nil {
		if errors.Is(err, errOwnedExitProofDeadline) {
			return "owned_exit_proof_deadline", false, err
		}

		return "owned_exit_event_error", false, fmt.Errorf("read owned process exit queue: %w", err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return "owned_exit_observation_canceled", false, fmt.Errorf(exitObservationErrorFormat, ctxErr)
	}

	if _, deadlineErr := exitEventTimeout(ctx, deadline); deadlineErr != nil {
		return "owned_exit_proof_deadline", false, deadlineErr
	}

	if count == 0 {
		return "owned_process_exit_not_observed", false, nil
	}

	if !ownedExitEvent(events[0], pid) {
		return "unexpected_owned_exit_event", false, fmt.Errorf("%w: %+v", errExitObserver, events[0])
	}

	o.exitCause = "owned_NOTE_EXIT"

	return o.exitCause, true, nil
}

// Compute the remaining absolute budget for every syscall, including EINTR retries.
func exitEventTimeout(ctx context.Context, deadline time.Time) (unix.Timespec, error) {
	if deadline.IsZero() {
		return unix.Timespec{}, nil
	}

	remaining := time.Until(deadline)
	if parentDeadline, found := ctx.Deadline(); found {
		remaining = min(remaining, time.Until(parentDeadline))
	}

	if remaining <= 0 {
		return unix.Timespec{}, errOwnedExitProofDeadline
	}

	return unix.NsecToTimespec(min(remaining, exitEventWaitQuantum).Nanoseconds()), nil
}

func ownedExitEvent(event unix.Kevent_t, pid int) bool {
	if pid <= 0 {
		return false
	}

	return event.Ident == uint64(pid) && event.Filter == unix.EVFILT_PROC && event.Fflags&unix.NOTE_EXIT != 0 &&
		event.Flags&unix.EV_ERROR == 0
}

func retryExitSyscall(ctx context.Context, evidence *ObserverRetryEvidence, native func() (int, error)) (int, error) {
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, fmt.Errorf(exitObservationErrorFormat, ctxErr)
		}

		started := time.Now()

		count, err := native()
		if !errors.Is(err, unix.EINTR) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return count, errors.Join(err, fmt.Errorf(exitObservationErrorFormat, ctxErr))
			}

			return count, err
		}

		evidence.Count++
		if evidence.First.IsZero() {
			evidence.First = started
		}

		evidence.Last = time.Now()
		evidence.Cause = "EINTR: " + err.Error()
	}
}
