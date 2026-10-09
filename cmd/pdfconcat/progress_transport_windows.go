// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

type (
	// progressTransport owns a duplicate stderr handle and one native writer thread.
	// CancelSynchronousIo targets that thread rather than an arbitrary [io.Writer].
	// The thread is joined before its handles are closed or reused.
	progressTransport struct {
		closeErr     error
		closed       chan struct{}
		cancellation *windows.LazyProc
		requests     chan progressWrite
		stopped      chan struct{}
		admission    chan struct{}
		handle       windows.Handle
		thread       windows.Handle
		threadID     uint32
		closedOnce   sync.Once
		closeOnce    sync.Once
		failed       atomic.Bool
		console      bool
	}
	progressWrite struct {
		aborted <-chan struct{}
		result  chan error
		record  []byte
	}
)

const progressWindowsKernel = "kernel32.dll"

var (
	errProgressUnsupportedHandle = errors.New("stderr is not a local synchronous byte pipe or console output buffer")
	errProgressRecordEncoding    = errors.New("console progress record is not valid UTF-8")
	errProgressNativeCount       = errors.New("native progress write returned an impossible count")
	errProgressRecordAborted     = errors.New("native progress record was abandoned")
)

func newProgressTransport(file *os.File) (*progressTransport, error) {
	if file == nil {
		return nil, os.ErrInvalid
	}

	cancelProcedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("CancelSynchronousIo")
	if err := cancelProcedure.Find(); err != nil {
		return nil, fmt.Errorf("load native progress cancellation: %w", err)
	}

	volumeQuery := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryVolumeInformationFile")
	if err := volumeQuery.Find(); err != nil {
		return nil, fmt.Errorf("load native pipe device query: %w", err)
	}

	handle, console, err := duplicateProgressHandle(file, volumeQuery)
	if err != nil {
		return nil, err
	}

	transport := &progressTransport{
		handle:       handle,
		closed:       make(chan struct{}),
		cancellation: cancelProcedure,
		requests:     make(chan progressWrite),
		stopped:      make(chan struct{}),
		admission:    make(chan struct{}, 1),
	}

	transport.console = console

	ready := make(chan error, 1)
	go transport.run(ready)

	if err = <-ready; err != nil {
		<-transport.stopped
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}

	transport.admission <- struct{}{}

	return transport, nil
}

func (transport *progressTransport) WriteRecord(ctx context.Context, record []byte) error {
	if transport.console && !utf8.Valid(record) {
		return errProgressRecordEncoding
	}

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
		transport.closedOnce.Do(func() { close(transport.closed) })
		close(transport.requests)
		<-transport.stopped

		transport.closeErr = errors.Join(windows.CloseHandle(transport.thread), windows.CloseHandle(transport.handle))
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

func (transport *progressTransport) run(ready chan<- error) {
	defer close(transport.stopped)

	runtime.LockOSThread()

	defer runtime.UnlockOSThread()

	threadID := windows.GetCurrentThreadId()

	thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, threadID)
	if err != nil {
		ready <- fmt.Errorf("own native progress thread: %w", err)
		return
	}

	transport.thread = thread
	transport.threadID = threadID

	ready <- nil

	for request := range transport.requests {
		request.result <- transport.writeWindowsRecord(request.aborted, request.record)
	}
}

func (transport *progressTransport) cancelWrite(ctx context.Context, result <-chan error) error {
	// Cancellation can race the worker entering WriteFile. Retry until the
	// particular operation completes; never leave that native writer behind.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	var failure error

	for {
		cancelled, _, cancelErr := transport.cancellation.Call(uintptr(transport.thread))
		if cancelled == 0 && !errors.Is(cancelErr, windows.ERROR_NOT_FOUND) {
			failure = fmt.Errorf("cancel native progress write: %w", cancelErr)
		}

		select {
		case completedErr := <-result:
			if completedErr == nil && failure == nil {
				return nil
			}

			return errors.Join(fmt.Errorf("progress write deadline: %w", ctx.Err()), completedErr, failure)
		case <-ticker.C:
		}
	}
}

func duplicateProgressHandle(file *os.File, volumeQuery *windows.LazyProc) (windows.Handle, bool, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return 0, false, fmt.Errorf("access stderr handle: %w", err)
	}

	var (
		handle       windows.Handle
		console      bool
		admissionErr error
	)

	controlErr := raw.Control(func(value uintptr) {
		original := windows.Handle(value)

		console, admissionErr = classifyProgressHandle(original, volumeQuery)
		if admissionErr != nil {
			return
		}

		process := windows.CurrentProcess()
		admissionErr = windows.DuplicateHandle(process, original, process, &handle, 0, false, windows.DUPLICATE_SAME_ACCESS)
	})
	if err = errors.Join(controlErr, admissionErr); err != nil {
		if handle != 0 {
			err = errors.Join(err, windows.CloseHandle(handle))
		}

		return 0, false, fmt.Errorf("admit owned progress stderr handle: %w", err)
	}

	return handle, console, nil
}

