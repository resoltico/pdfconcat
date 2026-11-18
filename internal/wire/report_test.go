// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package result

import (
	"fmt"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
)

type collectingWriter struct {
	records *int
	firstAt int
	largest int
	total   int
}

func (w *collectingWriter) Write(b []byte) (int, error) {
	if w.total == 0 {
		w.firstAt = *w.records
	}
	w.largest = max(w.largest, len(b))
	w.total += len(b)
	return len(b), nil
}

func TestFullReportStreamsParts(t *testing.T) {
	const count = 30_000
	produced := 0
	sink := collectingWriter{records: &produced}
	parts := func(yield func(outcome.Part) bool) {
		for i := range count {
			produced++
			if !yield(outcome.Part{Item: fmt.Sprintf("/items/%d", i), Kind: packet.PartPage, Count: 1}) {
				return
			}
		}
	}
	sections := func(func(outcome.Section) bool) {}
	if err := WriteReport(&sink, outcome.New("check"), parts, sections); err != nil {
		t.Fatal(err)
	}
	if produced != count || sink.firstAt >= 100 || sink.largest > 64<<10 || sink.total < 1<<20 {
		t.Fatalf("report did not stream bounded records: last=%d first-write=%d largest-write=%d bytes=%d", produced, sink.firstAt, sink.largest, sink.total)
	}
}
