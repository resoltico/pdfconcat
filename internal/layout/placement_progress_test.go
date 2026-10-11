// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package layout_test

import (
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/observation"
)

type placementObservations []observation.Milestone

func (o *placementObservations) Observe(m observation.Milestone) { *o = append(*o, m) }

func TestPlacementReportsCompletedSpecsIncludingMissingFont(t *testing.T) {
	font := loadFont(t)
	missing, valid := blankSpec("missing", 300, 200), blankSpec("valid", 300, 200)
	missing.Text.Font = assembly.Font{File: "missing.ttf"}
	var observed placementObservations
	placed, err := layout.PlaceBounds(t.Context(), layoutOf(used(&missing, 1), used(&valid, 2)), builtIn(font), &observed)
	if len(assembly.Diagnostics(err)) != 1 || len(placed) != 2 || placed[0] != nil || placed[1] == nil {
		t.Fatalf("placements=%v error=%v", placed, err)
	}
	want := []observation.Milestone{
		{Phase: observation.Layout, Unit: observation.ProcessedGeneratedSpecs, Completed: 1, Total: 2, TotalKnown: true},
		{Phase: observation.Layout, Unit: observation.ProcessedGeneratedSpecs, Completed: 2, Total: 2, TotalKnown: true},
	}
	if !slices.Equal(observed, want) {
		t.Fatalf("progress=%v want=%v", observed, want)
	}
}

func TestStaticValidationContinuesAfterEachMissingFont(t *testing.T) {
	font := loadFont(t)
	flat := &assembly.Flattened{Source: assembly.ArgumentSource{}}
	for _, ref := range []assembly.Ref{7, 8, 9} {
		style := assembly.BlankStyle{Text: assembly.TextStyle{Font: assembly.Set(assembly.Font{File: "missing.ttf"}, assembly.Origin{Ref: ref})}}
		if ref == 8 {
			style.Text.Font = assembly.Set(assembly.Font{}, assembly.Origin{Ref: ref})
			style.Text.Value = assembly.Set("supported", assembly.Origin{Ref: ref})
		}
		flat.Contributions = append(flat.Contributions, assembly.Contribution{Kind: assembly.ItemBlank, Origin: assembly.Origin{Ref: ref}, Style: len(flat.Styles), TextOverrides: &style.Text})
		flat.Styles = append(flat.Styles, assembly.LayeredStyle{Style: style, FirstUse: assembly.Origin{Ref: ref}})
	}
	problems := assembly.Diagnostics(layout.ValidateText(t.Context(), flat, builtIn(font)))
	if len(problems) != 2 {
		t.Fatalf("missing-font diagnostics=%v", problems)
	}
	for index, want := range []string{"argv:7", "argv:9"} {
		if problems[index].Code != assembly.CodeFontUnavailable || problems[index].Location.Pointer != want || problems[index].Affected != 1 {
			t.Fatalf("diagnostic=%+v want=%s", problems[index], want)
		}
	}
}
