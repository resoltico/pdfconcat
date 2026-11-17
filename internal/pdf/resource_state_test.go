// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestSelectedResourceBindingsChannelsAndRoles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unsafe bool
	}{
		{"type3-tf-safe", false}, {"type3-tf-unsafe", true}, {"type3-gs-safe", false}, {"type3-gs-unsafe", true},
		{"unsafe-inherited-font-safe-shadow", true}, {"safe-inherited-font-unsafe-shadow", false},
		{"inherited-safe-pattern-safe-shadow", false}, {"inherited-safe-pattern-unsafe-shadow", false},
		{"hidden-ap", false}, {"visible-ap", true}, {"unused-fill-pattern-stroke", false}, {"used-fill-pattern", true},
		{"type1-control", false}, {"type1-private-matrix", false}, {"type3-private-charproc-matrix", false}, {"plain-stroke-control", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata/resource-state", tc.name+".pdf")
			qpdfCheck(t, path)
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, output := selectiveRequest(t, readPDF(t, path), "1000x800", op)
				if tc.unsafe {
					if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) || r.Output.Published {
						t.Fatal("unsafe resource selection accepted", r.Diagnostics)
					}
				} else {
					if r.Status != "ok" {
						t.Fatal("unused/wrong-role resource vetoed execution", r.Diagnostics)
					}
					if op == app.Build {
						qpdfCheck(t, output)
					}
				}
			}
		})
	}
}

func TestHiddenAppearanceDoesNotHideLaterVisibleUnsafeAppearance(t *testing.T) {
	d := pdffixture.FitAnnotation("Square", "", true, 0, 2)
	risky := "q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm 0 0 1 rg 0 0 80 40 re f Q"
	d.Objs[6] = []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(risky), risky))
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Annots [6 0 R]"), []byte("/Annots [6 0 R 8 0 R]"))
	visible := bytes.ReplaceAll(d.Objs[5], []byte("/F 2"), []byte("/F 4"))
	d.Objs = append(d.Objs, visible)
	r, _, _ := selectiveRequest(t, d.Bytes(), "300x200", app.Check)
	if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
		t.Fatal("hidden first annotation skipped later visible execution", r.Diagnostics)
	}
}
func TestResourceRoleAndSafeBindingPicturesMatch(t *testing.T) {
	groups := [][]string{
		{"type3-tf-safe", "type3-gs-safe", "safe-inherited-font-unsafe-shadow", "type3-private-charproc-matrix"},
		{"type1-control", "type1-private-matrix"},
		{"inherited-safe-pattern-safe-shadow", "inherited-safe-pattern-unsafe-shadow"},
		{"plain-stroke-control", "unused-fill-pattern-stroke"},
	}
	for _, group := range groups {
		var control any
		for _, name := range group {
			path := filepath.Join("testdata/resource-state", name+".pdf")
			r, _, output := selectiveRequest(t, readPDF(t, path), "1000x800", app.Build)
			if r.Status != "ok" {
				t.Fatal(name, r.Diagnostics)
			}
			qpdfCheck(t, output)
			picture := fitRaster(t, output, 1, 144)
			if control == nil {
				control = picture
			} else if !reflect.DeepEqual(control, picture) {
				t.Fatal("ignored/shadowed state changed the fitted picture", group, name)
			}
		}
	}
}

func TestPatternsExecuteOnlyForSelectedPaintChannelsAndRestoredState(t *testing.T) {
	pattern := "q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm 0 0 1 rg 0 0 10 10 re f Q"
	for _, tc := range []struct {
		name, program string
		unsafe        bool
	}{
		{"unused-fill", "/Pattern cs /P1 scn 0 0 300 400 re S", false},
		{"unused-stroke", "/Pattern CS /P1 SCN 0 0 300 400 re f", false},
		{"fill", "/Pattern cs /P1 scn 0 0 300 400 re f", true},
		{"stroke", "/Pattern CS /P1 SCN 0 0 300 400 re S", true},
		{"combined", "/Pattern cs /P1 scn 0 0 300 400 re B", true},
		{"text-stroke-only", "/Pattern cs /P1 scn BT /F1 12 Tf 1 Tr (A) Tj ET", false},
		{"text-fill-only", "/Pattern CS /P1 SCN BT /F1 12 Tf 0 Tr (A) Tj ET", false},
		{"text-invisible", "/Pattern cs /P1 scn BT /F1 12 Tf 3 Tr (A) Tj ET", false},
		{"text-both", "/Pattern cs /P1 scn BT /F1 12 Tf 2 Tr (A) Tj ET", true},
		{"reset-color", "/Pattern cs /P1 scn 0 0 1 rg 0 0 300 400 re f", false},
		{"restore-pattern", "/Pattern cs /P1 scn q 0 0 1 rg Q 0 0 300 400 re f", true},
		{"restore-color", "q /Pattern cs /P1 scn Q 0 0 300 400 re f", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compositionDocument(tc.program)
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Resources <<"), []byte("/Resources << /Pattern << /P1 6 0 R >>"))
			d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(pattern), pattern)))
			r, _, output := selectiveRequest(t, d.Bytes(), "1000x800", app.Build)
			if tc.unsafe {
				if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
					t.Fatal("used pattern not checked", r.Diagnostics)
				}
			} else {
				if r.Status != "ok" {
					t.Fatal("unused pattern executed", r.Diagnostics)
				}
				qpdfCheck(t, output)
			}
		})
	}
}

func TestFontSelectionRestoresAcrossQAndGs(t *testing.T) {
	blue := "600 0 d0 q 0 0 1 rg 0 0 300 400 re f Q"
	bad := "600 0 d0 q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm 0 0 1 rg 0 0 300 400 re f Q"
	font := func(glyph int) []byte {
		return []byte(fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 600 800] /FontMatrix [1 0 0 1 0 0] /FirstChar 65 /LastChar 65 /Widths [600] /Encoding << /Type /Encoding /Differences [65 /A] >> /CharProcs << /A %d 0 R >> /Resources << >> >>", glyph))
	}
	stream := func(program string) []byte {
		return []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program))
	}
	for _, tc := range []struct {
		program string
		unsafe  bool
	}{
		{"/Good gs q /Bad gs Q", false}, {"/Bad gs q /Good gs Q", true},
		{"/Good gs q /Fbad 1 Tf Q", false}, {"/Fsafe 1 Tf q /Bad gs Q", false},
		{"/Bad gs /Fsafe 1 Tf", false},
	} {
		program := tc.program + " BT 1 0 0 1 0 0 Tm (A) Tj ET"
		d := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
			[]byte("<< /Type /Catalog /Pages 2 0 R >>"), []byte("<< /Type /Pages /Count 1 /Kids [3 0 R] >>"),
			[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Contents 4 0 R /Resources << /Font << /Fsafe 5 0 R /Fbad 8 0 R >> /ExtGState << /Good 6 0 R /Bad 10 0 R >> >> >>"),
			stream(program), font(7), []byte("<< /Type /ExtGState /Font [5 0 R 1] >>"), stream(blue), font(9), stream(bad), []byte("<< /Type /ExtGState /Font [8 0 R 1] >>"),
		}}
		r, _, output := selectiveRequest(t, d.Bytes(), "1000x800", app.Build)
		if tc.unsafe {
			if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
				t.Fatal("unsafe restored font missed", r.Diagnostics)
			}
		} else {
			if r.Status != "ok" {
				t.Fatal("safe restored font rebound", r.Diagnostics)
			}
			qpdfCheck(t, output)
		}
	}
}
