// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFiniteFixtureReaderRejectsZeroStorageWithoutConsumingBytes(t *testing.T) {
	t.Parallel()
	reader := ReaderRequiringStorage(strings.NewReader("real bytes"))
	if n, err := reader.Read(nil); n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("zero storage: %d, %v", n, err)
	}
	got, err := io.ReadAll(reader)
	if !errors.Is(err, io.EOF) || string(got) != "real bytes" {
		t.Fatalf("finite content changed: %q, %v", got, err)
	}
}
