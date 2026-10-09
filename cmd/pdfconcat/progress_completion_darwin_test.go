// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestProgressCompletedNativePipeWriteSurvivesLateCancellation(t *testing.T) {
	t.Parallel()

	reader, caller := progressPipe(t)

	transport := progressNativeTransport(t, caller)

	if !transport.interruptiblePipe {
		t.Fatal("actual Darwin pipe did not use its native writer")
	}

	result := make(chan error, 1)
	transport.requests <- progressWrite{record: []byte(progressEmptyRecord), result: result}

	data := make([]byte, len(progressEmptyRecord))
	_, err := io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressEmptyRecord {
		t.Fatal("real native write changed complete record bytes")
	}

	requireProgressBufferedCompletion(t, result)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	attempted, err := transport.retireInterruptiblePipe(ctx, result)
	if !attempted || err != nil || transport.Interrupted() {
		t.Fatalf("actual completed native pipe was retired by late cancellation: %t/%v", attempted, err)
	}

	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressEmptyRecord)))

	_, err = io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressEmptyRecord {
		t.Fatal("late cancellation disabled later healthy native writes")
	}

	requireProgressNoError(t, transport.Close())

	_, err = caller.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)

	_, err = io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressEmptyRecord {
		t.Fatal("late cancellation changed original caller pipe")
	}
}

func requireProgressBufferedCompletion(t *testing.T, result <-chan error) {
	t.Helper()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticks := time.NewTicker(time.Millisecond)
	defer ticks.Stop()

	for len(result) == 0 {
		select {
		case <-deadline.C:
			t.Fatal("native worker did not acknowledge its actual completed write")
		case <-ticks.C:
		}
	}
}
