// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin && cgo && progress_native

package main

import (
	"bytes"
	"io"
	"math"
	"os"
	"os/signal"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/progressnativefixture"
)

type progressSignalWriteResult struct {
	writeErr error
	pending  []byte
	data     []byte
	prefix   int
}

func runProgressSignalWrite(t *testing.T, scenario string) progressSignalWriteResult {
	t.Helper()

	reader, writer := progressPipe(t)
	descriptor, err := nativeProgressDescriptor(writer)
	requireProgressNoError(t, err)

	if descriptor > math.MaxInt32 {
		t.Fatal("native signal fixture descriptor exceeds the poll ABI range")
		return progressSignalWriteResult{}
	}

	native := int32(descriptor)
	requireProgressNoError(t, unix.SetNonblock(int(descriptor), true))
	prefix := fillProgressNativeSink(t, int(descriptor))
	requireProgressNoError(t, unix.SetNonblock(int(descriptor), false))

	runtime.LockOSThread()

	defer runtime.UnlockOSThread()

	notifications := make(chan os.Signal, 64)

	signal.Notify(notifications, unix.SIGUSR1)
	defer signal.Stop(notifications)

	state, err := progressnativefixture.OpenSignalState()

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, state.Close()) }()

	senderDone, signalErrors := sendProgressNativeSignals(state)
	// Join even on panic or Goexit, before Close frees C state or the OS thread unlocks.
	defer func() { <-senderDone }()

	readResult := drainProgressSignalPipe(reader, senderDone)
	result := performProgressSignalWrite(native, scenario)

	<-senderDone

	requireProgressNoError(t, <-signalErrors)
	requireProgressNoError(t, state.Close())
	requireProgressNoError(t, writer.Close())

	select {
	case received := <-readResult:
		requireProgressNoError(t, received.err)
		result.data = received.data
	case <-time.After(time.Second):
		t.Fatal("private signal pipe retained a writer or drainer")
	}

	result.prefix = prefix

	return result
}

func sendProgressNativeSignals(state *progressnativefixture.SignalState) (<-chan struct{}, <-chan error) {
	done := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		defer close(done)

		time.Sleep(10 * time.Millisecond)

		for range 10 {
			if err := state.Send(); err != nil {
				result <- err
				return
			}

			time.Sleep(5 * time.Millisecond)
		}

		result <- nil
	}()

	return done, result
}

func drainProgressSignalPipe(reader *os.File, senderDone <-chan struct{}) <-chan progressDeliveryRead {
	result := make(chan progressDeliveryRead, 1)

	go func() {
		<-senderDone

		data, err := io.ReadAll(reader)
		result <- progressDeliveryRead{data: data, err: err}
	}()

	return result
}

func performProgressSignalWrite(descriptor int32, scenario string) progressSignalWriteResult {
	record := []byte(progressEmptyRecord)
	if scenario == progressSignalStimulus {
		pending, err := writeProgressChunk(int(descriptor), record)
		return progressSignalWriteResult{pending: pending, writeErr: err}
	}

	return progressSignalWriteResult{writeErr: writeProgressPipeRecord(int(descriptor), descriptor, record)}
}

func assertProgressSignalBytes(t *testing.T, scenario string, result progressSignalWriteResult) {
	t.Helper()

	expected := make([]byte, result.prefix)
	if scenario == progressSignalRetry {
		expected = make([]byte, result.prefix+len(progressEmptyRecord))
		copy(expected[result.prefix:], progressEmptyRecord)
	}

	if !bytes.Equal(result.data, expected) {
		t.Fatal("native EINTR handling lost, duplicated or invented pipe bytes")
	}
}
