// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
)

type warningPointerSource struct {
	assembly.Source
	calls map[assembly.Ref]int
}

func (source *warningPointerSource) Pointer(ref assembly.Ref, member string) string {
	source.calls[ref]++
	return source.Source.Pointer(ref, member)
}

func TestSourceFeatureProvenanceOnlyMaterializesReportedReferences(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}
	facts := make(map[string]pdfengine.SourceInfo)
	for name, document := range map[string]*pdffixture.Doc{"plain.pdf": pdffixture.Plain("plain"), "outline.pdf": pdffixture.Outlined("outline")} {
		path := filepath.Join(dir, name)
		if err := document.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		info, err := engine.Inspect(t.Context(), path, nil)
		if err != nil {
			t.Fatal(err)
		}
		facts[path] = info
	}
	if len(facts[filepath.Join(dir, "plain.pdf")].Features) != 0 || len(facts[filepath.Join(dir, "outline.pdf")].Features) != 1 {
		t.Fatal("native fixtures do not isolate plain and outlined source facts")
	}
	for _, count := range []int{100, 1000, 10000} {
		for _, warned := range []bool{false, true} {
			t.Run(fmt.Sprintf("plain=%d/warnings=%t", count, warned), func(t *testing.T) {
				items := make([]any, 0, count+3)
				if warned {
					items = append(items, "outline.pdf")
				}
				items = append(items, map[string]any{"blank": map[string]any{"size": "100x100"}})
				for range count {
					items = append(items, "plain.pdf")
				}
				if warned {
					items = append(items, "outline.pdf")
				}
				data, err := json.Marshal(map[string]any{"version": 1, "items": items})
				if err != nil {
					t.Fatal(err)
				}
				job, err := plan.Decode(t.Context(), plan.Input{Name: "feature-cost.json", BaseDir: dir}, strings.NewReader(string(data)))
				if err != nil {
					t.Fatal(err)
				}
				flat, err := assembly.Flatten(job)
				if err != nil || len(flat.Contributions) != len(items) {
					t.Fatalf("real flattened fixture not admitted: %v", err)
				}
				measured := &warningPointerSource{Source: flat.Source, calls: make(map[assembly.Ref]int)}
				flat.Source = measured
				current := &pipeline{flat: flat, files: make([]sourceEvidence, len(flat.Files)), builder: report.NewBuilder("check")}
				for index, file := range flat.Files {
					info, found := facts[file.Path]
					if !found {
						t.Fatalf("unknown native source %s", file.Path)
					}
					current.files[index] = sourceEvidence{info: info, inspected: true}
				}
				current.sourceFeatureWarnings()
				for _, contribution := range flat.Contributions {
					if contribution.Kind != assembly.ItemPDF || len(current.files[contribution.File].info.Features) == 0 {
						if calls := measured.calls[contribution.Origin.Ref]; calls != 0 {
							t.Fatalf("unreported declaration materialized %d discarded provenance pointers", calls)
						}
					}
				}
				saved := current.builder.Build(report.StatusOK)
				if !warned {
					if len(saved.Diagnostics) != 0 {
						t.Fatal("plain sources invented warnings")
					}
				} else {
					if len(saved.Diagnostics) != 1 {
						t.Fatalf("native outlined source warnings: %+v", saved.Diagnostics)
					}
					warning := saved.Diagnostics[0]
					want := []string{"/items/0", fmt.Sprintf("/items/%d", count+2)}
					if warning.Code != "source_bookmarks_removed" || warning.Severity != report.SeverityWarning || warning.Location == nil ||
						warning.Location.Pointer != "/items/0" || warning.Recovery == nil || warning.Recovery.Location == nil ||
						warning.Recovery.Location.Pointer != "/items/0" || !slices.Equal(warning.Consumers, want) {
						t.Fatalf("ordered native warning provenance: %+v", warning)
					}
				}
				t.Logf("plain occurrences=%d warning occurrences=%d materialized declaration refs=%d", count, 2*len(saved.Diagnostics), len(measured.calls))
			})
		}
	}
}
