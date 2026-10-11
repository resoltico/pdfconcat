// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"errors"
	"os"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestPolicyLocationDistinguishesFirstAndUnavailableRun(t *testing.T) {
	t.Parallel()
	pages := int64(pdfengine.MaxOutputPages) + 1
	current := &pipeline{
		flat: &assembly.Flattened{
			Source:        assembly.ArgumentSource{},
			Contributions: []assembly.Contribution{{Path: "first.pdf", Origin: assembly.Origin{Ref: 7}}},
		},
		layout: &assembly.Layout{
			Runs:       []assembly.Run{{Count: pages}},
			Placements: []assembly.Placement{{Range: assembly.PageRange{Start: 1, End: pages}}},
		},
	}
	first := problem{}
	current.locatePolicyProblem(&first, &pdfengine.Error{Run: 0, Reason: pdfengine.ReasonPageLimit})
	if first.diagnostic.Path != "first.pdf" || first.diagnostic.Location == nil ||
		first.diagnostic.Location.ArgvIndex == nil || *first.diagnostic.Location.ArgvIndex != 7 {
		t.Fatalf("first run lost declaration provenance: %+v", first.diagnostic)
	}
	for _, run := range []int{pdfengine.NoRun, len(current.layout.Runs)} {
		unavailable := problem{diagnostic: report.Diagnostic{Path: "unlocated.pdf"}}
		current.locatePolicyProblem(&unavailable, &pdfengine.Error{Run: run, Reason: pdfengine.ReasonPageLimit})
		if unavailable.diagnostic.Path != "unlocated.pdf" || unavailable.diagnostic.Location != nil {
			t.Fatalf("unavailable run %d invented provenance: %+v", run, unavailable.diagnostic)
		}
	}
}

func TestProducedResourceReportsEveryCompletedSpec(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)
	workspace := openScratch(t)
	file, err := capture.CreatePrivateTemp(workspace.Dir(), "resource-progress-*")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	t.Cleanup(func() {
		if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	})
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	var specs []assembly.ResolvedSpec
	for _, width := range []assembly.Length{100, 200} {
		style := assembly.BlankStyle{}
		spec, resolveErr := style.Resolve(assembly.PageDim{Width: width, Height: 100})
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		specs = append(specs, assembly.ResolvedSpec{Spec: spec})
	}
	sink := &progressCapture{}
	progress := NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink)
	t.Cleanup(progress.Stop)
	current := &pipeline{
		workspace: workspace,
		fonts:     loadedFonts{byPath: map[string]*typeset.Font{"": font}},
		builder:   report.NewBuilder(buildName),
		layout:    &assembly.Layout{Specs: specs},
		progress:  progress,
	}
	if err := current.produceResource(t.Context(), path, file); err != nil {
		t.Fatal(err)
	}
	progress.Stop()
	var final *progressRecord
	for _, record := range decodeProgressRecords(t, sink.recordsCopy()) {
		if record.Phase == observation.Layout.String() && record.Unit == observation.ProcessedGeneratedSpecs.String() {
			final = &record
		}
	}
	if final == nil || final.Completed == nil || *final.Completed != 2 || final.Total == nil || *final.Total != 2 {
		t.Fatalf("actual generated-spec progress: %+v", final)
	}
	if err := tools.Check(path); err != nil {
		t.Fatal(err)
	}
	document, err := pdforacle.Load(tools, path)
	if err != nil || document.PageCount() != 2 {
		t.Fatalf("independent resource page count: %v, %v", document, err)
	}
}
