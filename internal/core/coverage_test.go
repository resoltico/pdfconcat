// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestNeutralSourceCoverageComposesWithMeaningfulContent(t *testing.T) {
	dir := t.TempDir()
	if err := pdffixture.Plain("Uncovered text").WriteFile(filepath.Join(dir, "plain.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := pdffixture.Tagged("Tagged text").WriteFile(filepath.Join(dir, "tagged.pdf")); err != nil {
		t.Fatal(err)
	}
	// One source contains a blank, artifact-only artwork and actual uncovered
	// content. Selecting either neutral page must not inherit page 3's gap.
	source := []byte(`{"version":5,"page_defaults":{"size":"A4"},"items":[{"page":{}},{"page":{"background":"#abc"}},{"source":"plain.pdf"}]}`)
	result := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanBytes{Data: source}, WorkingDir: dir, Output: "mixed-source.pdf"})
	if result.Status != "ok" {
		t.Fatal(result.Diagnostics)
	}
	serial := 0
	for _, selection := range [][2]int{{1, 1}, {2, 2}, {1, 2}, {3, 3}} {
		for _, fitted := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				for _, companionSource := range []bool{false, true} {
					serial++
					t.Run(fmt.Sprintf("selection=%v/fit=%v/reverse=%v/source=%v", selection, fitted, reverse, companionSource), func(t *testing.T) {
						neutral := map[string]any{"source": "mixed-source.pdf", "pages": [][2]int{selection}}
						companion := map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"value": "Meaningful generated text"}}}
						if companionSource {
							companion = map[string]any{"source": "tagged.pdf"}
						}
						items := []any{neutral, companion}
						if reverse {
							items = []any{companion, neutral}
						}
						plan := map[string]any{"version": 5, "items": items}
						if fitted {
							plan["fit_to"] = "A4"
						}
						data, err := json.Marshal(plan)
						if err != nil {
							t.Fatal(err)
						}
						expected := "complete"
						warning := false
						if selection[0] == 3 {
							expected = "partial"
							warning = true
						}
						for _, op := range []core.Operation{core.Check, core.Build} {
							r := core.Execute(t.Context(), core.Request{Operation: op, Input: core.PlanBytes{Data: data}, WorkingDir: dir, Output: fmt.Sprintf("case-%d.pdf", serial)})
							if r.Status != "ok" || r.Structure == nil || r.Structure.Coverage == nil || *r.Structure.Coverage != expected {
								t.Fatalf("%s: coverage/status %+v %+v", op, r.Structure, r.Diagnostics)
							}
							found := false
							for _, d := range r.Diagnostics {
								found = found || d.Code.String() == "source_structure_partial"
							}
							if found != warning {
								t.Fatalf("%s warning=%v expected=%v: %+v", op, found, warning, r.Diagnostics)
							}
							if op == core.Build && !r.Output.Published {
								t.Fatal("build did not publish")
							}
						}
					})
				}
			}
		}
	}
}
