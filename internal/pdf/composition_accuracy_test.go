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

func TestSourceCompositionPreservesKeepAndRefusesLostFittingOffset(t *testing.T) {
	paths := []string{filepath.Join("testdata/composition-accuracy", "plain.pdf"), filepath.Join("testdata/composition-accuracy", "cancelled-translations.pdf")}
	for _, path := range paths {
		qpdfCheck(t, path)
	}
	plain, cancelled := fitRaster(t, paths[0], 1, 144), fitRaster(t, paths[1], 1, 144)
	if !reflect.DeepEqual(plain, cancelled) {
		t.Fatal("source identity control differs")
	}
	for i, path := range paths {
		for _, target := range []string{"600x800", "1000x800"} {
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, output := selectiveRequest(t, readPDF(t, path), target, op)
				if i == 1 && target == "1000x800" {
					if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitGeometryUnsupported) || result.Output.Published {
						t.Fatal("lost fitting offset accepted", op, result.Diagnostics)
					}
					continue
				}
				if result.Status != "ok" {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, output)
					if target == "600x800" && !reflect.DeepEqual(plain, fitRaster(t, output, 1, 144)) {
						t.Fatal("keep changed valid source painting")
					}
				}
			}
		}
	}
}

func compositionDocument(program string) *pdffixture.Doc {
	d := pdffixture.Plain("composition")
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("612 792"), []byte("600 800"))
	d.Objs[3] = []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program))
	return d
}
func TestCompositionTracksRestorationAndInvocationState(t *testing.T) {
	blue := "0 0 1 rg 0 0 300 400 re f\n"
	for _, tc := range []struct {
		name, program string
		unsafe        bool
	}{
		{"safe", "q 1 0 0 1 10 20 cm 1 0 0 1 -10 -20 cm\n" + blue + "Q", false},
		{"restored", "q 1 0 0 1 -100000000000000000000.0 0 cm Q\n" + blue, false},
		{"nested-restored", "q\n" + "q 1 0 0 1 -100000000000000000000.0 0 cm Q\n" + "Q\n" + blue, false},
		{"unsafe", "q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm\n" + blue + "Q", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compositionDocument(tc.program)
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, output := selectiveRequest(t, d.Bytes(), "1000x800", op)
				if tc.unsafe {
					if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
						t.Fatal(r.Diagnostics)
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
		})
	}
	for _, unsafe := range []bool{false, true} {
		program := "q /Outer Do Q"
		d := compositionDocument(program)
		// Form6 invokes shared Form7; proofs must follow the invocation state and
		// include each declared Form matrix, rather than caching by object ref.
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Resources <<"), []byte("/Resources << /XObject << /Outer 6 0 R >>"))
		matrix := "[1 0 0 1 10 20]"
		inner := "q 1 0 0 1 -10 -20 cm " + blue + "Q"
		if unsafe {
			matrix = "[1 0 0 1 -100000000000000000000.0 0]"
			inner = "q 1 0 0 1 100000000000000000000.0 0 cm " + blue + "Q"
		}
		outer := "/Inner Do"
		d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 600 800] /Matrix %s /Resources << /XObject << /Inner 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream", matrix, len(outer), outer)), []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 600 800] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(inner), inner)))
		for _, op := range []app.Operation{app.Check, app.Build} {
			r, _, output := selectiveRequest(t, d.Bytes(), "1000x800", op)
			if unsafe {
				if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
					t.Fatal(r.Diagnostics)
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

func TestSharedFormIsCertifiedPerIncomingStateAndKeptPagesDoNotVeto(t *testing.T) {
	blue := "0 0 1 rg 0 0 300 400 re f"
	d := compositionDocument("q /Shared Do Q q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm /Shared Do Q")
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Resources <<"), []byte("/Resources << /XObject << /Shared 6 0 R >>"))
	d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 600 800] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(blue), blue)))
	r, _, _ := selectiveRequest(t, d.Bytes(), "1000x800", app.Check)
	if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) {
		t.Fatal("reference-only certificate reused", r.Diagnostics)
	}
	// A risky first page already matches; only an unrelated safe second page
	// moves. Preserve original ordinal attribution and skip kept programs.
	d = compositionDocument("q 1 0 0 1 -100000000000000000000.0 0 cm 1 0 0 1 100000000000000000000.0 0 cm " + blue + " Q")
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("600 800"), []byte("1000 800"))
	d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 6 0 R] >>")
	d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Resources << >> /Contents 7 0 R >>"), []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(blue), blue)))
	r, _, output := selectiveRequest(t, d.Bytes(), "1000x800", app.Build)
	if r.Status != "ok" {
		t.Fatal("kept program vetoed another page", r.Diagnostics)
	}
	qpdfCheck(t, output)
}
func TestSafeCompositionFitMatchesIndependentPlainPicture(t *testing.T) {
	plain := compositionDocument("q 0 0 1 rg 0 0 300 400 re f Q")
	safe := compositionDocument("q 1 0 0 1 10 20 cm 1 0 0 1 -10 -20 cm 0 0 1 rg 0 0 300 400 re f Q")
	a, _, one := selectiveRequest(t, plain.Bytes(), "1000x800", app.Build)
	b, _, two := selectiveRequest(t, safe.Bytes(), "1000x800", app.Build)
	if a.Status != "ok" || b.Status != "ok" {
		t.Fatal(a.Diagnostics, b.Diagnostics)
	}
	if !reflect.DeepEqual(fitRaster(t, one, 1, 144), fitRaster(t, two, 1, 144)) {
		t.Fatal("safe composition changed fitted picture")
	}
}
