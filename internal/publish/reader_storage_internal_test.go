// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type storageReader struct {
	source io.Reader
}

// readerRequiringStorage gives finite staging fixtures an explicit progress requirement.
func readerRequiringStorage(source io.Reader) io.Reader {
	return &storageReader{source: source}
}

func (r *storageReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, io.ErrNoProgress
	}

	return r.source.Read(buffer)
}

func TestReaderRequiringStorageRejectsEmptyBuffer(t *testing.T) {
	t.Parallel()

	reader := readerRequiringStorage(strings.NewReader("content"))
	if n, err := reader.Read(nil); n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("empty read = %d, %v", n, err)
	}

	buffer := make([]byte, len("content"))
	if n, err := reader.Read(buffer); n != len(buffer) || err != nil || string(buffer) != "content" {
		t.Fatalf("stored read = %d, %v, %q", n, err, buffer)
	}
}
