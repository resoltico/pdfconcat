// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type (
	progressDeliveryOutcome struct {
		err         error
		interrupted bool
	}
	progressDeliveryRead struct {
		err  error
		data []byte
	}
)

const progressDeliveryCases = 200

func TestProgressCancellationAndNativeDeliveryRemainTruthful(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for index := range progressDeliveryCases {
		if ctx.Err() != nil {
			t.Fatal("bounded native cancellation/delivery control exhausted its budget")
		}

		assertProgressDeliveryRace(ctx, t, index)
	}
}

func assertProgressDeliveryRace(budget context.Context, t *testing.T, index int) {
	t.Helper()

	reader, writer, err := os.Pipe()

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, reader.Close()) }()
	defer func() {
		if closeErr := writer.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Error(closeErr)
		}
	}()

	warmProgressDeliveryPipe(t, reader, writer)

	descriptor, err := nativeProgressDescriptor(writer)
	requireProgressNoError(t, err)

	flags := progressDeliveryFlags(t, descriptor)
	transport, err := newProgressTransport(writer)

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, transport.Close()) }()

	if progressDeliveryFlags(t, descriptor) != flags {
		t.Fatal("native constructor changed caller file status flags")
	}

	record := []byte(fmt.Sprintf("{\"case\":%d}\n", index))
	if len(record) > 512 {
		t.Fatal("native delivery oracle requires a record within POSIX minimum PIPE_BUF")
	}

	delivered := make(chan struct{})
	readResult := collectProgressDelivery(reader, len(record), delivered)
	ctx, cancel := context.WithCancel(budget)
	done := make(chan struct{})
	cancellationDone := cancelProgressDelivery(ctx, cancel, delivered, done, index%3)
	writeErr := transport.WriteRecord(ctx, record)

	close(done)
	<-cancellationDone
	assertProgressDeliveryOutcome(budget, t, transport, writeErr, index%3)
	requireProgressNoError(t, transport.Close())

	if progressDeliveryFlags(t, descriptor) != flags {
		t.Fatal("cancellation/delivery race changed caller file status flags")
	}

	_, err = writer.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	requireProgressNoError(t, writer.Close())

	select {
	case observed := <-readResult:
		requireProgressNoError(t, observed.err)
		assertProgressDeliveryBytes(t, observed.data, record, progressDeliveryOutcome{err: writeErr, interrupted: transport.Interrupted()})
	case <-time.After(time.Second):
		t.Fatal("native delivery left a writer/descriptor behind after Close")
	}
}

func warmProgressDeliveryPipe(t *testing.T, reader, writer *os.File) {
	t.Helper()

	// Establish normal written status before the baseline; consume this fixture prefix.
	_, err := writer.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)

	warmup := make([]byte, len(progressEmptyRecord))
	_, err = io.ReadFull(reader, warmup)
	requireProgressNoError(t, err)
}

func cancelProgressDelivery(ctx context.Context, cancel context.CancelFunc, delivered, done <-chan struct{}, timing int) <-chan struct{} {
	finished := make(chan struct{})

	if timing == 0 {
		cancel()
	}

	go func() {
		defer close(finished)
		defer cancel()

		if timing == 1 {
			runtime.Gosched()
			return
		}

		select {
		case <-delivered:
		case <-done:
		case <-ctx.Done():
		}
	}()

	return finished
}

func collectProgressDelivery(reader *os.File, length int, delivered chan<- struct{}) <-chan progressDeliveryRead {
	result := make(chan progressDeliveryRead, 1)

	go func() {
		var observed bytes.Buffer

		buffer := make([]byte, 4096)
		marked := false

		for {
			count, err := reader.Read(buffer)
			observed.Write(buffer[:count])

			if !marked && observed.Len() >= length {
				close(delivered)

				marked = true
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}

				result <- progressDeliveryRead{data: observed.Bytes(), err: err}

				return
			}
		}
	}()

	return result
}

func assertProgressDeliveryOutcome(ctx context.Context, t *testing.T, transport *progressTransport, err error, timing int) {
	t.Helper()

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected native delivery error: %v", err)
	}

	if timing == 0 && (!errors.Is(err, context.Canceled) || transport.Interrupted()) {
		t.Fatal("pre-admission cancellation wrote or poisoned the channel")
	}
	// Delivery success and channel availability are independent after retirement.
	// The byte oracle still requires the whole successful record exactly once.
	if transport.Interrupted() {
		assertProgressPoisoned(ctx, t, transport)
	}
}

func assertProgressDeliveryBytes(t *testing.T, data, record []byte, outcome progressDeliveryOutcome) {
	t.Helper()

	empty := []byte(progressEmptyRecord)

	completed := append(bytes.Clone(record), empty...)
	if outcome.err == nil {
		if !bytes.Equal(data, completed) {
			t.Fatal("successful native write was not delivered exactly once")
		}

		return
	}

	if !outcome.interrupted && !bytes.Equal(data, empty) {
		t.Fatal("unsent canceled producer delivered bytes without native interruption")
	}

	if !bytes.Equal(data, empty) && !bytes.Equal(data, completed) {
		t.Fatal("cancellation race tore, replayed or invented a completed pipe record")
	}
}

func progressDeliveryFlags(t *testing.T, descriptor uintptr) int {
	t.Helper()

	flags, err := unix.FcntlInt(descriptor, unix.F_GETFL, 0)
	requireProgressNoError(t, err)

	return flags
}
