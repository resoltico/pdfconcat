// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestMixedPDF20AndFormWithoutAppearancesIsIncompatible(t *testing.T) {
	form := defaultForm("FORM", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
	plain := pdffixture.Versioned("plain", "2.0", "")
	for _, docs := range [][]*pdffixture.Doc{{form, plain}, {plain, form}} {
		_, _, ds := assembleDocs(t, docs, nil)
		if !codePresent(ds, "source_output_incompatible") || codePresent(ds, "output_write_failed") {
			t.Fatalf("appearance incompatibility misclassified: %+v", ds)
		}
		if len(ds) != 1 || ds[0].File == "" || len(ds[0].Items) != 1 || !strings.Contains(ds[0].Message, "requires an appearance dictionary") {
			t.Fatalf("missing actionable appearance incompatibility context: %+v", ds)
		}
	}
	appearance := defaultForm("FORM", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
	insertEntry(appearance, 6, "/AP << /N 7 0 R >>")
	content := "BT /Helv 12 Tf 0 g 5 12 Td (FORM) Tj ET\n"
	appearance.Objs = append(appearance.Objs, []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 40] /Resources << /Font << /Helv 5 0 R >> >> /Length %d >>\nstream\n%sendstream", len(content), content)))
	for _, docs := range [][]*pdffixture.Doc{{form}, {appearance, plain}, {plain, appearance}} {
		_, _, ds := assembleDocs(t, docs, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatalf("supported form control: %+v", ds)
		}
	}
}

type shortOutputWriter struct{}

func (shortOutputWriter) Write([]byte) (int, error) { return 0, nil }

func TestAssemblyReportsActualWriterFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.pdf")
	doc := pdffixture.Plain("writer control")
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	facts, ds := pdf.Inspect(t.Context(), doc.Bytes())
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	layout, ds := packet.Resolve(packet.Job{Parts: []packet.Part{{Source: path, Pointer: path}}}, map[string]packet.SourceFacts{path: facts.SourceFacts}, 0)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		name   string
		writer io.Writer
		err    error
	}{
		{"disk error", &failingWriter{}, errDisk},
		{"short write", shortOutputWriter{}, io.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pdf.Assemble(t.Context(), layout, map[string]pdf.SourceFacts{path: facts}, wholeOccurrences(layout, map[string]pdf.SourceFacts{path: facts}), nil, nil, nil, tc.writer)
			if !errors.Is(err, tc.err) || pdf.Diagnostic(err).Code != "output_write_failed" {
				t.Fatalf("writer failure misclassified: %v", err)
			}
		})
	}
}
