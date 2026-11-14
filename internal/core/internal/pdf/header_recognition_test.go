// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestSourceHeaderRecognitionMatchesParserPlacement(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("plain text"), {0x89, 'P', 'N', 'G', 13, 10, 26, 10}, {0xff, 0xd8, 0xff, 0xe0}} {
		facts, _, ds := pdf.Inspect(t.Context(), data, nil)
		if len(ds) != 1 || ds[0].Code != diag.SourceNotPDF || facts.Bytes != int64(len(data)) || facts.SHA256 != sha256.Sum256(data) {
			t.Fatalf("non-PDF admission: %+v %+v", facts, ds)
		}
	}
	_, _, ds := pdf.Inspect(t.Context(), []byte("%PDF-1.7\ntruncated"), nil)
	if len(ds) != 1 || ds[0].Code != diag.SourceInvalid {
		t.Fatalf("recognizable malformed PDF: %+v", ds)
	}
	// The pinned backend scans beyond the first bytes and buffer boundary.
	shifted := append(bytes.Repeat([]byte{' '}, 203), pdffixture.Plain("displaced header").Bytes()...)
	_, _, ds = pdf.Inspect(t.Context(), shifted, nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatalf("parser-supported displaced header was rejected: %+v", ds)
	}
}