func (transport *progressTransport) writeNativeRecord(ctx context.Context, record []byte) (bool, error) {
	result := make(chan error, 1)
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("schedule progress write: %w", ctx.Err())
	case transport.requests <- progressWrite{aborted: ctx.Done(), record: record, result: result}:
	}

	select {
	case err := <-result:
		return true, progressNativeResult(ctx, err)
	case <-ctx.Done():
		select {
		case err := <-result:
			return true, progressNativeResult(ctx, err)
		default:
		}

		return true, transport.cancelWrite(ctx, result)
	}
}

func writeProgressWindowsRecord(aborted <-chan struct{}, handle windows.Handle, record []byte) error {
	remaining := record
	for len(remaining) > 0 {
		if progressRecordAbandoned(aborted) {
			return errProgressRecordAborted
		}

		var count uint32
		if err := windows.WriteFile(handle, remaining, &count, nil); err != nil {
			return fmt.Errorf("write native progress sink: %w", err)
		}

		if count == 0 {
			return io.ErrNoProgress
		}

		if uint64(count) > uint64(len(remaining)) {
			return errProgressNativeCount
		}

		remaining = remaining[count:]
	}

	return nil
}

func (transport *progressTransport) writeWindowsRecord(aborted <-chan struct{}, record []byte) error {
	if transport.console {
		return writeProgressConsoleRecord(aborted, transport.handle, record)
	}

	return writeProgressWindowsRecord(aborted, transport.handle, record)
}

// Match Go's standard console path: UTF-16 retains Unicode independently of
// the caller's output codepage, and bounded rune chunks avoid native buffer limits.
func writeProgressConsoleRecord(aborted <-chan struct{}, handle windows.Handle, record []byte) error {
	const maxConsoleRunes = 16000

	remaining := []rune(string(record))
	for len(remaining) > 0 {
		runeCount := min(len(remaining), maxConsoleRunes)
		units := utf16.Encode(remaining[:runeCount])
		// At most16000 runes produce at most32000 UTF-16 units.
		// Residual writes only shrink this buffer, so its length fits DWORD.
		for len(units) > 0 {
			if progressRecordAbandoned(aborted) {
				return errProgressRecordAborted
			}

			requested := uint32(len(units))

			var count uint32
			if err := windows.WriteConsole(handle, &units[0], requested, &count, nil); err != nil {
				return fmt.Errorf("write native progress console: %w", err)
			}

			if count == 0 {
				return io.ErrNoProgress
			}

			if count > requested {
				return errProgressNativeCount
			}

			units = units[count:]
		}

		remaining = remaining[runeCount:]
	}

	return nil
}

func classifyProgressHandle(handle windows.Handle, volumeQuery *windows.LazyProc) (bool, error) {
	kind, kindErr := windows.GetFileType(handle)
	if kindErr != nil {
		return false, fmt.Errorf("identify stderr native handle: %w", kindErr)
	}

	if kind == windows.FILE_TYPE_PIPE {
		return false, qualifyProgressPipe(handle, volumeQuery)
	}

	if kind != windows.FILE_TYPE_CHAR {
		return false, errProgressUnsupportedHandle
	}

	var mode uint32
	if modeErr := windows.GetConsoleMode(handle, &mode); modeErr != nil {
		return false, errors.Join(errProgressUnsupportedHandle, fmt.Errorf("identify stderr console mode: %w", modeErr))
	}

	var info windows.ConsoleScreenBufferInfo
	if bufferErr := windows.GetConsoleScreenBufferInfo(handle, &info); bufferErr != nil {
		return false, errors.Join(errProgressUnsupportedHandle, fmt.Errorf("identify stderr output buffer: %w", bufferErr))
	}

	return true, nil
}

func isProgressTerminal(file *os.File) (bool, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return false, fmt.Errorf("access stderr terminal handle: %w", err)
	}

	terminal := false

	controlErr := raw.Control(func(value uintptr) {
		var mode uint32

		terminal = windows.GetConsoleMode(windows.Handle(value), &mode) == nil
	})
	if controlErr != nil {
		return false, fmt.Errorf("inspect stderr terminal handle: %w", controlErr)
	}

	return terminal, nil
}

func progressRecordAbandoned(aborted <-chan struct{}) bool {
	select {
	case <-aborted:
		return true
	default:
		return false
	}
}

func progressNativeResult(ctx context.Context, err error) error {
	if errors.Is(err, errProgressRecordAborted) {
		return fmt.Errorf("abandon progress record: %w", ctx.Err())
	}

	return err
}
