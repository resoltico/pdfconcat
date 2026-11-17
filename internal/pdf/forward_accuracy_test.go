// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"image"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestPaintedLargeOriginPreservesKeepAndRefusesInaccurateFitting(t *testing.T) {
	paths := []string{filepath.Join("testdata/forward-accuracy", "rebased.pdf"), filepath.Join("testdata/forward-accuracy", "large-origin.pdf")}
	for _, path := range paths {
		qpdfCheck(t, path)
	}
	// Independent source equivalence is the control: identical nonempty painting
	// under distinct native origins. A blank page cannot establish this property.
	rebased, translated := fitRaster(t, paths[0], 1, 144), fitRaster(t, paths[1], 1, 144)
	if !reflect.DeepEqual(rebased, translated) {
		t.Fatal("source-equivalence control differs")
	}
	for index, path := range paths {
		data := readPDF(t, path)
		for _, target := range []string{"600x800", "A4"} {
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, output := selectiveRequest(t, data, target, op)
				if index == 1 && target == "A4" {
					if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitGeometryUnsupported) || result.Output.Published {
						t.Fatalf("inaccurate affine accepted: %s %+v", op, result.Diagnostics)
					}
					continue
				}
				if result.Status != "ok" {
					t.Fatal(op, target, result.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, output)
					if index == 0 && target == "A4" {
						picture := fitRaster(t, output, 1, 144)
						blue := image.Rectangle{}
						first := true
						for y := picture.Bounds().Min.Y; y < picture.Bounds().Max.Y; y++ {
							for x := picture.Bounds().Min.X; x < picture.Bounds().Max.X; x++ {
								r, g, b, _ := picture.At(x, y).RGBA()
								if r < 1000 && g < 1000 && b > 60000 {
									if first {
										blue = image.Rect(x, y, x+1, y+1)
										first = false
									} else {
										blue = blue.Union(image.Rect(x, y, x+1, y+1))
									}
								}
							}
						}
						// Independent 144dpi centered-A4 witness from the source
						// quarter-frame rectangle; allow one edge pixel for raster
						// antialiasing, not a geometric or publication tolerance.
						want := image.Rect(0, 842, 595, 1636)
						if first || picture.Bounds() != image.Rect(0, 0, 1191, 1684) || math.Abs(float64(blue.Min.X-want.Min.X)) > 1 || math.Abs(float64(blue.Min.Y-want.Min.Y)) > 1 || math.Abs(float64(blue.Max.X-want.Max.X)) > 1 || math.Abs(float64(blue.Max.Y-want.Max.Y)) > 1 {
							t.Fatal("stable fitted-picture control differs", picture.Bounds(), blue)
						}
					}
					if target == "600x800" && !reflect.DeepEqual(rebased, fitRaster(t, output, 1, 144)) {
						t.Fatal("true keep changed the picture")
					}
				}
			}
		}
	}
}
