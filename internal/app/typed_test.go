// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestTypedAndJSONFullPacket(t *testing.T) {
	dir := t.TempDir()
	if err := pdffixture.Pages("source", 3).WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	size := packet.Size{Width: 612, Height: 792}
	language := "ro"
	defaults := packet.PageStyle{Size: packet.Setting[packet.Size]{Present: true, Value: size}, Text: packet.TextStyle{Value: packet.Setting[string]{Present: true, Value: "ş"}}}
	typed := app.Declared{Declaration: packet.Declaration{Metadata: &packet.Metadata{Language: &language}, PageDefaults: defaults, Items: []packet.Item{&packet.ContentsItem{Ref: "/items/0"}, &packet.GroupItem{Ref: "/items/1", Title: "Annex", Divider: &packet.PageStyle{}, Items: []packet.Item{&packet.SourceItem{Ref: "/items/1/items/0", Path: "source.pdf", Selection: []packet.PageRange{{First: 1, Last: 2}}}, &packet.PageItem{Ref: "/items/1/items/1", Count: 2}}}}}}
	jsonInput := app.PlanBytes{Data: []byte(`{"version":8,"metadata":{"language":"ro"},"page_defaults":{"size":"Letter","text":{"value":"ş"}},"items":[{"contents":{}},{"title":"Annex","divider":{},"items":[{"source":"source.pdf","pages":[[1,2]]},{"page":{},"count":2}]}]}`)}
	var prior []byte
	for i, input := range []app.Input{typed, jsonInput} {
		r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1 + i, Operation: app.Build, Input: input, WorkingDir: dir, Output: "packet.pdf", Overwrite: true})
		if r.Status != "ok" {
			t.Fatal(r.Diagnostics)
		}
		data, err := os.ReadFile(filepath.Join(dir, "packet.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !bytes.Equal(prior, data) {
			t.Fatal("typed and JSON PDF bytes differ")
		}
		prior = data
	}
}

func TestTypedCancellationReservesContentsFont(t *testing.T) {
	dir := t.TempDir()
	font := filepath.Join(dir, "font.ttf")
	original := []byte("reserved font bytes")
	if err := os.WriteFile(font, original, 0600); err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithCancel(t.Context())
	cancel()
	input := app.Declared{Declaration: packet.Declaration{PageDefaults: packet.PageStyle{Text: packet.TextStyle{Font: packet.Setting[packet.Font]{Present: true, Value: packet.Font{Path: font}}}}, Items: []packet.Item{&packet.ContentsItem{}, &packet.GroupItem{Title: "Section", Items: []packet.Item{&packet.PageItem{Count: 1, Style: packet.PageStyle{Text: packet.TextStyle{Font: packet.Setting[packet.Font]{Present: true}}}}}}}}}
	r := app.Execute(c, app.Request{InspectionWorkers: 1, Operation: app.Check, WorkingDir: dir, Input: input, Report: font, Overwrite: true})
	if r.Status != "interrupted" || !code(r, "report_is_input") || !bytes.Equal(original, mustRead(t, font)) {
		t.Fatalf("early typed inventory lost: %+v", r)
	}
}

