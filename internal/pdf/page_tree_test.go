// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type pageTreeWriteFunc func([]byte) (int, error)

func (w pageTreeWriteFunc) Write(data []byte) (int, error) { return w(data) }

func TestNativePageTreeHasBoundedKidsAndPreservesWriteFailures(t *testing.T) {
	t.Parallel()
	pages := make([]Page, 8193)
	for index := range pages {
		pages[index] = Page{Width: 100, Height: 100}
	}
	var output bytes.Buffer
	written, err := Write(t.Context(), &output, pages)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(output.Len()) {
		t.Fatalf("returned %d bytes, actually wrote %d", written, output.Len())
	}
	native, _, err := generatedContext(t.Context(), pages, len(pages), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := rawPages(t.Context(), native)
	if err != nil || len(actual) != len(pages) {
		t.Fatalf("native page tree: %d %v", len(actual), err)
	}
	for _, entry := range native.Table {
		if entry == nil || entry.Free {
			continue
		}
		dict, ok := entry.Object.(types.Dict)
		if !ok || dict["Type"] != types.Name("Pages") {
			continue
		}
		kids, err := native.DereferenceArray(dict["Kids"])
		if err != nil || len(kids) > 64 {
			t.Fatalf("unbounded Kids array: %d %v", len(kids), err)
		}
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
