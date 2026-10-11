// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"bytes"
	"errors"
	"io"
	"math"
	"slices"
	"testing"
)

type observedCaptureReader struct {
	io.Reader
	windows []int
}

func (r *observedCaptureReader) Read(buffer []byte) (int, error) {
	r.windows = append(r.windows, len(buffer))
	if len(buffer) == 0 {
		return 0, io.ErrNoProgress
	}
	return r.Reader.Read(buffer)
}

func TestLimitedCopyReadsOnlyRemainingBytesAndOneOverflowByte(t *testing.T) {
	const limit = copyChunkSize + 2
	source := &observedCaptureReader{Reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, limit))}
	var sink bytes.Buffer
	captured := Captured{SourcePath: "bounded-source"}
	if err := newTestWorkspace(t).copyChunks(t.Context(), source, &sink, &captured, limit); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(source.windows, []int{copyChunkSize, 3, 1}) || captured.Size != limit || sink.Len() != limit {
		t.Fatalf("read windows=%v captured=%d sink=%d", source.windows, captured.Size, sink.Len())
	}
}

func TestStoredChunkAcceptsLastRepresentableByteCountAndRejectsOverflow(t *testing.T) {
	workspace := newTestWorkspace(t)
	captured := Captured{SourcePath: "large-source", Size: math.MaxInt64 - 1}
	if err := workspace.storeChunk(io.Discard, []byte{'x'}, &captured); err != nil || captured.Size != math.MaxInt64 {
		t.Fatalf("last representable byte: size=%d error=%v", captured.Size, err)
	}
	err := workspace.storeChunk(io.Discard, []byte{'x'}, &captured)
	var overflow *LimitError
	if !errors.As(err, &overflow) || captured.Size != math.MaxInt64 {
		t.Fatalf("overflow: size=%d error=%v", captured.Size, err)
	}
}
