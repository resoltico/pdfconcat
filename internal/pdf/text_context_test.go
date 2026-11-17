// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestFullTextContextAndPatternParentEntry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unsafe bool
	}{
		{"safe", false}, {"unsafe", true}, {"reciprocal-safe", false}, {"reciprocal-unsafe", true},
		{"inherited-mode3", false}, {"explicit-mode3", false}, {"explicit-mode0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata/text-context", tc.name+".pdf")
			qpdfCheck(t, path)
			for _, target := range []string{"600x800", "1000x800"} {
				for _, op := range []app.Operation{app.Check, app.Build} {
					r, _, output := selectiveRequest(t, readPDF(t, path), target, op)
					if tc.unsafe && target == "1000x800" {
						if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) || r.Output.Published {
							t.Fatal("wrong full execution context accepted", r.Diagnostics)
						}
					} else {
						if r.Status != "ok" {
							t.Fatal("safe/invisible execution context refused", r.Diagnostics)
						}
						if op == app.Build {
							qpdfCheck(t, output)
							if target == "600x800" && !reflect.DeepEqual(fitRaster(t, path, 1, 144), fitRaster(t, output, 1, 144)) {
								t.Fatal("keep changed the picture")
							}
						}
					}
				}
			}
		})
	}
}

func textContextDoc(program string) *pdffixture.Doc {
	glyph := "600 0 d0 q 0 0 1 rg 0 0 300 400 re f Q"
	stream := func(s string) []byte {
		return []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s))
	}
	return &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"), []byte("<< /Type /Pages /Count 1 /Kids [3 0 R] >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>"), stream(program),
		[]byte("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 600 800] /FontMatrix [1 0 0 1 0 0] /FirstChar 32 /LastChar 65 /Widths [0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 600] /Encoding << /Type /Encoding /Differences [65 /A] >> /CharProcs << /A 6 0 R >> /Resources << >> >>"), stream(glyph),
	}}
}
func TestTextCursorOperatorsProduceEquivalentFullRenderingContexts(t *testing.T) {
	control := textContextDoc("BT /F1 1 Tf 1 0 0 1 20 40 Tm (A) Tj ET")
	r, _, reference := selectiveRequest(t, control.Bytes(), "1000x800", app.Build)
	if r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	want := fitRaster(t, reference, 1, 144)
	for _, program := range []string{
		"BT /F1 1 Tf 20 40 Td (A) Tj ET",
		"BT /F1 1 Tf 20 40 TD (A) Tj ET",
		"BT /F1 1 Tf 20 50 Td 10 TL T* (A) Tj ET",
		"BT /F1 1 Tf 20 50 Td 10 TL (A) ' ET",
		"BT /F1 1 Tf 20 50 Td 10 TL 0 0 (A) \" ET",
		"BT /F1 1 Tf 1 0 0 1 0 40 Tm [-20000 (A)] TJ ET",
		"BT /F1 1 Tf 1 0 0 1 -580 40 Tm (A) Tj (A) Tj ET",
		"BT /F1 1 Tf 1 0 0 1 -590 40 Tm 10 Tc (A) Tj 0 Tc (A) Tj ET",
		"BT /F1 1 Tf 1 0 0 1 0 40 Tm 20 Tw ( ) Tj (A) Tj ET",
		// q/Q saves graphics text parameters, NOT Tm. The second placement persists.
		"BT /F1 1 Tf q 1 0 0 1 20 40 Tm Q (A) Tj ET",
	} {
		r, _, output := selectiveRequest(t, textContextDoc(program).Bytes(), "1000x800", app.Build)
		if r.Status != "ok" {
			t.Fatal(program, r.Diagnostics)
		}
		qpdfCheck(t, output)
		if !reflect.DeepEqual(want, fitRaster(t, output, 1, 144)) {
			t.Fatal("text cursor did not produce expected picture", program)
		}
	}
}

func TestUnknownTextCursorRequiresRecoveryBeforeType3(t *testing.T) {
	for _, recovery := range []string{"", "1 0 0 1 20 40 Tm", "20 40 Td", "ET BT 20 40 Td"} {
		d := textContextDoc("BT /F2 1 Tf (A) Tj /F1 1 Tf " + recovery + " (A) Tj ET")
		d.Objs[2] = []byte(strings.ReplaceAll(string(d.Objs[2]), "/F1 5 0 R", "/F1 5 0 R /F2 7 0 R"))
		d.Objs = append(d.Objs, []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"))
		r, _, _ := selectiveRequest(t, d.Bytes(), "1000x800", app.Build)
		if recovery == "" {
			if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) || r.Output.Published {
				t.Fatal("unknown cursor accepted", r.Diagnostics)
			}
		} else if r.Status != "ok" {
			t.Fatal(recovery, r.Diagnostics)
		}
	}
}

func TestSafeTextAndPatternContextPicturesAgree(t *testing.T) {
	for _, group := range [][]string{{"safe", "reciprocal-safe"}, {"inherited-mode3", "explicit-mode3"}} {
		var want any
		for _, name := range group {
			r, _, output := selectiveRequest(t, readPDF(t, filepath.Join("testdata/text-context", name+".pdf")), "1000x800", app.Build)
			if r.Status != "ok" {
				t.Fatal(name, r.Diagnostics)
			}
			picture := fitRaster(t, output, 1, 144)
			if want == nil {
				want = picture
			} else if !reflect.DeepEqual(want, picture) {
				t.Fatal("safe execution contexts paint differently", group)
			}
		}
	}
}
