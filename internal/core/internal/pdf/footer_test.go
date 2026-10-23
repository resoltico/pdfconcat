// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func preparedFooter(t *testing.T) (map[typeset.Digest]typeset.EmbeddedFont, map[typeset.Digest]string) {
	t.Helper()
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	digits, err := typeset.NewDigits(font, 9)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := digits.Place("0123456789", 10*digits.CellWidth, 0)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := typeset.PrepareFonts([]*typeset.Placed{placed})
	if err != nil {
		t.Fatal(err)
	}
	names, err := generatedFontNames(prepared, nil)
	if err != nil {
		t.Fatal(err)
	}
	return prepared, names
}
func footerLayout(count int) packet.Layout {
	return packet.Layout{PageNumbers: &packet.PageNumbers{Size: 9, Margin: 24}, Parts: []packet.Part{{Leaf: packet.Leaf{Ref: "/items/0", Kind: "source"}, FirstPage: 1, LastPage: int64(count)}}}
}
func writeFooterContext(t *testing.T, s *model.Context, name string) string {
	t.Helper()
	var out bytes.Buffer
	if err := api.WriteContext(t.Context(), s, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name+".pdf")
	if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func footerRaster(t *testing.T, path string) image.Image {
	t.Helper()
	tools := oracle.RequireTools(t)
	prefix := filepath.Join(t.TempDir(), "page")
	out, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-cropbox", "-singlefile", "-r", "144", "-png", path, prefix).CombinedOutput()
	if err != nil {
		t.Fatalf("raster: %v %s", err, out)
	}
	data, err := os.ReadFile(prefix + ".png")
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
func TestPhysicalFootersPreserveSourceAndPostFitGeometryStreamsAndPixels(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, topOutside := range []bool{false, true} {
		crop := "[0 30 310 430]"
		if topOutside {
			crop = "[0 30 310 460]"
		}
		for _, rotation := range []int{0, 90, 180, 270, -90, 450} {
			for _, fit := range []bool{false, true} {
				t.Run(fmt.Sprintf("topOutside%v/rotate%d/fit%v", topOutside, rotation, fit), func(t *testing.T) {
					fixture := pdffixture.WithBoxes("SOURCE", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[10 20 330 440]", Crop: crop, Rotate: strconv.Itoa(rotation), UserUnit: "2"})
					s, err := read(t.Context(), fixture.Bytes())
					if err != nil {
						t.Fatal(err)
					}
					d, pageRef, _, err := s.PageDict(t.Context(), 1, false)
					if err != nil {
						t.Fatal(err)
					}
					annotation := types.Dict{"Type": types.Name("Annot"), "Subtype": types.Name("Link"), "Rect": types.NewNumberArray(90, 120, 130, 150), "Dest": types.Array{*pageRef, types.Name("Fit")}, "P": *pageRef}
					annotationRef, err := s.IndRefForNewObject(annotation)
					if err != nil {
						t.Fatal(err)
					}
					d["Annots"] = types.Array{*annotationRef}
					// Retain legal transformed/clipped graphics state through the original stream.
					originalObject := d["Contents"]
					original, _, err := s.DereferenceStreamDict(originalObject)
					if err != nil {
						t.Fatal(err)
					}
					if err = original.Decode(); err != nil {
						t.Fatal(err)
					}
					original.Content = append([]byte("1 0 0 1 3 4 cm 20 30 280 350 re W n\n1 0 0 rg 90 120 30 40 re f\n"), original.Content...)
					if err = original.Encode(); err != nil {
						t.Fatal(err)
					}
					contentRef, ok := originalObject.(types.IndirectRef)
					if !ok {
						t.Fatal("source stream is not indirect")
					}
					s.Table[contentRef.ObjectNumber.Value()].Object = *original
					if fit {
						if err = fitContext(t.Context(), s, &packet.Size{Width: 400, Height: 500}); err != nil {
							t.Fatal(err)
						}
					}

					baseline := writeFooterContext(t, s, "baseline")
					// Native serialization owns mutable write state. Reopen the
					// immediate pre-footer baseline before independently writing it.
					baselineBytes, err := os.ReadFile(baseline)
					if err != nil {
						t.Fatal(err)
					}
					s, err = read(t.Context(), baselineBytes)
					if err != nil {
						t.Fatal(err)
					}
					d, _, _, err = s.PageDict(t.Context(), 1, false)
					if err != nil {
						t.Fatal(err)
					}
					annotationArray, err := s.DereferenceArray(d["Annots"])
					if err != nil || len(annotationArray) != 1 {
						t.Fatalf("annotation baseline: %v %v", annotationArray, err)
					}
					annotationBefore, err := s.DereferenceDict(annotationArray[0])
					if err != nil {
						t.Fatal(err)
					}
					annotationSnapshot := annotationBefore.Clone()
					geometry := types.Dict{}
					for key, value := range d {
						if key != "Resources" && key != "Contents" {
							geometry[key] = value.Clone()
						}
					}
					originals := map[int]types.StreamDict{}
					for number, entry := range s.Table {
						if entry != nil && !entry.Free {
							if sd, ok := entry.Object.(types.StreamDict); ok {
								originals[number] = types.StreamDict{Raw: bytes.Clone(sd.Raw), Content: bytes.Clone(sd.Content)}
							}
						}
					}
					prepared, names := preparedFooter(t)
					if err = applyPageNumbers(t.Context(), s, footerLayout(1), prepared, names, footerForLayout(t, footerLayout(1))); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(annotationBefore, annotationSnapshot) {
						t.Fatal("footer changed annotation coordinates or destination")
					}
					for key, value := range geometry {
						if !reflect.DeepEqual(d[key], value) {
							t.Fatalf("page geometry changed: %s", key)
						}
					}
					for number, before := range originals {
						after, ok := s.Table[number].Object.(types.StreamDict)
						if !ok || !bytes.Equal(after.Raw, before.Raw) || !bytes.Equal(after.Content, before.Content) {
							t.Fatalf("original stream %d changed", number)
						}
					}
					output := writeFooterContext(t, s, "numbered")
					if err = tools.Check(output); err != nil {
						t.Fatal(err)
					}
					extracted, err := exec.CommandContext(t.Context(), tools.PDFToText, "-raw", output, "-").Output()
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(extracted), "1") {
						t.Fatalf("footer missing: %q", extracted)
					}
					a, b := footerRaster(t, baseline), footerRaster(t, output)
					if a.Bounds() != b.Bounds() {
						t.Fatal("footer changed viewport")
					}
					// Independent physical geometry: Crop intersect Media is 300x400
					// source units at UserUnit 2; fitting produces 400x500 pt.
					physicalWidth, physicalHeight := 600.0, 800.0
					if (rotation%360+360)%180 != 0 {
						physicalWidth, physicalHeight = physicalHeight, physicalWidth
					}
					unit := 2.0
					if fit {
						physicalWidth, physicalHeight, unit = 400, 500, 1
					}
					exactRasterViewport := !topOutside || fit
					if exactRasterViewport && (a.Bounds().Dx() != int(physicalWidth/unit*2) || a.Bounds().Dy() != int(physicalHeight/unit*2)) {
						t.Fatalf("raster physical viewport: %v expected %gx%gpt", a.Bounds(), physicalWidth, physicalHeight)
					}
					// Poppler repairs the top-outside CropBox to [10 20 310 440],
					// rather than its intersection [10 30 310 440]. Keep this
					// fixture's preservation controls, while measuring exact footer
					// location only where its raster viewport matches PDF geometry.
					pixelWidth, pixelHeight := unit/2, unit/2
					protectedStrip := 42.0
					if !exactRasterViewport {
						protectedStrip = 84
					}

					left, right, bottom := a.Bounds().Max.X, -1, -1
					different, sourceInk := 0, 0
					for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
						for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
							r, g, blue, _ := a.At(x, y).RGBA()
							if r != 65535 || g != 65535 || blue != 65535 {
								sourceInk++
							}
							if a.At(x, y) != b.At(x, y) {
								different++
								left = min(left, x)
								right = max(right, x)
								bottom = max(bottom, y)
								if float64(a.Bounds().Max.Y-y)*pixelHeight > protectedStrip {
									t.Fatalf("footer changed pixels outside protected strip at %d,%d", x, y)
								}
							}
						}
					}
					if different == 0 || sourceInk == 0 {
						t.Fatalf("missing footer/source pixel evidence: changed=%d source=%d", different, sourceInk)
					}
					inkBottom := float64(a.Bounds().Max.Y-bottom-1) * pixelHeight
					inkCenter := float64(left+right+1) / 2 * pixelWidth
					if exactRasterViewport && math.Abs(inkBottom-24) > 2*pixelHeight {
						t.Fatalf("painted footer bottom %gpt differs from 24 pt margin (pixel=%gpt)", inkBottom, pixelHeight)
					}
					// Centering uses fixed logical cells; allow a small glyph-bearing
					// offset in addition to raster rounding.
					if exactRasterViewport && math.Abs(inkCenter-physicalWidth/2) > 2*pixelWidth+0.5 {
						t.Fatalf("painted footer center %gpt differs from sheet center %g pt", inkCenter, physicalWidth/2)
					}

				})
			}
		}
	}
}

