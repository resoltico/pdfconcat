// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"fmt"
	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// visitContext measures cancellation checkpoints at the actual owned traversal boundary.
type visitContext struct {
	context.Context
	visits, stop int
}

func (c *visitContext) Err() error {
	c.visits++
	if c.stop > 0 && c.visits >= c.stop {
		return context.Canceled
	}
	return nil
}
func TestFontFailureProvenanceHasProportionalVisits(t *testing.T) {
	for _, count := range []int{100, 1000, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			dir := t.TempDir()
			font := filepath.Join(dir, "invalid.ttf")
			if err := os.WriteFile(font, []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			item := `{"blank":{"size":"100x100","text":{"value":"x","font":{"file":"invalid.ttf"}}}},`
			data := `{"version":1,"items":[` + strings.TrimSuffix(strings.Repeat(item, count), ",") + `]}`
			job, err := plan.Decode(t.Context(), plan.Input{Name: "cost.json", BaseDir: dir}, strings.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			flat, err := assembly.Flatten(job)
			if err != nil {
				t.Fatal(err)
			}
			if len(flat.Contributions) != count || len(flat.Fonts[0].Declarations) != count {
				t.Fatal("scale fixture not admitted")
			}
			workspace := openScratch(t)
			current := &pipeline{flat: flat, builder: report.NewBuilder("check"), workspace: workspace, registry: capture.NewRegistry(), captures: capture.NewSet(workspace)}
			measured := &visitContext{Context: t.Context()}
			if err := current.loadFonts(measured); err == nil {
				t.Fatal("invalid font accepted")
			}
			if measured.visits > 5*count+100 {
				t.Fatalf("owned visits=%d for %d contributions", measured.visits, count)
			}
			saved := current.builder.Build(current.status)
			if len(saved.Diagnostics) != count {
				t.Fatalf("lost declarations: %d", len(saved.Diagnostics))
			}
			for index, diagnostic := range saved.Diagnostics {
				if len(diagnostic.Consumers) != 1 || diagnostic.Consumers[0] != fmt.Sprintf("/items/%d", index) {
					t.Fatalf("incomplete provenance at %d: %+v", index, diagnostic)
				}
			}
			t.Logf("admitted=%d checkpoint_visits=%d emitted_references=%d", count, measured.visits, len(saved.Diagnostics))
			cancelled := &visitContext{Context: t.Context(), stop: 50}
			if err := current.indexFontConsumers(cancelled); err == nil || cancelled.visits != 50 {
				t.Fatalf("cancel visits=%d err=%v", cancelled.visits, err)
			}
		})
	}
}
func TestCancelledRegistrationDoesNotTouchFilesystem(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	current := &pipeline{flat: &assembly.Flattened{Files: []assembly.SourceFile{{Path: "missing.pdf"}}}, builder: report.NewBuilder("check")}
	// A nil registry would panic if registration proceeds past the cancelled boundary.
	if err := current.registerSources(ctx); err == nil || current.status != report.StatusInterrupted {
		t.Fatalf("registration: %v %s", err, current.status)
	}
}
