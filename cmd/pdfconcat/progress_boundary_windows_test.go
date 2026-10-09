// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/resoltico/pdfconcat/internal/cli"
)

type admissionCancellationContext struct {
	context.Context

	cancel context.CancelFunc
}

func (ctx *admissionCancellationContext) Err() error {
	ctx.cancel()
	return ctx.Context.Err()
}

func TestProgressWindowsCancellationAfterAdmissionKeepsChannelHealthy(t *testing.T) {
	t.Parallel()
	reader, caller := progressPipe(t)
	transport := progressNativeTransport(t, caller)

	base, cancel := context.WithCancel(t.Context())
	defer cancel()

	ctx := &admissionCancellationContext{Context: base, cancel: cancel}
	if ctx.Done() != base.Done() {
		t.Fatal("admission cancellation fixture did not delegate the actual Done channel")
	}

	if err := transport.acquire(ctx); !errors.Is(err, context.Canceled) || transport.Interrupted() {
		t.Fatalf("actual cancellation after admission poisoned a healthy channel: %v", err)
	}

	if !errors.Is(base.Err(), context.Canceled) {
		t.Fatal("admission checkpoint did not invoke actual context cancellation")
	}

	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressProducerSentinel)))
	requireProgressNoError(t, transport.Close())
	requireProgressNoError(t, caller.Close())

	data, err := io.ReadAll(reader)
	requireProgressNoError(t, err)

	if string(data) != progressProducerSentinel {
		t.Fatalf("cancellation leaked an admission token or emitted bytes: %q", data)
	}
}

func TestProgressWindowsBackendClassificationRejectsRemoteAndMalformedResults(t *testing.T) {
	t.Parallel()
	// FILE_DEVICE_NAMED_PIPE=0x11, FILE_REMOTE_DEVICE=0x10, FILE_DEVICE_DISK=0x7,
	// and FILE_FS_DEVICE_INFORMATION is eight bytes in the supported Windows SDK ABI.
	cases := []struct {
		name     string
		device   progressPipeDevice
		bytes    uintptr
		accepted bool
	}{
		{"local", progressPipeDevice{deviceType: 0x11}, 8, true},
		{"remote", progressPipeDevice{deviceType: 0x11, characteristics: 0x10}, 8, false},
		{"disk", progressPipeDevice{deviceType: 0x7}, 8, false},
		{"truncated", progressPipeDevice{deviceType: 0x11}, 7, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()

			err := qualifyProgressPipeBackend(item.device, item.bytes)
			if item.accepted && err != nil || !item.accepted && !errors.Is(err, errProgressUnsupportedHandle) {
				t.Fatalf("backend classification acceptance=%v error=%v", item.accepted, err)
			}
		})
	}
}

func TestProgressWindowsRejectsUnavailableCallerHandles(t *testing.T) {
	t.Parallel()

	transport, err := newProgressTransport(nil)
	if transport != nil || !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nil caller handle admitted: %v", err)
	}

	query := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryVolumeInformationFile")
	if _, _, err = duplicateProgressHandle(nil, query); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nil syscall connection accepted: %v", err)
	}

	if terminal, terminalErr := isProgressTerminal(nil); terminal || !errors.Is(terminalErr, os.ErrInvalid) {
		t.Fatalf("nil terminal accepted: %v", terminalErr)
	}

	if _, err = classifyProgressHandle(windows.InvalidHandle, query); !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("invalid native handle accepted: %v", err)
	}

	if _, modeErr := progressPipeMode(windows.InvalidHandle); !errors.Is(modeErr, errProgressUnsupportedHandle) {
		t.Fatalf("invalid pipe mode query accepted: %v", modeErr)
	}

	if _, _, backendErr := progressPipeBackend(windows.InvalidHandle, query); !errors.Is(backendErr, errProgressUnsupportedHandle) {
		t.Fatalf("invalid pipe backend query accepted: %v", backendErr)
	}

	_, caller := progressPipe(t)
	requireProgressNoError(t, caller.Close())

	owner := &progressOwner{file: caller}
	if owner.newSession(t.Context(), cli.ProgressAuto, "closed-caller") != nil || !owner.interrupted() {
		t.Fatal("closed caller did not disable automatic progress truthfully")
	}
}

func TestProgressWindowsUnsupportedOwnerKeepsCallerFile(t *testing.T) {
	t.Parallel()
	file, err := os.Create(filepath.Join(t.TempDir(), "caller-output.txt"))
	requireProgressNoError(t, err)
	closeProgressResource(t, file)

	owner := &progressOwner{file: file}
	if owner.newSession(t.Context(), cli.ProgressJSON, "unsupported-file") != nil || !owner.interrupted() {
		t.Fatal("unsupported file sink did not mark requested telemetry unavailable")
	}

	if err = owner.WriteRecord(t.Context(), []byte(progressTestRecord)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("unconstructed owner accepted output: %v", err)
	}

	owner.release()

	info, err := file.Stat()
	requireProgressNoError(t, err)

	if info.Size() != 0 {
		t.Fatal("refused progress changed the caller's file")
	}
}