func TestFooterCopiesInheritedResourcesAndPreservesSharedContent(t *testing.T) {
	fixture := pdffixture.Pages("SHARED", 2)
	s, err := read(t.Context(), fixture.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	first, _, attrs, err := s.PageDict(t.Context(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := s.PageDict(t.Context(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.Pages()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.DereferenceDict(*root)
	if err != nil {
		t.Fatal(err)
	}
	parent["Resources"] = attrs.Resources
	originalFonts, err := s.DereferenceDict(attrs.Resources["Font"])
	if err != nil {
		t.Fatal(err)
	}
	originalFonts["PCFooter"] = originalFonts["F1"]
	originalFonts["PCFooter1"] = nil
	originalResources := attrs.Resources.Clone()
	delete(first, "Resources")
	delete(second, "Resources")
	second["Contents"] = first["Contents"]
	originalContent := first["Contents"]
	prepared, names := preparedFooter(t)
	if err = applyPageNumbers(t.Context(), s, footerLayout(2), prepared, names, footerForLayout(t, footerLayout(2))); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parent["Resources"], originalResources) {
		t.Fatal("inherited resources changed")
	}
	var finalFont types.Object
	for _, page := range []types.Dict{first, second} {
		contents, err := s.DereferenceArray(page["Contents"])
		if err != nil || len(contents) != 3 || !reflect.DeepEqual(contents[1], originalContent) {
			t.Fatalf("shared original content changed: %v %v", contents, err)
		}
		resources, err := s.DereferenceDict(page["Resources"])
		if err != nil {
			t.Fatal(err)
		}
		fonts, err := s.DereferenceDict(resources["Font"])
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fonts["PCFooter"], originalFonts["PCFooter"]) || fonts["PCFooter2"] == nil {
			t.Fatal("font key collision")
		}
		if finalFont != nil && !reflect.DeepEqual(finalFont, fonts["PCFooter2"]) {
			t.Fatal("footer font was not shared")
		}
		finalFont = fonts["PCFooter2"]
	}
}

func TestFooterNumbersAllPhysicalPagesExceptPadding(t *testing.T) {
	tools := oracle.RequireTools(t)
	prepared, names := preparedFooter(t)
	s, _, err := generatedContext(t.Context(), []compose.Page{{Width: 300, Height: 200}}, 3, prepared, names)
	if err != nil {
		t.Fatal(err)
	}
	padding, _, _, err := s.PageDict(t.Context(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	untouched := padding.Clone()
	layout := footerLayout(3)
	layout.Parts = []packet.Part{
		{Leaf: packet.Leaf{Kind: "page"}, FirstPage: 1, LastPage: 1},
		{Leaf: packet.Leaf{Kind: "padding"}, FirstPage: 2, LastPage: 2},
		{Leaf: packet.Leaf{Kind: "page"}, FirstPage: 3, LastPage: 3},
	}
	if err = applyPageNumbers(t.Context(), s, layout, prepared, names, footerForLayout(t, layout)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(padding, untouched) {
		t.Fatal("padding received footer changes")
	}
	path := writeFooterContext(t, s, "padding")
	if err = tools.Check(path); err != nil {
		t.Fatal(err)
	}
	text, err := exec.CommandContext(t.Context(), tools.PDFToText, "-raw", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	pages := strings.Split(strings.TrimSuffix(string(text), "\f"), "\f")
	if len(pages) != 3 || strings.TrimSpace(pages[0]) != "1" || strings.TrimSpace(pages[1]) != "" || strings.TrimSpace(pages[2]) != "3" {
		t.Fatalf("physical numbering: %q", text)
	}
	first, _, _, err := s.PageDict(t.Context(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	third, _, _, err := s.PageDict(t.Context(), 3, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []types.Dict{first, third} {
		resources, err := s.DereferenceDict(page["Resources"])
		if err != nil {
			t.Fatal(err)
		}
		fonts, err := s.DereferenceDict(resources["Font"])
		if err != nil || len(fonts) != 1 {
			t.Fatalf("footer font resource: %v %v", fonts, err)
		}
	}
}

func TestFooterOnLargeSourceDoesNotImposeGeneratedCanvasLimit(t *testing.T) {
	fixture := pdffixture.WithBoxes("LARGE", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 50000 50000]"})
	s, err := read(t.Context(), fixture.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	prepared, names := preparedFooter(t)
	if err = applyPageNumbers(t.Context(), s, footerLayout(1), prepared, names, footerForLayout(t, footerLayout(1))); err != nil {
		t.Fatal(err)
	}
	path := writeFooterContext(t, s, "large")
	if err = oracle.RequireTools(t).Check(path); err != nil {
		t.Fatal(err)
	}
}

func TestFooterOverflowAdviceRetainsPartContextAcrossPhysicalViews(t *testing.T) {
	prepared, names := preparedFooter(t)
	for _, tc := range []struct {
		kind          string
		selected, fit bool
	}{{"source", false, false}, {"source", true, false}, {"source", true, true}, {"page", false, false}, {"contents", false, false}} {
		t.Run(fmt.Sprintf("%s/selected%v/fit%v", tc.kind, tc.selected, tc.fit), func(t *testing.T) {
			var s *model.Context
			var err error
			if tc.kind == "source" {
				fixture := pdffixture.WithBoxes("VIEW", pdffixture.PageBoxes{Media: "[0 0 612 792]"}, pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[10 20 330 440]", Crop: "[0 30 310 430]", Rotate: "90", UserUnit: "2"})
				s, err = read(t.Context(), fixture.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				if tc.selected {
					if _, err = projectSelection(t.Context(), s, []packet.PageRange{{First: 2, Last: 2}}); err != nil {
						t.Fatal(err)
					}
				}
				if tc.fit {
					if err = fitContext(t.Context(), s, &packet.Size{Width: 400, Height: 500}); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				s, _, err = generatedContext(t.Context(), []compose.Page{{Width: 300, Height: 200}}, 1, prepared, names)
				if err != nil {
					t.Fatal(err)
				}
			}
			layout := footerLayout(s.PageCount)
			layout.PageNumbers.Margin = 0
			layout.Parts[0].Kind = packet.PartKind(tc.kind)
			layout.Parts[0].Ref = "/items/1"
			if tc.kind == "source" {
				layout.Parts[0].Source = "selected.pdf"
			}
			err = applyPageNumbers(t.Context(), s, layout, prepared, names, footerForLayout(t, layout))
			if err == nil {
				t.Fatal("lower logical overflow was accepted")
			}
			d := Diagnostic(err)
			if d.Code != diag.TextOverflow || !strings.Contains(d.Message, "Increase page_numbers.margin") || !reflect.DeepEqual(d.Items, []string{"/items/1", "/page_numbers"}) || d.File != layout.Parts[0].Source {
				t.Fatalf("physical footer advice/context: %+v", d)
			}
		})
	}
}

func footerForLayout(t *testing.T, layout packet.Layout) *compose.Footer {
	t.Helper()
	if layout.PageNumbers == nil {
		return nil
	}
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	footer, err := compose.NewFooter(font, *layout.PageNumbers)
	if err != nil {
		t.Fatal(err)
	}
	return footer
}
