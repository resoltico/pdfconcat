// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestProgressByteAcknowledgmentPreservesPendingRecord(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		native  error
		failure error
		name    string
		pending string
		count   int
	}{
		{name: "zero acknowledges nothing", pending: progressEmptyRecord, failure: io.ErrNoProgress},
		{name: "partial acknowledges exact prefix", count: 1, pending: progressEmptyRecord[1:]},
		{name: "full acknowledges entire record", count: len(progressEmptyRecord)},
		{
			name: "error retains all bytes and cause", count: -1, native: io.ErrClosedPipe,
			pending: progressEmptyRecord, failure: io.ErrClosedPipe,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			record := []byte(progressEmptyRecord)
			original := bytes.Clone(record)
			pending, err := acknowledgeProgressBytes(record, test.count, test.native)

			if string(pending) != test.pending || !errors.Is(err, test.failure) || !bytes.Equal(record, original) {
				t.Fatalf("acknowledgment changed pending bytes, input or cause: %q %v", pending, err)
			}
		})
	}
}
