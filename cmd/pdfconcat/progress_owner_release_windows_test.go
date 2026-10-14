// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"golang.org/x/sys/windows"
)

func TestProgressOwnerReleaseReportsActualNativeCloseResult(t *testing.T) {
	if runProgressTransportChild(t, progressTransportChildBudget) {
		return
	}
	t.Parallel()
	for _, fault := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy release", true: "actual owned handle close failure"}[fault], func(t *testing.T) {
			reader, caller := progressPipe(t)
			transport, err := newProgressTransport(caller)
			requireProgressNoError(t, err)
			owner := &progressOwner{file: caller, transport: transport}
			t.Cleanup(owner.release)
			if owner.interrupted() {
				t.Fatal("healthy owner unavailable before release")
			}
			if fault {
				requireProgressNoError(t, windows.CloseHandle(owner.transport.handle))
				// Retire the deliberately broken owned identity before another native handle
				// can reuse it. The caller's distinct original remains open.
				owner.transport.handle = windows.InvalidHandle
			}
			owner.release()
			if owner.interrupted() != fault || fault && !errors.Is(owner.transport.closeErr, windows.ERROR_INVALID_HANDLE) || !fault && owner.transport.closeErr != nil {
				t.Fatalf("native release result hidden: interrupted=%t error=%v", owner.interrupted(), owner.transport.closeErr)
			}
			select {
			case <-owner.transport.stopped:
			default:
				t.Fatal("release returned before native writer joined")
			}
			record := []byte(progressTestRecord)
			_, err = caller.Write(record)
			requireProgressNoError(t, err)
			got := make([]byte, len(record))
			_, err = io.ReadFull(reader, got)
			requireProgressNoError(t, err)
			if !bytes.Equal(got, record) {
				t.Fatalf("owner release changed caller bytes: %q", got)
			}
			owner.release()
			if owner.interrupted() != fault {
				t.Fatal("repeated release lost native close outcome")
			}
		})
	}
}
