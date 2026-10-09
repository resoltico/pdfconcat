// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type (
	progressTransport struct {
		*progressTransportState

		requests chan progressWrite
	}
	progressWrite struct {
		result chan error
		record []byte
	}
)

const progressNativePathBytes = 1024

var errProgressTerminalPath = errors.New("terminal path exceeds native limit")

func openProgressDescriptor(file *os.File) (progressNativeSink, error) {
	descriptor, err := nativeProgressDescriptor(file)
	if err != nil {
		return progressNativeSink{}, err
	}

	var stat unix.Stat_t
	if err = unix.Fstat(int(descriptor), &stat); err != nil {
		return progressNativeSink{}, fmt.Errorf("inspect stderr type: %w", err)
	}

	if stat.Mode&unix.S_IFMT == unix.S_IFIFO {
		return openProgressPipe(descriptor)
	}

	if stat.Mode&unix.S_IFMT != unix.S_IFCHR || !term.IsTerminal(int(descriptor)) {
		return progressNativeSink{}, errProgressSinkType
	}

	path, pathErr := progressTerminalPath(descriptor)
	if pathErr != nil {
		return progressNativeSink{}, pathErr
	}
	// Reopening the device, unlike /dev/fd, owns independent status flags.
	owned, openErr := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if openErr != nil {
		return progressNativeSink{}, fmt.Errorf("open owned progress terminal: %w", openErr)
	}

	protected, protectErr := protectProgressDescriptor(owned)
	if protectErr != nil {
		return progressNativeSink{}, protectErr
	}

	return progressNativeSink{fd: protected, guard: -1}, nil
}

func progressTerminalPath(descriptor uintptr) (string, error) {
	path := make([]byte, progressNativePathBytes)

	var pin runtime.Pinner

	pin.Pin(&path[0])
	defer pin.Unpin()

	_, err := unix.FcntlInt(descriptor, unix.F_GETPATH, int(uintptr(unsafe.Pointer(&path[0]))))
	runtime.KeepAlive(path)

	if err != nil {
		return "", fmt.Errorf("inspect progress terminal path: %w", err)
	}

	before, _, ok := bytes.Cut(path, []byte{0})
	if !ok {
		return "", errProgressTerminalPath
	}

	return string(before), nil
}

func openProgressPipe(descriptor uintptr) (progressNativeSink, error) {
	owned, dupErr := unix.FcntlInt(descriptor, unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
	if dupErr != nil {
		return progressNativeSink{}, fmt.Errorf("duplicate progress pipe: %w", dupErr)
	}

	guard, openErr := unix.Open("/dev/null", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if openErr != nil {
		return progressNativeSink{}, errors.Join(fmt.Errorf("reserve progress cancellation sink: %w", openErr), unix.Close(owned))
	}
	// dup2 atomically drains the original fileproc and replaces its descriptor
	// slot, so a writer entering late cannot hit a reused unrelated descriptor.
	protected, protectErr := protectProgressDescriptor(guard)
	if protectErr != nil {
		return progressNativeSink{}, errors.Join(protectErr, unix.Close(owned))
	}

	return progressNativeSink{fd: owned, guard: protected, interruptiblePipe: true}, nil
}

func interruptProgressPipe(descriptor, guard int) error {
	// dup2 clears CLOEXEC. ForkLock prevents Go's fork/exec between replacement
	// and reinstating CLOEXEC; the reserved slot remains owned until write joins.
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()

	if err := unix.Dup2(guard, descriptor); err != nil {
		return fmt.Errorf("interrupt progress pipe: %w", err)
	}

	return protectCancellationDescriptor(descriptor)
}

// protectCancellationDescriptor finalizes the native replacement under its caller's ForkLock.
func protectCancellationDescriptor(descriptor int) error {
	if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return fmt.Errorf("protect progress cancellation descriptor: %w", err)
	}

	return nil
}

func (transport *progressTransport) startNativeProgress() {
	if transport.interruptiblePipe {
		transport.requests = make(chan progressWrite)
		go transport.run()
	}
}

func (transport *progressTransport) stopNativeProgress() {
	if transport.interruptiblePipe {
		close(transport.requests)
		<-transport.stopped
	}
}

func (transport *progressTransport) closeNativeProgress() {
	transport.closeErr = unix.Close(transport.fd)
	if transport.guard >= 0 {
		transport.closeErr = errors.Join(transport.closeErr, unix.Close(transport.guard))
	}
}

func (transport *progressTransport) run() {
	defer close(transport.stopped)

	for request := range transport.requests {
		request.result <- writeProgressPipeRecord(transport.fd, transport.pollDescriptor, request.record)
	}
}

func (transport *progressTransport) writeInterruptiblePipe(ctx context.Context, record []byte) (bool, error) {
	result := make(chan error, 1)
	select {
	case transport.requests <- progressWrite{record: record, result: result}:
	case <-ctx.Done():
		return false, fmt.Errorf("schedule progress write: %w", ctx.Err())
	}

	select {
	case err := <-result:
		return true, err
	case <-ctx.Done():
		select {
		case err := <-result:
			return true, err
		default:
		}

		// Retiring the descriptor makes every later record unavailable, even if
		// this completed native write raced its result notification with cancellation.
		transport.poison()
		interruptErr := interruptProgressPipe(transport.fd, transport.guard)

		joinedErr := <-result // Join native write before releasing the reserved descriptor slot.

		transport.closeDescriptor()

		retirementErr := errors.Join(interruptErr, transport.closeErr)
		if joinedErr == nil {
			return true, retirementErr
		}

		return true, errors.Join(fmt.Errorf("interrupt progress write: %w", ctx.Err()), joinedErr, retirementErr)
	}
}

func (transport *progressTransport) writeNativeRecord(ctx context.Context, record []byte) (bool, error) {
	if transport.interruptiblePipe {
		return transport.writeInterruptiblePipe(ctx, record)
	}

	return transport.writeNonblocking(ctx, record)
}

func writeProgressPipeRecord(descriptor int, pollDescriptor int32, record []byte) error {
	remaining := record
	for len(remaining) > 0 {
		pending, err := writeProgressChunk(descriptor, remaining)
		if errors.Is(err, unix.EINTR) {
			continue
		}

		if errors.Is(err, unix.EAGAIN) {
			poll := []unix.PollFd{{Fd: pollDescriptor, Events: unix.POLLOUT}}

			_, pollErr := unix.Poll(poll, progressPollMilliseconds)
			if pollErr != nil && !errors.Is(pollErr, unix.EINTR) {
				return fmt.Errorf("poll progress pipe: %w", pollErr)
			}

			continue
		}

		if err != nil {
			return err
		}

		remaining = pending
	}

	return nil
}
