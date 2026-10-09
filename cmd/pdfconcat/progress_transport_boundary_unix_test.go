// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProgressTransportRefusesNonterminalCharacterDevice(t *testing.T) {
	t.Parallel()

	file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	requireProgressNoError(t, err)
	closeProgressResource(t, file)

	if _, err = newProgressTransport(file); !errors.Is(err, errProgressSinkType) {
		t.Fatalf("nonterminal character device: %v", err)
	}
}

func TestProgressTransportClosedAndUnrepresentableDescriptors(t *testing.T) {
	t.Parallel()
	_, file := progressPipe(t)
	requireProgressNoError(t, file.Close())

	if _, err := newProgressTransport(file); err == nil {
		t.Fatal("closed stderr accepted")
	}

	tooLarge := os.NewFile(uintptr(math.MaxInt32)+1, "unrepresentable-progress-descriptor")
	if tooLarge == nil {
		t.Fatal("descriptor wrapper unavailable")
	}

	t.Cleanup(func() {
		if closeErr := tooLarge.Close(); !errors.Is(closeErr, unix.EBADF) {
			t.Errorf("invalid wrapper close: %v", closeErr)
		}
	})

	if _, err := newProgressTransport(tooLarge); !errors.Is(err, errProgressDescriptorRange) {
		t.Fatalf("native poll range not enforced: %v", err)
	}
}

func TestProgressTransportCancellationAfterAdmissionReleasesGate(t *testing.T) {
	t.Parallel()
	_, writer := progressPipe(t)
	transport := progressNativeTransport(t, writer)
	<-transport.admission

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	transport.admission <- struct{}{}

	if err := transport.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission: %v", err)
	}

	if transport.Interrupted() {
		t.Fatal("pre-native cancellation poisoned healthy channel")
	}

	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressEmptyRecord)))
}

func TestProgressNativeWriteErrorsPreserveCompletePendingRecord(t *testing.T) {
	t.Parallel()

	record := []byte(progressTestRecord)

	pending, err := writeProgressChunk(-1, record)
	if !errors.Is(err, unix.EBADF) || len(pending) != len(record) {
		t.Fatalf("failed native write lost pending bytes: %q %v", pending, err)
	}
}