func TestProgressWindowsAbandonedRecordNeverWritesCaller(t *testing.T) {
	t.Parallel()
	reader, caller := progressPipe(t)
	aborted := make(chan struct{})
	close(aborted)

	err := writeProgressWindowsRecord(aborted, windows.Handle(caller.Fd()), []byte(progressTestRecord))
	if !errors.Is(err, errProgressRecordAborted) {
		t.Fatalf("abandoned native record admitted: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err = progressNativeResult(ctx, err); !errors.Is(err, context.Canceled) {
		t.Fatalf("abandoned native record lost actual cancellation cause: %v", err)
	}

	transport := progressNativeTransport(t, caller)
	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressProducerSentinel)))
	requireProgressNoError(t, transport.Close())
	requireProgressNoError(t, caller.Close())

	data, readErr := io.ReadAll(reader)
	requireProgressNoError(t, readErr)

	if string(data) != progressProducerSentinel {
		t.Fatalf("canceled scheduling changed healthy delivery: %q", data)
	}
}

func TestProgressWindowsFullNonblockingBytePipeReportsNoProgress(t *testing.T) {
	t.Parallel()
	pair := newProgressNamedPair(t, windows.PIPE_TYPE_BYTE, 0)
	client := pair.wrapClient(t)
	server := progressProducerHandle(t, pair.server)
	mode := uint32(windows.PIPE_NOWAIT)
	requireProgressNoError(t, windows.SetNamedPipeHandleState(server, &mode, nil, nil))
	t.Cleanup(func() {
		blocking := uint32(windows.PIPE_WAIT)
		requireProgressNoError(t, windows.SetNamedPipeHandleState(server, &blocking, nil, nil))
	})

	buffer := make([]byte, 4096)
	accepted := uint32(0)
	full := false

	for range 16 {
		var written uint32
		requireProgressNoError(t, windows.WriteFile(server, buffer, &written, nil))

		if written == 0 {
			full = true
			break
		}

		if uint64(written) > uint64(len(buffer)) {
			t.Fatal("independent nonblocking pipe fill returned impossible byte count")
		}

		accepted += written
	}

	if !full || accepted == 0 {
		t.Fatal("native nonblocking byte pipe did not reach a positively filled zero-write boundary")
	}

	transport := progressNativeTransport(t, pair.server)

	writeErr := transport.WriteRecord(t.Context(), []byte(progressTestRecord))
	if !errors.Is(writeErr, io.ErrNoProgress) || !transport.Interrupted() {
		t.Fatalf("full native byte pipe did not disable unavailable telemetry: %v", writeErr)
	}

	assertProgressPoisoned(t.Context(), t, transport)
	requireProgressNoError(t, transport.Close())

	data := make([]byte, accepted)
	_, err := io.ReadFull(client, data)
	requireProgressNoError(t, err)

	if _, err = pair.server.WriteString(progressProducerSentinel); err != nil {
		t.Fatal(err)
	}

	data = make([]byte, len(progressProducerSentinel))
	_, err = io.ReadFull(client, data)
	requireProgressNoError(t, err)

	if string(data) != progressProducerSentinel {
		t.Fatal("no-progress refusal changed the original producer handle")
	}
}

func TestProgressWindowsCompletedNativeWriteSurvivesLateCancellation(t *testing.T) {
	t.Parallel()
	reader, caller := progressPipe(t)
	transport := progressNativeTransport(t, caller)

	completed := make(chan error, 1)
	transport.requests <- progressWrite{record: []byte(progressTestRecord), result: completed}

	data := make([]byte, len(progressTestRecord))
	_, err := io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressTestRecord {
		t.Fatal("actual native completion did not deliver the intended record")
	}

	deadline := time.Now().Add(time.Second)
	for len(completed) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("actual native writer did not publish its completed result")
		}

		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	requireProgressNoError(t, transport.cancelWrite(ctx, completed))

	if transport.Interrupted() {
		t.Fatal("a genuinely completed native write was falsely classified as interrupted")
	}
}

func TestProgressWindowsCancellationPolicyKeepsActualNativeCauses(t *testing.T) {
	t.Parallel()
	reader, caller := progressPipe(t)
	transport := progressNativeTransport(t, caller)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	postCancellation := make(chan error, 1)
	transport.requests <- progressWrite{record: []byte(progressTestRecord), result: postCancellation}

	data := make([]byte, len(progressTestRecord))
	_, err := io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	completedErr := <-postCancellation
	requireProgressNoError(t, completedErr)
	requireProgressNoError(t, progressCancellationResult(ctx, completedErr, nil))

	var written uint32

	writeErr := windows.WriteFile(windows.InvalidHandle, []byte(progressTestRecord), &written, nil)
	if !errors.Is(writeErr, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("native invalid-handle write control returned %v", writeErr)
	}

	combined := progressCancellationResult(ctx, writeErr, nil)
	if !errors.Is(combined, context.Canceled) || !errors.Is(combined, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("native completion failure lost cancellation or I/O cause: %v", combined)
	}

	accepted, _, cancelErr := transport.cancellation.Call(uintptr(windows.InvalidHandle))
	if accepted != 0 || !errors.Is(cancelErr, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("native invalid-thread cancellation control accepted=%d error=%v", accepted, cancelErr)
	}

	combined = progressCancellationResult(ctx, completedErr, cancelErr)
	if !errors.Is(combined, context.Canceled) || !errors.Is(combined, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("native cancellation failure lost actual causes: %v", combined)
	}
}
