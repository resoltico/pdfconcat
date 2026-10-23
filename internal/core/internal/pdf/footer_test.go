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
					if err = applyPageNumbers(t.Context(), s, footerLayout(1), prepared, names); err != nil {
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
	if err = applyPageNumbers(t.Context(), s, footerLayout(2), prepared, names); err != nil {
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

func TestFooterInkBottomMatchesMarginForEveryDigit(t *testing.T) {
	digits, err := footerDigits(packet.PageNumbers{Size: 9})
	if err != nil {
		t.Fatal(err)
	}
	for number := int64(1); number <= 20; number++ {
		placed, err := footerPlacement(digits, packet.Size{Width: 300, Height: 200}, number, 24)
		if err != nil {
			t.Fatal(err)
		}
		if placed.InkBounds == nil || math.Abs(placed.InkBounds.Y-24) > 1e-9 {
			t.Fatalf("number %d ink bottom: %+v", number, placed.InkBounds)
		}
	}
}

func TestFooterPlacementRejectsOverflowAndAcceptsLargeSourceCanvas(t *testing.T) {
	for _, test := range []struct {
		size   packet.Size
		number int64
		config packet.PageNumbers
		bad    bool
	}{
		{packet.Size{Width: 50000, Height: 50000}, 123, packet.PageNumbers{Size: 9, Margin: 24}, false},
		{packet.Size{Width: 5, Height: 200}, 1234567890, packet.PageNumbers{Size: 9, Margin: 24}, true},
		{packet.Size{Width: 200, Height: 20}, 1, packet.PageNumbers{Size: 9, Margin: 24}, true},
		{packet.Size{Width: 200, Height: 200}, 1, packet.PageNumbers{Size: 100, Margin: 180}, true},
	} {
		digits, err := footerDigits(test.config)
		if err != nil {
			t.Fatal(err)
		}
		err = CheckNumberPlacement(digits, test.size, test.number, test.config.Margin)
		if (err != nil) != test.bad {
			t.Fatalf("placement %+v: %v", test, err)
		}
	}
}

func TestFooterNumbersAllPhysicalPagesExceptPadding(t *testing.T) {
	tools := oracle.RequireTools(t)
	prepared, names := preparedFooter(t)
	s, _, err := generatedContext(t.Context(), []Page{{Width: 300, Height: 200}}, 3, prepared, names)
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
	if err = applyPageNumbers(t.Context(), s, layout, prepared, names); err != nil {
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
	if err = applyPageNumbers(t.Context(), s, footerLayout(1), prepared, names); err != nil {
		t.Fatal(err)
	}
	path := writeFooterContext(t, s, "large")
	if err = oracle.RequireTools(t).Check(path); err != nil {
		t.Fatal(err)
	}
}

func TestFooterOverflowAdviceMatchesFailedPhysicalBounds(t *testing.T) {
	digits, err := footerDigits(packet.PageNumbers{Size: 9})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		size         packet.Size
		number       int64
		margin       float64
		want, absent []string
	}{
		{"lower", packet.Size{Width: 300, Height: 200}, 1, 0, []string{"below the physical page", "Increase page_numbers.margin to at least 2.637 pt"}, []string{"Decrease page_numbers.margin"}},
		{"upper", packet.Size{Width: 300, Height: 20}, 1, 24, []string{"above the physical page", "Decrease page_numbers.margin to at most 10.379 pt"}, []string{"Increase page_numbers.margin"}},
		{"width", packet.Size{Width: 5, Height: 200}, 1234567890, 24, []string{"width", "reduce page_numbers.size", "increase sheet width"}, []string{"Increase page_numbers.margin", "Decrease page_numbers.margin"}},
		{"width and lower", packet.Size{Width: 5, Height: 200}, 1234567890, 0, []string{"width", "below the physical page", "Increase page_numbers.margin"}, nil},
		{"short envelope with lower only", packet.Size{Width: 300, Height: 11}, 1, 0, []string{"below the physical page", "complete logical and ink envelope", "reduce page_numbers.size", "increase sheet height"}, []string{"Increase page_numbers.margin"}},
		{"short envelope crossing both edges", packet.Size{Width: 300, Height: 5}, 1, 0, []string{"below and above the physical page", "complete logical and ink envelope", "reduce page_numbers.size", "increase sheet height"}, []string{"Increase page_numbers.margin"}},
		{"width and both vertical edges", packet.Size{Width: 5, Height: 5}, 1234567890, 0, []string{"width", "below and above the physical page", "increase sheet width", "increase sheet height"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckNumberPlacement(digits, tc.size, tc.number, tc.margin)
			if err == nil {
				t.Fatal("overflow was accepted")
			}
			diagnostic := Diagnostic(err)
			if diagnostic.Code != diag.TextOverflow {
				t.Fatalf("overflow classification: %+v", diagnostic)
			}
			assertFooterMarginHintFits(t, digits, tc.size, tc.number, diagnostic.Message)
			for _, text := range tc.want {
				if !strings.Contains(diagnostic.Message, text) {
					t.Errorf("missing %q in %q", text, diagnostic.Message)
				}
			}
			for _, text := range tc.absent {
				if strings.Contains(diagnostic.Message, text) {
					t.Errorf("misleading %q in %q", text, diagnostic.Message)
				}
			}
		})
	}
	// The stated measured thresholds must give the user working remedies.
	for _, tc := range []struct {
		size   packet.Size
		margin float64
	}{{packet.Size{Width: 300, Height: 200}, 2.637}, {packet.Size{Width: 300, Height: 20}, 10.379}} {
		if err = CheckNumberPlacement(digits, tc.size, 1, tc.margin); err != nil {
			t.Fatalf("recommended margin %g did not fit: %v", tc.margin, err)
		}
	}
	tight, err := footerDigits(packet.PageNumbers{Size: 9.1})
	if err != nil {
		t.Fatal(err)
	}
	tightSize := packet.Size{Width: 300, Height: tight.Top - tight.Bottom}
	message := Diagnostic(CheckNumberPlacement(tight, tightSize, 1, 0)).Message
	_, tail, found := strings.Cut(message, "Increase page_numbers.margin to at least ")
	if !found {
		t.Fatalf("missing tight-interval advice: %s", message)
	}
	var minimum float64
	if _, err = fmt.Sscanf(tail, "%f", &minimum); err != nil {
		t.Fatal(err)
	}
	if err = CheckNumberPlacement(tight, tightSize, 1, minimum); err != nil {
		t.Fatalf("rounded advice left the narrow usable interval: %g: %v", minimum, err)
	}
	if err = CheckNumberPlacement(digits, packet.Size{Width: 300, Height: math.NaN()}, 1, 24); err == nil || Diagnostic(err).Code != diag.TextOverflow {
		t.Fatalf("nonfinite physical height escaped the guard: %v", err)
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
				s, _, err = generatedContext(t.Context(), []Page{{Width: 300, Height: 200}}, 1, prepared, names)
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
			err = applyPageNumbers(t.Context(), s, layout, prepared, names)
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

func assertFooterMarginHintFits(t *testing.T, digits *typeset.Digits, size packet.Size, number int64, message string) {
	t.Helper()
	for _, phrase := range []string{"Increase page_numbers.margin to at least ", "Decrease page_numbers.margin to at most "} {
		_, tail, found := strings.Cut(message, phrase)
		if !found {
			continue
		}
		var margin float64
		if _, err := fmt.Sscanf(tail, "%f", &margin); err != nil {
			t.Fatalf("invalid margin hint in %q: %v", message, err)
		}
		if err := CheckNumberPlacement(digits, size, number, margin); err != nil {
			t.Fatalf("unusable margin hint %g in %q: %v", margin, message, err)
		}
	}
}

func TestFooterAdviceOmitsNumericalBoundsThatFailExactContainment(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		fontSize, width, height, margin, validMargin float64
		direction                                    string
	}{
		{"tight upper 9.1pt", 9.1, 1000000, 12.3942, 13.3942, 2.6663, "Decrease"},
		{"tight upper 1pt", 1, 1000000, 1.3619999999999999, 2.362, 0.293, "Decrease"},
		{"lower roundoff", 1.3890784982935154, 300, 200, 0, 0.408, "Increase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			digits, err := footerDigits(packet.PageNumbers{Size: tc.fontSize})
			if err != nil {
				t.Fatal(err)
			}
			size := packet.Size{Width: tc.width, Height: tc.height}
			err = CheckNumberPlacement(digits, size, 1, tc.margin)
			if err == nil {
				t.Fatal("expected the initial margin to overflow")
			}
			message := Diagnostic(err).Message
			assertFooterMarginHintFits(t, digits, size, 1, message)
			if !strings.Contains(message, tc.direction+" page_numbers.margin.") || !strings.Contains(message, "reduce page_numbers.size or increase sheet height") {
				t.Fatalf("missing directional fallback: %s", message)
			}
			if strings.Contains(message, "to at least") || strings.Contains(message, "to at most") {
				t.Fatalf("unverified numerical bound was advertised: %s", message)
			}
			if err = CheckNumberPlacement(digits, size, 1, tc.validMargin); err != nil {
				t.Fatalf("known usable margin %g refused: %v", tc.validMargin, err)
			}
		})
	}
}
