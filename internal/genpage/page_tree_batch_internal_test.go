// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package genpage

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

type pageTreeWriteCounter struct {
	bytes.Buffer
	writes int
}

func (w *pageTreeWriteCounter) Write(data []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(data)
}

func TestPageTreeReferenceBatchingBoundsWriteAttempts(t *testing.T) {
	t.Parallel()
	// A small transport buffer makes each page-tree batch observable. With the
	// production buffer, the same per-reference regression is hidden by bufio.
	var output pageTreeWriteCounter
	writer := bufio.NewWriterSize(&output, 16)
	doc := document{out: &sink{w: writer}, offsets: make([]int64, 3), firstPg: 8}
	doc.writePageTree(8193)
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if doc.out.err != nil {
		t.Fatal(doc.out.err)
	}
	if output.writes > 128 {
		t.Fatalf("page-tree references caused %d underlying writes for 8193 pages; batching should keep writes proportional to 4 KiB reference groups", output.writes)
	}
	if !strings.Contains(output.String(), "8 0 R ") || !strings.Contains(output.String(), "16392 0 R ") {
		t.Fatal("page-tree batch omitted its first or last ordered reference")
	}
}
