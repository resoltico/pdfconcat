// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func boundaryActionAnnotation(subtype, event string, flags int, appearance bool) *pdffixture.Doc {
	action := "/A << /S /GoTo /D [3 0 R /Fit] >>"
	if event != "" {
		action = fmt.Sprintf("/AA << /%s << /S /GoTo /D [3 0 R /Fit] >> >>", event)
	}
	doc := pdffixture.FitAnnotation(subtype, action, appearance, 0, flags)
	doc.Objs[5] = bytes.ReplaceAll(doc.Objs[5], []byte("/Rect [10 10 90 90]"), []byte("/Rect [10 -30 90 70]"))
	if appearance {
		if subtype == "Link" {
			// Isolate AP painting: Poppler also paints a Link BS border despite
			// AP taking precedence (Table173), independently of the AP clip.
			doc.Objs[5] = bytes.ReplaceAll(doc.Objs[5], []byte("/BS << /W 2 >>"), []byte("/Border [0 0 0]"))
		}
		// A solid appearance crosses the source frame. A missing clip would expose
		// a 240 x 90 point red rectangle in the fitted lower margin.
		program := "1 0 0 rg 0 0 80 40 re f"
		doc.Objs[6] = []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 40] /Matrix [0 2 -1 0 40 0] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(program), program))
	}
	return doc
}

func TestFittingReadOnlyInteractionAndAppearance(t *testing.T) {
	for _, shape := range []struct{ subtype, event string }{{"Link", ""}, {"Screen", ""}, {"Screen", "E"}} {
		actionName := "A"
		if shape.event != "" {
			actionName = "AA-" + shape.event
		}
		for _, flags := range []struct {
			name     string
			value    int
			accepted bool
		}{
			// Table165: Print=4, NoView=32, ReadOnly=64, Locked=128.
			{"ReadOnly", 68, true}, {"ordinary", 4, false}, {"NoView", 36, true}, {"Locked", 132, false},
		} {
			t.Run(shape.subtype+"/"+actionName+"/"+flags.name, func(t *testing.T) {
				doc := boundaryActionAnnotation(shape.subtype, shape.event, flags.value, true)
				dir := t.ArtifactDir()
				source := filepath.Join(dir, "source.pdf")
				output := filepath.Join(dir, "out.pdf")
				if err := doc.WriteFile(source); err != nil {
					t.Fatal(err)
				}
				original := sha256.Sum256(doc.Bytes())
				sheet, _ := core.ParseSheet("300x500pt")
				for _, op := range []core.Operation{core.Check, core.Build} {
					result := core.Execute(t.Context(), core.Request{Operation: op, Input: core.Items{core.SourceItem{Path: source, Ref: "argv:1"}}, WorkingDir: dir, Output: output, FitTo: &sheet})
					if flags.accepted {
						if result.Status != "ok" {
							t.Fatalf("%s: %+v", op, result)
						}
						if result.Output == nil || result.Output.Published != (op == core.Build) {
							t.Fatalf("%s publication: %+v", op, result.Output)
						}
					} else if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitAnnotationUnsupported) || result.Output == nil || result.Output.Published {
						t.Fatalf("%s interactive refusal: %+v", op, result)
					}
					data, err := os.ReadFile(source)
					if err != nil || sha256.Sum256(data) != original {
						t.Fatal("source bytes changed", err)
					}
					entries, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					count := 1
					if flags.accepted && op == core.Build {
						count = 2
					}
					if len(entries) != count {
						t.Fatalf("%s unexpected output/stage: %v", op, entries)
					}
				}
				if !flags.accepted {
					return
				}
				annotations := fitAnnotationObjects(t, output)
				if len(annotations) != 1 {
					t.Fatalf("got %d annotations", len(annotations))
				}
				annotation := annotations[0]
				if annotation["/Subtype"] != "/"+shape.subtype || annotation["/F"] != float64(flags.value) || annotation["/AP"] == nil {
					t.Fatalf("annotation state lost: %v", annotation)
				}
				fitWantNumbers(t, annotation["/Rect"], []float64{30, 10, 270, 310})
				if shape.event == "" {
					if annotation["/A"] == nil {
						t.Fatal("activation action lost")
					}
				} else {
					aa, ok := annotation["/AA"].(map[string]any)
					if !ok || aa["/"+shape.event] == nil {
						t.Fatal("interaction event lost", annotation)
					}
				}
				independent, err := oracle.Load(oracle.RequireTools(t), output)
				if err != nil {
					t.Fatal(err)
				}
				targets, err := independent.ActionTargets(1)
				if err != nil || !slices.Equal(targets, []int{1}) {
					t.Fatalf("retained action target: %v %v", targets, err)
				}
				fitCompareSheet(t, source, output, 3, 100)
				if flags.value == 68 {
					img := fitRaster(t, output, 1, 72)
					red, green, blue, _ := img.At(150, 250).RGBA()
					if red < 60000 || green > 1000 || blue > 1000 {
						t.Fatal("visible appearance ink was lost")
					}
					for y := range img.Bounds().Dy() {
						if y >= 99 && y <= 400 {
							continue
						} // Allow one antialiased clipping-edge row.
						for x := range img.Bounds().Dx() {
							r, g, b, _ := img.At(x, y).RGBA()
							if r != 65535 || g != 65535 || b != 65535 {
								t.Fatalf("appearance escaped source clip at %d,%d", x, y)
							}
						}
					}
				}
			})
		}
	}
}

