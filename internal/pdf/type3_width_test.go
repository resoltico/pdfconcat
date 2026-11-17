// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
)

func TestType3DefaultWidthPackagedWitnesses(t *testing.T) {
	var want any
	for _, name := range []string{"implicit-zero", "explicit-zero"} {
		path := filepath.Join("testdata/type3-width", name+".pdf")
		qpdfCheck(t, path)
		original := fitRaster(t, path, 1, 144)
		if want == nil {
			want = original
		} else if !reflect.DeepEqual(want, original) {
			t.Fatal("sources differ")
		}
		for _, target := range []string{"600x800", "1000x800"} {
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, out := selectiveRequest(t, readPDF(t, path), target, op)
				if r.Status != "ok" {
					t.Fatal(name, target, op, r.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, out)
					if target == "600x800" && !reflect.DeepEqual(original, fitRaster(t, out, 1, 144)) {
						t.Fatal("keep changed picture")
					}
				}
			}
		}
	}
}

func TestType3ZeroDefaultStillAppliesSpacingAndCursorOperators(t *testing.T) {
	for _, tc := range []struct {
		name, show, params string
		code               int
		x                  int
	}{
		{"above-literal", "(BA) Tj", "", 66, 0},
		{"above-hex", "<4241> Tj", "", 66, 0},
		{"below-literal", "(@A) Tj", "", 64, 0},
		{"below-hex", "<4041> Tj", "", 64, 0},
		{"separate", "(B) Tj (A) Tj", "", 66, 0},
		{"TJ", "[(B) (A)] TJ", "", 66, 0},
		{"TJ-adjust", "[(B) -20000 (A)] TJ", "", 66, 20},
		{"char-spacing", "(BA) Tj", "20 Tc", 66, 20},
		{"word-spacing", "( A) Tj", "20 Tw", 32, 20},
		{"font-size-spacing", "(BA) Tj", "/F1 2 Tf 20 Tc", 66, 20},
		{"word-char-scale", "<2041> Tj", "20 Tw 10 Tc 50 Tz", 32, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := textContextDoc(fmt.Sprintf("BT /F1 1 Tf %s 1 0 0 1 %d 0 Tm (A) Tj ET", tc.params, tc.x))
			rr, _, reference := selectiveRequest(t, control.Bytes(), "1000x800", app.Build)
			if rr.Status != "ok" {
				t.Fatal(rr.Diagnostics)
			}
			want := fitRaster(t, reference, 1, 144)
			for _, explicit := range []bool{false, true} {
				d := textContextDoc("BT /F1 1 Tf " + tc.params + " " + tc.show + " ET")
				first, last, widths := 65, 65, "600"
				if explicit {
					if tc.code < 65 {
						first = tc.code
						widths = strings.Repeat("0 ", 65-tc.code) + "600"
					} else {
						last = tc.code
						widths = "600 " + strings.Repeat("0 ", tc.code-65)
					}
				}
				d.Objs[4] = []byte(fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 600 800] /FontMatrix [1 0 0 1 0 0] /FirstChar %d /LastChar %d /Widths [%s] /Encoding << /Type /Encoding /Differences [%d /B 65 /A] >> /CharProcs << /A 6 0 R /B 7 0 R >> /Resources << >> >>", first, last, widths, tc.code))
				d.Objs = append(d.Objs, []byte("<< /Length 6 >>\nstream\n0 0 d0\nendstream"))
				r, _, out := selectiveRequest(t, d.Bytes(), "1000x800", app.Build)
				if r.Status != "ok" {
					t.Fatal(explicit, r.Diagnostics)
				}
				qpdfCheck(t, out)
				if !reflect.DeepEqual(want, fitRaster(t, out, 1, 144)) {
					t.Fatal("wrong cursor picture", explicit)
				}
			}
		})
	}
}
