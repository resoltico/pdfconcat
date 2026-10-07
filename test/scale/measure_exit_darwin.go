// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

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

const exitObservationErrorFormat = "observe owned process exit: %w"

var (
	errOwnedPID     = errors.New("owned process PID must be positive")
	errExitObserver = errors.New("owned process exit observer unavailable")
)

// observeProcessExit runs before the command wait may reap this owned child. At this boundary its PID
// cannot be reused; ESRCH from registration therefore means the child has already terminated.
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
		observer.exitCause = "owned_exit_before_registration: ESRCH"
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

	count, err := retryExitSyscall(ctx, &o.retries, func() (int, error) { return unix.Kevent(o.queue, nil, events, &unix.Timespec{}) })
	if count > 0 && ownedExitEvent(events[0], pid) {
		o.exitCause = "owned_NOTE_EXIT"
	}

	if err != nil {
		return "owned_exit_event_error", false, fmt.Errorf("read owned process exit queue: %w", err)
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
