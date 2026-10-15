// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"
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
	// Every page reference occurs exactly once in bounded Kids arrays.
	refs := map[int]int{}
	for _, match := range regexp.MustCompile(`/Kids \[([^\]]*)\]`).FindAllStringSubmatch(output.String(), -1) {
		fields := strings.Fields(match[1])
		if len(fields)%3 != 0 || len(fields) > 64*3 {
			t.Fatal("unbounded or malformed Kids array")
		}
		for i := 0; i < len(fields); i += 3 {
			id, err := strconv.Atoi(fields[i])
			if err != nil {
				t.Fatal(err)
			}
			refs[id]++
		}
	}
	for i := range pages {
		if refs[3+2*i] != 1 {
			t.Fatalf("page %d appears %d times", i, refs[3+2*i])
		}
	}
	if !strings.Contains(output.String(), "/Count 8193 ") {
		t.Fatal("root count is missing")
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
