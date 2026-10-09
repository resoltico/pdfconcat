// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"errors"
	"fmt"
	"maps"
	"os"

	"golang.org/x/sys/unix"
)

type (
	progressDescriptorLimitResult struct {
		operationErr            error
		cleanupErr              error
		slotsPreserved          bool
		closedStandardPreserved bool
		transportNil            bool
	}
	// The fixture owns all temporary native slots and the exact restoration state.
	progressDescriptorLimitFixture struct {
		baseline      map[int]int
		unexpected    *progressTransport
		fillers       []int
		original      unix.Rlimit
		standardCopy  int
		standardFlags int
	}
)

const progressFixtureDescriptorLimit = 64

var errProgressDescriptorFixture = errors.New("native descriptor-limit fixture")

func runProgressDescriptorLimit(source *os.File, scenario progressDescriptorLimitCase) *progressDescriptorLimitResult {
	result := &progressDescriptorLimitResult{}

	fixture, err := newProgressDescriptorLimitFixture()
	if err != nil {
		result.cleanupErr = err
		return result
	}
	// Return the same result object after deferred cleanup settles every owned slot.
	defer fixture.settle(result)

	if err = fixture.prepare(scenario); err != nil {
		result.cleanupErr = err
		return result
	}

	fixture.unexpected, result.operationErr = newProgressTransport(source)
	result.transportNil = fixture.unexpected == nil
	result.closedStandardPreserved = true

	if scenario.closeStdout {
		_, err = unix.FcntlInt(uintptr(unix.Stdout), unix.F_GETFD, 0)
		result.closedStandardPreserved = errors.Is(err, unix.EBADF)
	}

	return result
}

func newProgressDescriptorLimitFixture() (*progressDescriptorLimitFixture, error) {
	baseline, err := progressOpenDescriptorSlots()
	if err != nil {
		return nil, err
	}

	var original unix.Rlimit
	if limitErr := unix.Getrlimit(unix.RLIMIT_NOFILE, &original); limitErr != nil {
		return nil, fmt.Errorf("read original descriptor limits: %w", limitErr)
	}

	if original.Cur < progressFixtureDescriptorLimit {
		return nil, fmt.Errorf("%w: soft limit %d is below %d", errProgressDescriptorFixture, original.Cur, progressFixtureDescriptorLimit)
	}

	return &progressDescriptorLimitFixture{baseline: baseline, original: original, standardCopy: -1}, nil
}

func (fixture *progressDescriptorLimitFixture) prepare(scenario progressDescriptorLimitCase) error {
	var err error
	if scenario.closeStdout {
		fixture.standardCopy, err = unix.FcntlInt(uintptr(unix.Stdout), unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
		if err != nil {
			return fmt.Errorf("save stdout restoration state: %w", err)
		}

		fixture.standardFlags, err = unix.FcntlInt(uintptr(unix.Stdout), unix.F_GETFD, 0)
		if err != nil {
			return fmt.Errorf("save stdout restoration state: %w", err)
		}
	}

	limited := fixture.original

	limited.Cur = progressFixtureDescriptorLimit
	if limitErr := unix.Setrlimit(unix.RLIMIT_NOFILE, &limited); limitErr != nil {
		return fmt.Errorf("lower descriptor limit: %w", limitErr)
	}

	fixture.fillers, err = fillProgressDescriptorSlots()
	if err != nil {
		return err
	}

	if scenario.freeSlot {
		if len(fixture.fillers) == 0 {
			return fmt.Errorf("%w: no owned filler slot to release", errProgressDescriptorFixture)
		}

		last := fixture.fillers[len(fixture.fillers)-1]
		fixture.fillers = fixture.fillers[:len(fixture.fillers)-1]

		if closeErr := unix.Close(last); closeErr != nil {
			return fmt.Errorf("release owned filler slot: %w", closeErr)
		}
	}

	if scenario.closeStdout {
		if closeErr := unix.Close(unix.Stdout); closeErr != nil {
			return fmt.Errorf("close fixture stdout: %w", closeErr)
		}
	}

	return nil
}

func (fixture *progressDescriptorLimitFixture) settle(result *progressDescriptorLimitResult) {
	result.cleanupErr = errors.Join(result.cleanupErr, unix.Setrlimit(unix.RLIMIT_NOFILE, &fixture.original))
	for _, fd := range fixture.fillers {
		result.cleanupErr = errors.Join(result.cleanupErr, unix.Close(fd))
	}

	if fixture.unexpected != nil {
		result.cleanupErr = errors.Join(result.cleanupErr, fixture.unexpected.Close())
	}

	if fixture.standardCopy >= 0 {
		result.cleanupErr = errors.Join(result.cleanupErr, unix.Dup2(fixture.standardCopy, unix.Stdout))
		_, restoreErr := unix.FcntlInt(uintptr(unix.Stdout), unix.F_SETFD, fixture.standardFlags)
		result.cleanupErr = errors.Join(result.cleanupErr, restoreErr, unix.Close(fixture.standardCopy))
	}

	var restored unix.Rlimit
	if limitErr := unix.Getrlimit(unix.RLIMIT_NOFILE, &restored); limitErr != nil {
		result.cleanupErr = errors.Join(result.cleanupErr, limitErr)
	} else if restored != fixture.original {
		result.cleanupErr = errors.Join(result.cleanupErr, fmt.Errorf("%w: limits not restored", errProgressDescriptorFixture))
	}

	current, slotsErr := progressOpenDescriptorSlots()
	result.cleanupErr = errors.Join(result.cleanupErr, slotsErr)
	result.slotsPreserved = maps.Equal(fixture.baseline, current)
}

func fillProgressDescriptorSlots() ([]int, error) {
	var descriptors []int
	for len(descriptors) <= progressFixtureDescriptorLimit {
		fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.EMFILE) {
			return descriptors, nil
		}

		if err != nil {
			return descriptors, fmt.Errorf("fill native descriptor slots: %w", err)
		}

		descriptors = append(descriptors, fd)
	}

	return descriptors, fmt.Errorf("%w: did not reach EMFILE", errProgressDescriptorFixture)
}

func progressOpenDescriptorSlots() (map[int]int, error) {
	slots := map[int]int{}

	for fd := range progressFixtureDescriptorLimit {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if errors.Is(err, unix.EBADF) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("inspect native descriptor slot: %w", err)
		}

		slots[fd] = flags
	}

	return slots, nil
}