func TestReadOnlyDoesNotWaiveFittingProtections(t *testing.T) {
	widget := pdffixture.Form("ReadOnly widgets")
	for _, object := range []int{21, 22, 23} {
		widget.Objs[object-1] = bytes.ReplaceAll(widget.Objs[object-1], []byte("/F 4"), []byte("/F 68"))
	}
	cases := []struct {
		name string
		doc  *pdffixture.Doc
		code diag.Code
	}{
		{"Link missing AP", boundaryActionAnnotation("Link", "", 68, false), diag.FitAnnotationUnsupported},
		{"Screen missing AP", boundaryActionAnnotation("Screen", "", 68, false), diag.FitAnnotationUnsupported},
		{"Screen AA missing AP", boundaryActionAnnotation("Screen", "E", 68, false), diag.FitAnnotationUnsupported},
		{"FreeText missing AP", pdffixture.FitAnnotation("FreeText", "/DA (/F1 12 Tf 0 g) /Contents (Read only)", false, 0, 68), diag.FitAnnotationUnsupported},
		{"widgets", widget, diag.FitFormUnsupported},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.ArtifactDir()
			source := filepath.Join(dir, "source.pdf")
			output := filepath.Join(dir, "out.pdf")
			if err := tc.doc.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			original := sha256.Sum256(tc.doc.Bytes())
			sentinel := []byte("preserve existing output")
			if err := os.WriteFile(output, sentinel, 0600); err != nil {
				t.Fatal(err)
			}
			sheet, _ := core.ParseSheet("300x500pt")
			for _, op := range []core.Operation{core.Check, core.Build} {
				result := core.Execute(t.Context(), core.Request{Operation: op, Input: core.Items{core.SourceItem{Path: source, Ref: "argv:1"}}, WorkingDir: dir, Output: output, Overwrite: true, FitTo: &sheet})
				if result.Status != "invalid" || !codePresent(result.Diagnostics, tc.code) || result.Output == nil || result.Output.Published {
					t.Fatalf("%s: %+v", op, result)
				}
				data, err := os.ReadFile(source)
				if err != nil || sha256.Sum256(data) != original {
					t.Fatal("source changed", err)
				}
				data, err = os.ReadFile(output)
				if err != nil || !bytes.Equal(data, sentinel) {
					t.Fatal("output changed on refusal", err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 2 {
					t.Fatal("staging leak", entries, err)
				}
			}
		})
	}
}