func TestWorkerCountsAndConcurrentRequestsAgree(t *testing.T) {
	dir := t.TempDir()
	var items app.Items
	for index := range 3 {
		path := filepath.Join(dir, fmt.Sprintf("%d.pdf", index))
		if err := pdffixture.Plain(fmt.Sprintf("source %d", index)).WriteFile(path); err != nil {
			t.Fatal(err)
		}
		items = append(items, app.SourceItem{Ref: fmt.Sprintf("source:%d", index), Path: path})
	}
	type built struct {
		result app.Result
		data   []byte
		err    error
	}
	results := make(chan built, 2)
	for _, workers := range []int{1, 3} {
		go func() {
			output := filepath.Join(dir, fmt.Sprintf("workers-%d.pdf", workers))
			r := app.Execute(t.Context(), app.Request{InspectionWorkers: workers, Operation: app.Build, Input: items, WorkingDir: dir, Output: output})
			data, err := os.ReadFile(output)
			results <- built{r, data, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.result.Status != "ok" || second.result.Status != "ok" || !bytes.Equal(first.data, second.data) {
		t.Fatalf("worker budgets changed output: %+v %+v", first.result.Diagnostics, second.result.Diagnostics)
	}
}

func TestTypedDerivedIdentityCollisionRefusesPublication(t *testing.T) {
	dir := t.TempDir()
	input := app.Declared{Declaration: packet.Declaration{Items: []packet.Item{&packet.GroupItem{Ref: "section", Title: "Section", Divider: &packet.PageStyle{}, Items: []packet.Item{&packet.PageItem{Ref: "section/divider", Count: 1}}}}}}
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, Input: input, WorkingDir: dir, Output: "must-not-exist.pdf", Report: "refusal.json"})
	if r.Status != "invalid" || !code(r, "plan_value") {
		t.Fatal(r)
	}
	if _, err := os.Stat(filepath.Join(dir, "must-not-exist.pdf")); !os.IsNotExist(err) {
		t.Fatalf("invalid identity published: %v", err)
	}
	if !r.Report.Written {
		t.Fatal("refusal report missing")
	}
}

func TestBareTypedContainerAndExplicitJSONGroupingGrammar(t *testing.T) {
	dir := t.TempDir()
	typed := app.Declared{Declaration: packet.Declaration{PageDefaults: packet.PageStyle{Size: packet.Setting[packet.Size]{Present: true, Value: packet.Size{Width: 300, Height: 400}}}, Items: []packet.Item{&packet.GroupItem{Items: []packet.Item{&packet.PageItem{Count: 1}}}}}}
	req := app.Request{Operation: app.Check, InspectionWorkers: 1, WorkingDir: dir, Input: typed}
	if r := app.Execute(t.Context(), req); r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	req.Input = app.PlanBytes{Data: []byte(`{"version":8,"items":[{"items":[{"page":{"size":"300x400"}}]}]}`)}
	if r := app.Execute(t.Context(), req); r.Status != "invalid" || !code(r, "plan_item_shape") {
		t.Fatal("JSON-only grouping grammar drifted", r)
	}
}

func TestGeneratedFittingGeometrySurvivesNumberingRefusal(t *testing.T) {
	authored := packet.Size{Width: 599.5, Height: 799.5}
	target := packet.Size{Width: 600, Height: 800}
	for _, tc := range []struct {
		name   string
		policy *packet.FitPolicy
		want   packet.Size
	}{
		{"no fitting", nil, authored},
		{"kept", &packet.FitPolicy{Target: target, Tolerance: 1}, authored},
		{"moved", &packet.FitPolicy{Target: target, Tolerance: 0}, target},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := packet.PageStyle{Size: packet.Setting[packet.Size]{Present: true, Value: authored}}
			numbers := packet.DefaultPageNumbers()
			numbers.Margin = 900
			declared := app.Declared{Declaration: packet.Declaration{
				Fitting: tc.policy, PageNumbers: numbers, Items: []packet.Item{&packet.PageItem{Ref: "page", Count: 2, Style: style}},
			}}
			dir := t.TempDir()
			req := app.Request{InspectionWorkers: 1, Operation: app.Build, Input: declared, WorkingDir: dir, Output: "packet.pdf", Report: "review.json"}
			r := app.Execute(t.Context(), req)
			if !code(r, "text_overflow") || r.Status != "invalid" || r.Output.Published || r.Pages.Total == nil ||
				*r.Pages.Total != 2 || r.Structure.Coverage != nil || len(r.Parts) != 1 || !r.Report.Written {
				t.Fatalf("incorrect refusal stage: %+v", r)
			}
			p := r.Parts[0]
			if p.Size == nil || *p.Size != authored || p.OutputSize == nil || *p.OutputSize != tc.want || p.Text != nil || p.Pages == nil || *p.Pages != 2 {
				t.Fatalf("incorrect established part facts: %+v", p)
			}
			if _, err := os.Stat(filepath.Join(dir, req.Output)); !os.IsNotExist(err) {
				t.Fatalf("unexpected output: %v", err)
			}
			schemaValid(t, r)
		})
	}
}

func TestGeneratedOutputFramePreservesAuthoredTextCoordinates(t *testing.T) {
	authored := packet.Size{Width: 599.5, Height: 799.5}
	target := packet.Size{Width: 600, Height: 800}
	style := packet.PageStyle{
		Size: packet.Setting[packet.Size]{Present: true, Value: authored},
		Text: packet.TextStyle{Value: packet.Setting[string]{Present: true, Value: "Generated cover"}},
	}
	draft := packet.Declaration{Items: []packet.Item{&packet.PageItem{Ref: "cover", Style: style, Count: 1}}}
	req := app.Request{InspectionWorkers: 1, Operation: app.Check, WorkingDir: t.TempDir()}
	var bounds [4]float64
	for i, tolerance := range []float64{1, 0} {
		draft.Fitting = &packet.FitPolicy{Target: target, Tolerance: tolerance}
		req.Input = app.Declared{Declaration: draft}
		r := app.Execute(t.Context(), req)
		if r.Status != "ok" || len(r.Parts) != 1 {
			t.Fatal(r)
		}
		p := r.Parts[0]
		want := []packet.Size{authored, target}[i]
		if p.Size == nil || *p.Size != authored || p.OutputSize == nil || *p.OutputSize != want || p.Text == nil {
			t.Fatalf("tolerance %g: %+v", tolerance, p)
		}
		if i == 0 {
			bounds = p.Text.Bounds
		} else if bounds != p.Text.Bounds {
			t.Fatal("transformation changed the reported authored text bounds")
		}
	}
}
