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
	jsonInput := app.PlanBytes{Data: []byte(`{"version":6,"metadata":{"language":"ro"},"page_defaults":{"size":"Letter","text":{"value":"ş"}},"items":[{"contents":{}},{"title":"Annex","divider":{},"items":[{"source":"source.pdf","pages":[[1,2]]},{"page":{},"count":2}]}]}`)}
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
