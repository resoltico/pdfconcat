// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

type fuzzExitObserver struct {
	queue    int
	finished bool
}

// Darwin sys/proc.h records these states and its irreversible P_WEXIT transition.
const (
	fuzzDarwinIdle     = 1
	fuzzDarwinRunnable = 2
	fuzzDarwinSleeping = 3
	fuzzDarwinStopped  = 4
	fuzzDarwinZombie   = 5
	fuzzDarwinExiting  = 0x2000
)

func newFuzzExitObserver() (fuzzExitObserver, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return fuzzExitObserver{}, fmt.Errorf("create fuzz exit observer: %w", err)
	}

	if _, err = unix.FcntlInt(uintptr(queue), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return fuzzExitObserver{}, errors.Join(err, unix.Close(queue))
	}

	return fuzzExitObserver{queue: queue}, nil
}

func (o *fuzzExitObserver) attach(pid int) error {
	if pid < 1 {
		return fmt.Errorf("%w: fuzz PID must be positive", errGate)
	}

	event := unix.Kevent_t{
		Ident: uint64(pid), Filter: unix.EVFILT_PROC,
		Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT,
	}

	_, err := unix.Kevent(o.queue, []unix.Kevent_t{event}, nil, &unix.Timespec{})
	if errors.Is(err, unix.ESRCH) {
		// The child may have exited before registration, but it has not been reaped.
		info, queryErr := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if queryErr == nil && int(info.Proc.P_pid) == pid &&
			(info.Proc.P_stat == fuzzDarwinZombie || info.Proc.P_flag&fuzzDarwinExiting != 0) {
			o.finished = true
			return nil
		}

		return errors.Join(fmt.Errorf("register fuzz exit observer: %w", err), queryErr)
	}

	if err != nil {
		return fmt.Errorf("register fuzz exit observer: %w", err)
	}

	return nil
}

func (o *fuzzExitObserver) exited(pid int) (bool, error) {
	if pid < 1 {
		return false, fmt.Errorf("%w: fuzz PID must be positive", errGate)
	}

	if o.finished {
		return true, nil
	}

	events := make([]unix.Kevent_t, 1)

	count, err := unix.Kevent(o.queue, nil, events, &unix.Timespec{})
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("read fuzz exit observer: %w", err)
	}

	if count == 0 {
		return false, nil
	}

	event := events[0]
	if event.Ident != uint64(pid) || event.Filter != unix.EVFILT_PROC ||
		event.Fflags&unix.NOTE_EXIT == 0 || event.Flags&unix.EV_ERROR != 0 {
		return false, fmt.Errorf("%w: unexpected owned fuzz exit event: %+v", errGate, event)
	}

	o.finished = true

	return true, nil
}

func (o *fuzzExitObserver) release() error {
	if err := unix.Close(o.queue); err != nil {
		return fmt.Errorf("close fuzz exit observer: %w", err)
	}

	return nil
}

func signalFuzzGroup(pid int) error {
	if err := unix.Kill(-pid, unix.SIGKILL); err != nil {
		return fmt.Errorf("signal owned fuzz group: %w", err)
	}

	return nil
}

func fuzzGroupTerminal(pid int, signalErr error) (bool, error) {
	if signalErr != nil && !errors.Is(signalErr, unix.ESRCH) && !errors.Is(signalErr, unix.EPERM) {
		return false, fmt.Errorf("terminate owned fuzz group: %w", signalErr)
	}

	group, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pid)
	if err != nil {
		return false, errors.Join(signalErr, fmt.Errorf("inspect owned fuzz group: %w", err))
	}

	leader, terminal := false, true

	for index := range group {
		member := &group[index]
		if int(member.Eproc.Pgid) != pid {
			return false, fmt.Errorf("%w: process group identity mismatch", errGate)
		}

		leader = leader || int(member.Proc.P_pid) == pid

		stopped, stateErr := fuzzDarwinMemberTerminal(member, signalErr)
		if stateErr != nil {
			return false, stateErr
		}

		terminal = terminal && stopped
	}

	if !leader {
		return false, fmt.Errorf("%w: unreaped fuzz group leader absent", errGate)
	}

	return terminal, nil
}

func fuzzDarwinMemberTerminal(member *unix.KinfoProc, signalErr error) (bool, error) {
	switch member.Proc.P_stat {
	case fuzzDarwinZombie:
		return true, nil
	case fuzzDarwinIdle, fuzzDarwinRunnable, fuzzDarwinSleeping, fuzzDarwinStopped:
		if errors.Is(signalErr, unix.EPERM) && member.Proc.P_flag&fuzzDarwinExiting == 0 {
			return false, fmt.Errorf("terminate live fuzz group: %w", signalErr)
		}

		return false, nil
	default:
		return false, fmt.Errorf("%w: unknown owned fuzz process state", errGate)
	}
}
