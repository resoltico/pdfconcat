// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestEverySourcePageRequiresPositivePhysicalFrame(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bad       int
		crop      string
		inherited bool
	}{
		{"first disjoint", 0, "[150 0 250 100]", false},
		{"middle disjoint", 1, "[150 0 250 100]", false},
		{"last disjoint", 2, "[150 0 250 100]", false},
		{"middle touching", 1, "[100 0 200 100]", false},
		{"middle inherited", 1, "[150 0 250 100]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boxes := []pdffixture.PageBoxes{{}, {}, {}}
			boxes[tc.bad].Crop = tc.crop
			doc := pdffixture.WithBoxes("frames", pdffixture.PageBoxes{Media: "[0 0 100 100]"}, boxes...)
			if tc.inherited {
				node := len(doc.Objs) + 1
				page := 4 + 2*tc.bad
				doc.Objs[page-1] = bytes.ReplaceAll(doc.Objs[page-1], []byte("/Parent 2 0 R"), []byte(fmt.Sprintf("/Parent %d 0 R", node)))
				doc.Objs[page-1] = bytes.ReplaceAll(doc.Objs[page-1], []byte(" /CropBox "+tc.crop), nil)
				doc.Objs[1] = bytes.ReplaceAll(doc.Objs[1], []byte(fmt.Sprintf("%d 0 R", page)), []byte(fmt.Sprintf("%d 0 R", node)))
				doc.Objs = append(doc.Objs, []byte(fmt.Sprintf("<< /Type /Pages /Parent 2 0 R /Count 1 /Kids [%d 0 R] /CropBox %s >>", page, tc.crop)))
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "source.pdf")
			if err := doc.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			original := doc.Bytes()
			digest := sha256.Sum256(original)
			for _, fit := range []bool{false, true} {
				for _, selected := range []bool{false, true} {
					for _, op := range []core.Operation{core.Check, core.Build} {
						output := filepath.Join(dir, "out.pdf")
						sentinel := []byte("Existing reviewed output")
						if err := os.WriteFile(output, sentinel, 0600); err != nil {
							t.Fatal(err)
						}
						req := core.Request{Operation: op, WorkingDir: dir, Output: output, Overwrite: true, Input: core.Items{core.SourceItem{Path: source, Ref: "argv:1"}}}
						if fit {
							sheet, _ := core.ParseSheet("300x500pt")
							req.FitTo = &sheet
						}
						if selected {
							good := 1
							if tc.bad == 0 {
								good = 2
							}
							plan := map[string]any{"version": 4, "items": []any{map[string]any{"source": source, "pages": [][2]int{{good, good}}}}}
							data, err := json.Marshal(plan)
							if err != nil {
								t.Fatal(err)
							}
							path := filepath.Join(dir, "plan.json")
							if err := os.WriteFile(path, data, 0600); err != nil {
								t.Fatal(err)
							}
							req.Input = core.PlanFile{Path: path}
						}
						result := core.Execute(t.Context(), req)
						if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.SourceInvalid) || result.Output.Published {
							t.Fatalf("fit=%v selected=%v %s: %+v", fit, selected, op, result)
						}
						for _, finding := range result.Diagnostics {
							if finding.Code == diag.SourceInvalid && (finding.PDFLocation == nil || finding.PDFLocation.Page != tc.bad+1 || finding.PDFLocation.Object != 4+2*tc.bad || finding.PDFLocation.Path != "Pages.EffectiveGeometry") {
								t.Fatalf("wrong geometry attribution: %+v", finding)
							}
						}
						after, err := os.ReadFile(output)
						if err != nil || !bytes.Equal(after, sentinel) {
							t.Fatal("output changed on refusal", err)
						}
						after, err = os.ReadFile(source)
						if err != nil || sha256.Sum256(after) != digest {
							t.Fatal("source changed", err)
						}
						entries, err := os.ReadDir(dir)
						if err != nil {
							t.Fatal(err)
						}
						for _, e := range entries {
							if e.Name() != "source.pdf" && e.Name() != "out.pdf" && e.Name() != "plan.json" {
								t.Fatalf("unexpected stage: %s", e.Name())
							}
						}
					}
				}
			}
		})
	}
}
