// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestAppearanceProgramCompositionIsCheckedBeforeWrapping(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		d := pdffixture.FitAnnotation("Square", "", true, 0, 4)
		program := "q 1 0 0 RG 4 4 m 76 36 l S Q"
		if unsafe {
			program = "q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm 1 0 0 RG 4 4 m 76 36 l S Q"
		}
		d.Objs[6] = []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Matrix [0 2 -1 0 40 0] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(program), program))
		for _, op := range []app.Operation{app.Check, app.Build} {
			r, _, output := selectiveRequest(t, d.Bytes(), "200x300", op)
			if unsafe {
				if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
					t.Fatal("unsafe AP composition accepted", r.Diagnostics)
				}
			} else {
				if r.Status != "ok" {
					t.Fatal(r.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, output)
				}
			}
		}
	}
}
func TestPatternProgramCompositionUsesItsIncomingMatrix(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		d := pdffixture.FitPatterns(false)
		original := d.Objs[5]
		start := bytes.Index(original, []byte("stream\n")) + 7
		end := bytes.LastIndex(original, []byte("\nendstream"))
		program := string(original[start:end])
		if unsafe {
			program = "q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm " + program + " Q"
		}
		d.Objs[5] = []byte(fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Matrix [0 1 -1 0 7 3] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(program), program))
		r, _, output := selectiveRequest(t, d.Bytes(), "300x500", app.Build)
		if unsafe {
			if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
				t.Fatal("unsafe pattern composition accepted", r.Diagnostics)
			}
		} else {
			if r.Status != "ok" {
				t.Fatal(r.Diagnostics)
			}
			qpdfCheck(t, output)
		}
	}
}
