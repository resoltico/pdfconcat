// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"reflect"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestPageGeometryInheritanceDoesNotLeakAcrossSiblings(t *testing.T) {
	doc := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Count 3 /Kids [3 0 R 4 0 R] /MediaBox [100 100 0 0] /CropBox [10 10 90 90] /Rotate 90 /Resources 10 0 R >>"),
		[]byte("<< /Type /Pages /Parent 2 0 R /Count 2 /Kids [5 0 R 6 0 R] /Rotate 0 >>"),
		[]byte("<< /Type /Pages /Parent 2 0 R /Count 1 /Kids [7 0 R] /CropBox [70 90 20 30] /Resources << >> >>"),
		[]byte("<< /Type /Page /Parent 3 0 R /Contents 8 0 R >>"),
		[]byte("<< /Type /Page /Parent 3 0 R /CropBox null /Rotate null /Resources 14 0 R /Contents 8 0 R >>"),
		[]byte("<< /Type /Page /Parent 4 0 R /MediaBox 11 0 R /Rotate 270 /Contents 8 0 R >>"),
		[]byte("<< /Length 0 >>\nstream\n\nendstream"),
		[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"),
		[]byte("<< /Font << /F1 9 0 R >> >>"),
		[]byte("[0 0 12 0 R 13 0 R]"),
		[]byte("200"), []byte("100"), []byte("null"),
	}}
	s := parsedFixture(t, doc)
	geometries, err := pageGeometries(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(geometries) != 3 {
		t.Fatal("leaf count", len(geometries))
	}
	for index, g := range geometries {
		want := packet.Size{Width: 80, Height: 80}
		rotation := 0
		if index == 2 {
			want = packet.Size{Width: 60, Height: 50}
			rotation = 270
		}
		if g.size != want || g.attrs.Rotate != rotation {
			t.Fatalf("page %d got %v rotate %d", index+1, g.size, g.attrs.Rotate)
		}
		if (len(g.attrs.Resources) == 0) != (index == 2) {
			t.Fatalf("page %d resources: %v", index+1, g.attrs.Resources)
		}
	}
	if geometries[0].box != *types.NewRectangle(10, 10, 90, 90) || geometries[2].box != *types.NewRectangle(20, 30, 70, 90) {
		t.Fatal("incorrect normalized inherited boxes")
	}
}

func TestFitChecksGeometryBeforeMutatingAnyPage(t *testing.T) {
	for _, unit := range []float64{0, -1, 1e308} {
		t.Run(number(unit), func(t *testing.T) {
			s, _, err := readSource(t.Context(), pdffixture.Pages("safe", 3).Bytes())
			if err != nil {
				t.Fatal(err)
			}
			pages, err := rawPages(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			contents := []types.Object{pages[0].dict["Contents"], pages[1].dict["Contents"], pages[2].dict["Contents"]}
			pages[1].dict["UserUnit"] = types.Float(unit)
			err = fitContext(t.Context(), s, &packet.Size{Width: 300, Height: 500})
			if err == nil || diagnostic(err, diag.AssemblyFailed).Code != diag.SourceInvalid {
				t.Fatalf("invalid unit %g: %v", unit, err)
			}
			for i, page := range pages {
				if !reflect.DeepEqual(page.dict["Contents"], contents[i]) {
					t.Fatal("mutated a page before later geometry refusal")
				}
			}
		})
	}
}
