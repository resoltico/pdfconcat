// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type (
	// progressTransport never changes status flags on the caller's open-file
	// description. All writes and descriptor shutdown belong to this transport.
	progressTransportState struct {
		closeErr          error
		admission         chan struct{}
		stopped           chan struct{}
		closed            chan struct{}
		fd                int
		guard             int
		closedOnce        sync.Once
		fdOnce            sync.Once
		closeOnce         sync.Once
		failed            atomic.Bool
		pollDescriptor    int32
		interruptiblePipe bool
	}
	progressNativeSink struct {
		guard             int
		fd                int
		interruptiblePipe bool
	}
)

var (
	errProgressSinkType        = errors.New("stderr is not a pipe or terminal")
	errProgressDescriptorRange = errors.New("stderr descriptor exceeds native poll range")
)

func isProgressTerminal(file *os.File) (bool, error) {
	descriptor, err := nativeProgressDescriptor(file)
	if err != nil {
		return false, err
	}

	return term.IsTerminal(int(descriptor)), nil
}

func newProgressTransport(file *os.File) (*progressTransport, error) {
	if file == nil {
		return nil, os.ErrInvalid
	}

	sink, err := openProgressDescriptor(file)
	if err != nil {
		return nil, err
	}

	if sink.fd < 0 || sink.fd > math.MaxInt32 {
		return nil, errors.Join(errProgressDescriptorRange, unix.Close(sink.fd))
	}

	state := &progressTransportState{
		fd:                sink.fd,
		guard:             sink.guard,
		pollDescriptor:    int32(sink.fd),
		interruptiblePipe: sink.interruptiblePipe,
		admission:         make(chan struct{}, 1),
		closed:            make(chan struct{}),
		stopped:           make(chan struct{}),
	}
	transport := &progressTransport{progressTransportState: state}

	transport.admission <- struct{}{}

	transport.startNativeProgress()

	return transport, nil
}

func (transport *progressTransport) WriteRecord(ctx context.Context, record []byte) error {
	bounded, cancel := context.WithTimeout(ctx, progressWriteTimeout)
	defer cancel()

	if err := transport.acquire(bounded); err != nil {
		return err
	}

	defer func() { transport.admission <- struct{}{} }()

	attempted, writeErr := transport.writeNativeRecord(bounded, record)
	if attempted && writeErr != nil {
		transport.poison()
	}

	return writeErr
}

func (transport *progressTransport) Close() error {
	transport.closeOnce.Do(func() {
		<-transport.admission
		transport.closeDescriptor()

		transport.stopNativeProgress()

		transport.admission <- struct{}{}
	})

	return transport.closeErr
}

func (transport *progressTransport) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for progress writer: %w", ctx.Err())
	case <-transport.closed:
		return os.ErrClosed
	case <-transport.admission:
	}

	if err := ctx.Err(); err != nil {
		transport.admission <- struct{}{}
		return fmt.Errorf("begin progress write: %w", err)
	}

	select {
	case <-transport.closed:
		transport.admission <- struct{}{}
		return os.ErrClosed
	default:
		return nil
	}
}

func (transport *progressTransport) writeNonblocking(ctx context.Context, record []byte) (bool, error) {
	poll := []unix.PollFd{{Fd: transport.pollDescriptor, Events: unix.POLLOUT}}
	remaining := record
	attempted := false

	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			return attempted, fmt.Errorf("progress write deadline: %w", err)
		}

		attempted = true
		pending, writeErr := writeProgressChunk(transport.fd, remaining)
		remaining = pending

		if writeErr == nil {
			continue
		}

		if !errors.Is(writeErr, unix.EAGAIN) && !errors.Is(writeErr, unix.EINTR) {
			return attempted, writeErr
		}

		if _, pollErr := unix.Poll(poll, progressPollMilliseconds); pollErr != nil && !errors.Is(pollErr, unix.EINTR) {
			return attempted, fmt.Errorf("poll progress sink: %w", pollErr)
		}
	}

	return attempted, nil
}

func writeProgressChunk(descriptor int, record []byte) ([]byte, error) {
	count, err := unix.Write(descriptor, record)

	return acknowledgeProgressBytes(record, count, err)
}

// acknowledgeProgressBytes advances only the prefix actually acknowledged by a native write.
func acknowledgeProgressBytes(record []byte, count int, err error) ([]byte, error) {
	if err != nil {
		return record, fmt.Errorf("write progress sink: %w", err)
	}

	if count == 0 {
		return record, io.ErrNoProgress
	}

	return record[count:], nil
}

func (transport *progressTransport) closeDescriptor() {
	transport.closedOnce.Do(func() { close(transport.closed) })
	transport.fdOnce.Do(func() {
		transport.closeNativeProgress()
	})
}

// SyscallConn observes the native descriptor without File.Fd's implicit change
// from Go-managed nonblocking I/O to blocking I/O.
func nativeProgressDescriptor(file *os.File) (uintptr, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("access stderr descriptor: %w", err)
	}

	var descriptor uintptr

	if err = raw.Control(func(value uintptr) { descriptor = value }); err != nil {
		return 0, fmt.Errorf("inspect stderr descriptor: %w", err)
	}

	if descriptor > math.MaxInt32 {
		return 0, errProgressDescriptorRange
	}

	return descriptor, nil
}

// Native opens may select a closed standard descriptor. Relocate the owned
// resource before returning so final stdout and any later stdin retain truth.
func protectProgressDescriptor(owned int) (int, error) {
	if owned >= progressMinimumOwnedDescriptor {
		return owned, nil
	}

	protected, dupErr := unix.FcntlInt(uintptr(owned), unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)

	return completeProgressRelocation(owned, protected, dupErr)
}

// completeProgressRelocation retires the original acquisition and transfers only a protected descriptor.
func completeProgressRelocation(owned, protected int, dupErr error) (int, error) {
	closeErr := unix.Close(owned)
	if dupErr != nil {
		return -1, errors.Join(fmt.Errorf("protect owned progress descriptor: %w", dupErr), closeErr)
	}

	if closeErr != nil {
		return -1, errors.Join(fmt.Errorf("release standard progress descriptor: %w", closeErr), unix.Close(protected))
	}

	return protected, nil
}
