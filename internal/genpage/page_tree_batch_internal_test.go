// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package genpage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type pageTreeWriteCounter struct {
	bytes.Buffer
	chunks []int
}

func (w *pageTreeWriteCounter) Write(data []byte) (int, error) {
	w.chunks = append(w.chunks, len(data))
	return w.Buffer.Write(data)
}

type pageTreeWriteFunc func([]byte) (int, error)

func (w pageTreeWriteFunc) Write(data []byte) (int, error) { return w(data) }

func TestPageTreeProductionBufferPreservesOrderedReferencesAndWriteChunks(t *testing.T) {
	t.Parallel()
	pages := make([]Page, 8193)
	for index := range pages {
		pages[index] = Page{Width: 100, Height: 100}
	}
	var output pageTreeWriteCounter
	written, err := Write(t.Context(), &output, pages)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(output.Len()) {
		t.Fatalf("returned %d bytes, actually wrote %d", written, output.Len())
	}
	// Match every ordered reference independently of the private reference batch.
	var references strings.Builder
	references.WriteString("/Kids [")
	for index := range pages {
		_, _ = fmt.Fprintf(&references, "%d 0 R ", 3+2*index)
	}
	references.WriteString("]")
	if !strings.Contains(output.String(), references.String()) {
		t.Fatal("page tree omitted, duplicated or reordered a source page reference")
	}
	// Exercise the configured production transport, not a private tiny buffer.
	// A direct write may exceed its capacity, but only the last write may be
	// smaller. Ask bufio for the effective capacity, including its zero default.
	capacity := bufio.NewWriterSize(io.Discard, writeBufferSize).Size()
	remaining := output.Len()
	for index, size := range output.chunks {
		minimum := min(capacity, remaining)
		if size < minimum {
			t.Fatalf("production write %d used %d bytes before the final flush, want at least a %d-byte buffered chunk", index, size, minimum)
		}
		remaining -= size
	}
	if remaining != 0 {
		t.Fatalf("production chunks omitted %d bytes", remaining)
	}

	refused := errors.New("staged output refused")
	for _, short := range []bool{false, true} {
		calls := 0
		failure := pageTreeWriteFunc(func(data []byte) (int, error) {
			calls++
			if short {
				return len(data) - 1, nil
			}
			return 0, refused
		})
		_, err := Write(t.Context(), failure, pages)
		want := refused
		if short {
			want = io.ErrShortWrite
		}
		if !errors.Is(err, want) || calls != 1 {
			t.Fatalf("short=%v: output failure = %v after %d writes, want %v after one write", short, err, calls, want)
		}
	}
}
