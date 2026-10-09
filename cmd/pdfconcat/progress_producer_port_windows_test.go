// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type progressPortOperation struct {
	source    *os.File
	pin       runtime.Pinner
	data      []byte
	operation windows.Overlapped
	handle    windows.Handle
	port      windows.Handle
	retired   bool
}

const progressCompletionKey = uintptr(0x504446)

var errProgressCompletionIdentity = errors.New("native completion identity does not match submitted operation")

func assertProgressForeignPortProducer(t *testing.T) {
	t.Helper()
	pair := newProgressNamedPair(t, windows.PIPE_TYPE_BYTE, windows.FILE_FLAG_OVERLAPPED)

	runtime.LockOSThread()

	defer runtime.UnlockOSThread()

	port, err := windows.CreateIoCompletionPort(pair.clientHandle, 0, progressCompletionKey, 1)

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, windows.CloseHandle(port)) }()

	source := pair.wrapClient(t)
	assertProgressPortExchange(t, source, pair.server, port)
	assertProgressProducerRefused(t, source)
	assertProgressPortExchange(t, source, pair.server, port)
}

func assertProgressPortExchange(t *testing.T, source, peer *os.File, port windows.Handle) {
	t.Helper()

	operation := &progressPortOperation{
		source: source, handle: progressProducerHandle(t, source), port: port,
		data: []byte(progressProducerSentinel),
	}
	if unsafe.Sizeof(operation.operation) != 32 || unsafe.Offsetof(operation.operation.HEvent) != 24 {
		t.Fatal("native completion fixture requires the documented Windows64 OVERLAPPED ABI")
	}

	operation.pin.Pin(&operation.operation)

	operation.pin.Pin(&operation.data[0])
	defer operation.pin.Unpin()

	var count uint32

	writeErr := windows.WriteFile(operation.handle, operation.data, &count, &operation.operation)
	if writeErr != nil && !errors.Is(writeErr, windows.ERROR_IO_PENDING) {
		t.Fatal(writeErr)
	}
	// Every unwind must consume this operation's completion before releasing memory.
	defer operation.retire(t)

	completed, err := operation.completion(t)
	if !completed || err != nil {
		t.Errorf("native completion failed or was misattributed: %v", err)
		return
	}

	data := make([]byte, len(operation.data))
	_, err = io.ReadFull(peer, data)
	requireProgressNoError(t, err)

	if !bytes.Equal(data, operation.data) {
		t.Fatal("foreign completion-port producer changed exact peer bytes")
	}

	runtime.KeepAlive(source)
}

func (operation *progressPortOperation) completion(t *testing.T) (bool, error) {
	t.Helper()

	var (
		count     uint32
		key       uintptr
		completed *windows.Overlapped
		pin       runtime.Pinner
	)
	pin.Pin(&count)
	pin.Pin(&key)

	pin.Pin(&completed)
	defer pin.Unpin()

	err := windows.GetQueuedCompletionStatus(operation.port, &count, &key, &completed, 1000)
	runtime.KeepAlive(operation.source)

	if completed != &operation.operation || key != progressCompletionKey {
		return false, errors.Join(errProgressCompletionIdentity, err)
	}

	operation.retired = true

	if err != nil {
		return true, fmt.Errorf("native operation completed with failure: %w", err)
	}

	if uint64(count) != uint64(len(operation.data)) {
		return true, errProgressNativeCount
	}

	return true, nil
}

func (operation *progressPortOperation) retire(t *testing.T) {
	t.Helper()

	if operation.retired {
		return
	}

	err := windows.CancelIoEx(operation.handle, &operation.operation)
	if err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		t.Errorf("native completion cleanup cancellation: %v", err)
	}

	completed, completionErr := operation.completion(t)
	if completed {
		if completionErr != nil {
			t.Logf("failed fixture's operation joined: %v", completionErr)
		}

		return
	}
	// Neither cancellation nor a timeout authorizes Unpin. Keep this failed
	// private process's owners alive until its parent kills and joins it.
	t.Errorf("native completion not joined; parent watchdog must terminate this failed fixture: %v", completionErr)

	for {
		time.Sleep(time.Second)
	}
}
