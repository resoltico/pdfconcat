// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func fitRaster(t *testing.T, path string, page int, dpi float64) image.Image {
	t.Helper()
	tools := oracle.RequireTools(t)
	prefix := filepath.Join(t.TempDir(), "render")
	output, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-cropbox", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-singlefile", "-r", strconv.FormatFloat(dpi, 'f', -1, 64), "-png", path, prefix).CombinedOutput()
	if err != nil {
		t.Fatalf("render: %v %s", err, output)
	}
	data, err := os.ReadFile(prefix + ".png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func fitOutput(t *testing.T, doc *pdffixture.Doc, target packet.Size, repeats int) string {
	t.Helper()
	docs := make([]*pdffixture.Doc, repeats)
	for i := range docs {
		docs[i] = doc
	}
	path, _, ds := assembleDocs(t, docs, &target)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	tools := oracle.RequireTools(t)
	loaded, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range loaded.Verify(oracle.Expectation{}) {
		if finding.Check == oracle.CheckStructure {
			t.Fatal(finding)
		}
	}
	return path
}

// Source rasterization at the independently calculated fit scale supplies the
// expected artwork. No expected PDF passes through the fitter.
func TestFitPatternDefaultSpacesAndSharedOccurrences(t *testing.T) {
	for _, shading := range []bool{false, true} {
		for _, target := range []packet.Size{{Width: 300, Height: 500}, {Width: 50, Height: 100}} {
			t.Run(fmt.Sprintf("shading=%v/width=%g", shading, target.Width), func(t *testing.T) {
				doc := pdffixture.FitPatterns(shading)
				source := filepath.Join(t.TempDir(), "source.pdf")
				if err := os.WriteFile(source, doc.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				output := fitOutput(t, doc, target, 2)
				for page := 1; page <= 4; page++ {
					sourcePage := (page-1)%2 + 1
					width, height := 100.0, 100.0
					if sourcePage == 2 {
						height = 200
					}
					scale := math.Min(target.Width/width, target.Height/height)
					// A higher-resolution grid bounds device-pixel quantization
					// at tiling boundaries without relaxing physical fidelity.
					const rasterScale = 3.0
					expected := fitRaster(t, source, sourcePage, 72*scale*rasterScale)
					actual := fitRaster(t, output, page, 72*rasterScale)
					left, top := int(math.Round((target.Width-width*scale)/2*rasterScale)), int(math.Round((target.Height-height*scale)/2*rasterScale))
					mismatches, total := patternPixelDifference(expected, actual, left, top)
					// Allow antialiasing differences at pattern and Form edges.
					if float64(mismatches)/float64(total) > .02 {
						t.Fatalf("page %d pattern scale/phase/orientation: %d/%d different", page, mismatches, total)
					}
					if !shading && target.Width == 300 && page == 1 {
						shifted := pdffixture.FitPatterns(false)
						// The fit scale is 3: changing pattern y translation by
						// 1/3 source unit introduces a real one-point phase error.
						shifted.Objs[5] = bytes.Replace(shifted.Objs[5], []byte("0 1 -1 0 7 3"), []byte("0 1 -1 0 7 3.3333333333333335"), 1)
						wrong := fitOutput(t, shifted, target, 2)
						bad := fitRaster(t, wrong, 1, 72*rasterScale)
						different, count := patternPixelDifference(expected, bad, left, top)
						if float64(different)/float64(count) <= .02 {
							t.Fatalf("oracle accepted a one-point pattern phase error: %d/%d", different, count)
						}
					}

				}
			})
		}
	}
}

func patternPixelDifference(expected, actual image.Image, left, top int) (mismatches, total int) {
	for y := 2; y < expected.Bounds().Dy()-2; y++ {
		for x := 2; x < expected.Bounds().Dx()-2; x++ {
			ar, ag, ab, _ := actual.At(x+left, y+top).RGBA()
			er, eg, eb, _ := expected.At(x, y).RGBA()
			if math.Abs(float64(ar)-float64(er)) > 24*257 || math.Abs(float64(ag)-float64(eg)) > 24*257 || math.Abs(float64(ab)-float64(eb)) > 24*257 {
				mismatches++
			}
			total++
		}
	}
	return mismatches, total
}

func fitAnnotationObjects(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw := run(t, qpdfTool, "--json", path)
	var dump struct {
		QPDF  []map[string]json.RawMessage `json:"qpdf"`
		Pages []struct {
			Object string `json:"object"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatal(err)
	}
	var deref func(any) any
	deref = func(value any) any {
		if ref, ok := value.(string); ok {
			if object, exists := dump.QPDF[1]["obj:"+ref]; exists {
				var entry map[string]any
				if err := json.Unmarshal(object, &entry); err != nil {
					t.Fatal(err)
				}
				if v, ok := entry["value"]; ok {
					return deref(v)
				}
			}
		}
		return value
	}
	var annotations []map[string]any
	for _, page := range dump.Pages {
		dict, ok := deref(page.Object).(map[string]any)
		if !ok {
			t.Fatal("page dictionary missing")
		}
		annots, ok := deref(dict["/Annots"]).([]any)
		if !ok {
			t.Fatal("annotations missing")
		}
		for _, a := range annots {
			d, ok := deref(a).(map[string]any)
			if !ok {
				t.Fatal("annotation dictionary missing")
			}
			for k, v := range d {
				d[k] = deref(v)
			}
			annotations = append(annotations, d)
		}
	}
	return annotations
}

func fitWantNumbers(t *testing.T, value any, want []float64) {
	t.Helper()
	values, ok := value.([]any)
	if !ok || len(values) != len(want) {
		t.Fatalf("coordinates: got %v want %v", value, want)
	}
	for i, v := range values {
		n, ok := v.(float64)
		if !ok || math.Abs(n-want[i]) > 1e-7 {
			t.Fatalf("coordinates: got %v want %v", value, want)
		}
	}
}

func TestFitAnnotationSemanticGeometry(t *testing.T) {
	for _, tc := range []struct {
		subtype, field, geometry string
		points                   []float64
	}{
		{"Line", "/L", "/L [10 20 80 90] /LL 3 /LLE 4 /LLO 5 /CO [6 7]", []float64{10, 20, 80, 90}},
		{"Ink", "/InkList", "/InkList [[10 20 40 50 80 90]]", []float64{10, 20, 40, 50, 80, 90}},
		{"Polygon", "/Vertices", "/Vertices [10 20 80 90 20 70]", []float64{10, 20, 80, 90, 20, 70}},
		{"PolyLine", "/Vertices", "/Vertices [10 20 80 90 20 70]", []float64{10, 20, 80, 90, 20, 70}},
		{"FreeText", "/CL", "/CL [10 20 40 50 80 90] /DA (/Helvetica 10 Tf 0 g)", []float64{10, 20, 40, 50, 80, 90}},
		{"Link", "/QuadPoints", "/QuadPoints [10 90 90 90 10 10 90 10] /A << /S /URI /URI (https://example.invalid/) >>", []float64{10, 90, 90, 90, 10, 10, 90, 10}},
	} {
		for _, rotation := range []int{0, 90, 180, 270, 450, -90} {
			for _, ap := range []bool{false, true} {
				if tc.subtype == "FreeText" && !ap {
					continue
				}
				t.Run(fmt.Sprintf("%s/rotate=%d/AP=%v", tc.subtype, rotation, ap), func(t *testing.T) {
					doc := pdffixture.FitAnnotation(tc.subtype, tc.geometry, ap, rotation, 4)
					output := fitOutput(t, doc, packet.Size{Width: 300, Height: 500}, 2)
					want := make([]float64, len(tc.points))
					for i := 0; i < len(want); i += 2 {
						x, y := tc.points[i], tc.points[i+1]
						switch (rotation%360 + 360) % 360 {
						case 90:
							x, y = y, 100-x
						case 180:
							x, y = 100-x, 100-y
						case 270:
							x, y = 100-y, x
						}
						want[i], want[i+1] = 3*x, 100+3*y
					}
					annotations := fitAnnotationObjects(t, output)
					if len(annotations) != 2 {
						t.Fatalf("got %d annotations, want two", len(annotations))
					}
					for _, a := range annotations {
						if bs, ok := a["/BS"].(map[string]any); !ok || bs["/W"] != float64(6) {
							t.Fatalf("border width: %v", a["/BS"])
						}
						if tc.subtype == "Line" {
							for key, want := range map[string]float64{"/LL": 9, "/LLE": 12, "/LLO": 15} {
								if a[key] != want {
									t.Fatalf("%s got %v want %g", key, a[key], want)
								}
							}
							fitWantNumbers(t, a["/CO"], []float64{18, 21})
						}
						value := a[tc.field]
						if tc.subtype == "Ink" {
							strokes, ok := value.([]any)
							if !ok || len(strokes) != 1 {
								t.Fatal(value)
							}
							value = strokes[0]
						}
						fitWantNumbers(t, value, want)
						fitWantNumbers(t, a["/Rect"], []float64{30, 130, 270, 370})
						if a["/Subtype"] != "/"+tc.subtype {
							t.Fatal("subtype changed")
						}
						if tc.subtype == "Link" && a["/A"] == nil {
							t.Fatal("link action lost")
						}
						if ap && a["/AP"] == nil {
							t.Fatal("appearance lost")
						}
					}
				})
			}
		}
	}
}

func TestFitAnnotationInsetSides(t *testing.T) {
	for _, subtype := range []string{"Square", "Circle", "Caret"} {
		for _, ap := range []bool{false, true} {
			if subtype == "Caret" && !ap {
				continue
			} // Its fallback symbol needs an appearance; tested as a compatibility refusal.
			for rotation, want := range map[int][]float64{0: {6, 8, 12, 16}, 90: {16, 6, 8, 12}, 180: {12, 16, 6, 8}, 270: {8, 12, 16, 6}} {
				for _, scale := range []float64{2, .5} {
					t.Run(fmt.Sprintf("%s/AP=%v/rotate=%d/scale=%g", subtype, ap, rotation, scale), func(t *testing.T) {
						source := pdffixture.FitAnnotation(subtype, "/RD [3 4 6 8]", ap, rotation, 4)
						if subtype == "Caret" {
							insertEntry(source, 6, "/Sy /P")
						}
						output := fitOutput(t, source, packet.Size{Width: 100 * scale, Height: 150 * scale}, 1)
						annotations := fitAnnotationObjects(t, output)
						if len(annotations) != 1 {
							t.Fatalf("got %d annotations", len(annotations))
						}
						annotation := annotations[0]
						expected := append([]float64{}, want...)
						for i := range expected {
							expected[i] *= scale / 2
						}
						fitWantNumbers(t, annotation["/RD"], expected)
						fitWantNumbers(t, annotation["/Rect"], []float64{10 * scale, 35 * scale, 90 * scale, 115 * scale})
						if annotation["/Subtype"] != "/"+subtype {
							t.Fatal("subtype lost")
						}
						if subtype == "Caret" && annotation["/Sy"] != "/P" {
							t.Fatal("caret symbol lost")
						}
					})
				}
			}
		}
	}
}

// Compare the entire fitted sheet to independently rendered source pixels plus
// white margins, so deleting all artwork or exposing marks in margins fails.
func fitCompareSheet(t *testing.T, source, output string, scale float64, margin int) {
	t.Helper()
	expected := fitRaster(t, source, 1, 72*scale)
	actual := fitRaster(t, output, 1, 72)
	mismatches := 0
	for y := range actual.Bounds().Dy() {
		for x := range actual.Bounds().Dx() {
			er, eg, eb := uint32(65535), uint32(65535), uint32(65535)
			if image.Pt(x, y-margin).In(expected.Bounds()) {
				er, eg, eb, _ = expected.At(x, y-margin).RGBA()
			}
			ar, ag, ab, _ := actual.At(x, y).RGBA()
			if math.Abs(float64(ar)-float64(er)) > 24*257 || math.Abs(float64(ag)-float64(eg)) > 24*257 || math.Abs(float64(ab)-float64(eb)) > 24*257 {
				mismatches++
			}
		}
	}
	if float64(mismatches)/float64(actual.Bounds().Dx()*actual.Bounds().Dy()) > .005 {
		t.Fatalf("source display differs: %d pixels", mismatches)
	}
}

func TestFitSourceBoundaryAndContentStreams(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		for _, kind := range []string{"crop", "media", "oversized", "inherited", "streams", "save"} {
			t.Run(fmt.Sprintf("%s/rotate=%d", kind, rotation), func(t *testing.T) {
				doc := pdffixture.FitMarks("")
				switch kind {
				case "streams":
					doc = pdffixture.FitMarkStreams()
				case "save":
					doc = pdffixture.FitMarks("q\n")
				case "media":
					doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("/MediaBox [0 0 100 100] /Resources"), []byte("/MediaBox [25 25 75 75] /Resources"))
					doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte(" /CropBox [25 25 75 75]"), nil)
				case "oversized":
					doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("/MediaBox [0 0 100 100]"), []byte("/MediaBox [25 25 75 75]"))
					doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("/CropBox [25 25 75 75]"), []byte("/CropBox [25 25 125 75]"))
				case "inherited":
					doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte(" /CropBox [25 25 75 75]"), nil)
					insertEntry(doc, 2, "/CropBox [25 25 75 75]")
				}
				insertEntry(doc, 3, fmt.Sprintf("/Rotate %d", rotation))
				source := filepath.Join(t.TempDir(), "source.pdf")
				if err := os.WriteFile(source, doc.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				for _, scale := range []float64{.5, 6} {
					size := packet.Size{Width: 50 * scale, Height: 70 * scale}
					output := fitOutput(t, doc, size, 1)
					fitCompareSheet(t, source, output, scale, int(10*scale))
				}
			})
		}
	}
}

func TestFitAppearanceViewerFlagsAndTextException(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		for _, subtype := range []string{"Line", "Text"} {
			for _, flags := range []int{12, 28} {
				t.Run(fmt.Sprintf("rotate=%d/%s/flags=%d", rotation, subtype, flags), func(t *testing.T) {
					doc := pdffixture.FitAnnotation(subtype, "/Contents (viewer flags)", true, rotation, flags)
					if subtype == "Line" {
						insertEntry(doc, 6, "/L [10 10 90 90]")
					}
					source := filepath.Join(t.TempDir(), "source.pdf")
					if err := doc.WriteFile(source); err != nil {
						t.Fatal(err)
					}
					// Compare at one display zoom to isolate the NoRotate/Text pivot;
					// NoZoom itself remains set for subsequent reader magnification.
					output := fitOutput(t, doc, packet.Size{Width: 100, Height: 200}, 2)
					fitCompareSheet(t, source, output, 1, 50)
					annotations := fitAnnotationObjects(t, output)
					if len(annotations) != 2 {
						t.Fatalf("annotation count: %d", len(annotations))
					}
					for _, a := range annotations {
						if a["/F"] != float64(flags) || a["/AS"] != "/On" {
							t.Fatal("viewer flags/state lost")
						}
					}
				})
			}
		}
	}
}

func TestFitMissingAppearanceNoRotatePath(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(strconv.Itoa(rotation), func(t *testing.T) {
			doc := pdffixture.FitAnnotation("Line", "/L [40 40 60 50]", false, rotation, 20)
			doc.Objs[5] = bytes.ReplaceAll(doc.Objs[5], []byte("/Rect [10 10 90 90]"), []byte("/Rect [38 38 62 52]"))
			source := filepath.Join(t.TempDir(), "source.pdf")
			if err := doc.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			if rotation != 0 {
				_, _, ds := assembleDocs(t, []*pdffixture.Doc{doc}, &packet.Size{Width: 300, Height: 500})
				if !codePresent(ds, diag.FitAnnotationUnsupported) {
					t.Fatal(ds)
				}
				return
			}
			output := fitOutput(t, doc, packet.Size{Width: 300, Height: 500}, 1)
			fitCompareSheet(t, source, output, 3, 100)
		})
	}
}

func TestFitAppearanceInheritsPageResourcesWithoutNameCollision(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(strconv.Itoa(rotation), func(t *testing.T) {
			doc := pdffixture.FitAnnotation("FreeText", "/Contents (inherited font) /DA (/F1 10 Tf 1 0 0 rg)", true, rotation, 4)
			// Both the font and the misleadingly named /Original Form are inherited
			// from page resources. The wrapper must neither hide nor overwrite them.
			doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("/Resources << /Font << /F1 5 0 R >> >>"), []byte("/Resources << /Font << /F1 5 0 R >> /XObject << /Original 8 0 R >> >>"))
			content := "BT /F1 10 Tf 1 0 0 rg 4 20 Td (Hello world) Tj ET /Original Do"
			doc.Objs[6] = []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Length %d >>\nstream\n%s\nendstream", len(content), content))
			nested := "0 0 1 rg 4 4 20 8 re f"
			doc.Objs = append(doc.Objs, []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(nested), nested)))
			source := filepath.Join(t.TempDir(), "source.pdf")
			if err := doc.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			output := fitOutput(t, doc, packet.Size{Width: 300, Height: 500}, 2)
			fitCompareSheet(t, source, output, 3, 100)
		})
	}
}

func TestFitResourceOwnershipDuringMerge(t *testing.T) {
	shared := pdffixture.Pages("shared font", 2)
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{pdffixture.Plain("preceding"), shared, shared}, &packet.Size{Width: 300, Height: 500})
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	doc, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"preceding", "shared font p1", "shared font p2", "shared font p1", "shared font p2"}
	if doc.PageCount() != len(want) {
		t.Fatalf("got %d pages want %d", doc.PageCount(), len(want))
	}
	for i, text := range want {
		if doc.FirstLine(i+1) != text {
			t.Fatalf("page %d: %q want %q", i+1, doc.FirstLine(i+1), text)
		}
	}
}

func TestFitLinkBorderRadiiAndDashes(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(strconv.Itoa(rotation), func(t *testing.T) {
			doc := pdffixture.FitAnnotation("Link", "/A << /S /URI /URI (https://example.invalid/) >>", false, rotation, 4)
			doc.Objs[5] = bytes.ReplaceAll(doc.Objs[5], []byte("/BS << /W 2 >>"), []byte("/Border [2 6 3 [4 5]]"))
			output := fitOutput(t, doc, packet.Size{Width: 300, Height: 500}, 1)
			a := fitAnnotationObjects(t, output)[0]
			border, ok := a["/Border"].([]any)
			if !ok || len(border) != 4 {
				t.Fatal(a)
			}
			want := []float64{6, 18, 9}
			if rotation%180 != 0 {
				want[0], want[1] = want[1], want[0]
			}
			fitWantNumbers(t, border[:3], want)
			fitWantNumbers(t, border[3], []float64{12, 15})
		})
	}
}

func TestFitAppearanceRectangleCornerOrder(t *testing.T) {
	for _, rect := range []string{"[10 10 90 90]", "[90 90 10 10]", "[90 10 10 90]", "[10 90 90 10]"} {
		for _, bbox := range []string{"[0 0 80 80]", "[80 80 0 0]"} {
			t.Run(rect+"/"+bbox, func(t *testing.T) {
				doc := pdffixture.FitAnnotation("Square", "", true, 0, 4)
				doc.Objs[5] = bytes.ReplaceAll(doc.Objs[5], []byte("[10 10 90 90]"), []byte(rect))
				program := "1 0 0 rg 5 5 m 70 5 l 5 60 l h f"
				doc.Objs[6] = []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox %s /Matrix [1 .2 .1 1 0 0] /Resources << >> /Length %d >>\nstream\n%s\nendstream", bbox, len(program), program))
				source := filepath.Join(t.TempDir(), "source.pdf")
				if err := doc.WriteFile(source); err != nil {
					t.Fatal(err)
				}
				output := fitOutput(t, doc, packet.Size{Width: 300, Height: 500}, 1)
				fitCompareSheet(t, source, output, 3, 100)
				annotations := fitAnnotationObjects(t, output)
				if len(annotations) != 1 {
					t.Fatal("annotation lost")
				}
				fitWantNumbers(t, annotations[0]["/Rect"], []float64{30, 130, 270, 370})
			})
		}
	}
}
