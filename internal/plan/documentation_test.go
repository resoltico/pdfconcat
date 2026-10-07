// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

func readPlanDoc(tb testing.TB) string {
	tb.Helper()

	contract, err := os.ReadFile(filepath.Join("..", "..", "docs", "PLAN.md"))
	if err != nil {
		tb.Fatal(err)
	}

	return string(contract)
}

// documentedExample extracts the first json block of docs/PLAN.md.
func documentedExample(tb testing.TB) string {
	tb.Helper()

	_, afterFence, found := strings.Cut(readPlanDoc(tb), "```json\n")
	if !found {
		tb.Fatal("docs/PLAN.md has no json example")
	}

	example, _, found := strings.Cut(afterFence, "\n```")
	if !found {
		tb.Fatal("docs/PLAN.md json example is not terminated")
	}

	return example
}

// TestDocumentedExamplePlanDecodes keeps the example in docs/PLAN.md valid and accepted by the schema.
func TestDocumentedExamplePlanDecodes(t *testing.T) {
	t.Parallel()

	example := documentedExample(t)

	job, err := plan.Decode(context.Background(), plan.Input{Name: "PLAN.md", BaseDir: "/plan"}, strings.NewReader(example))
	if err != nil {
		t.Fatalf("documented example does not decode: %v", err)
	}

	if got := schemaVerdict(t, planSchema(t), []byte(example)); got != schemaAccept {
		t.Errorf("schema verdict for the documented example: %s", got)
	}

	if len(job.Items) != 6 || job.Items[0].Path != "cover.pdf" || job.Output.Value != "Annex.pdf" || job.Dir.Value != "source" {
		t.Errorf("documented example decoded to %+v", job)
	}

	if font := job.Defaults.Text.Font.Value; font != (assembly.Font{File: "fonts/Example.ttf", Base: "/plan"}) {
		t.Errorf("default font %+v", font)
	}

	if background := job.Items[3].Blank.Style.Background; !background.IsSet() || background.Value.Painted {
		t.Error(`the documented "background": "none" must be an explicit unpainted value`)
	}
}

// TestDocumentedLimitsMatchTheCode keeps the limits table in docs/PLAN.md equal to the declared bounds.
func TestDocumentedLimitsMatchTheCode(t *testing.T) {
	t.Parallel()

	contract := readPlanDoc(t)

	for _, want := range []string{
		"| Plan size | 64 MiB", "| Nested groups | 64 |", "| 100,000 |", "| 250,000 |", "| One blank item's `count` | 1,000,000 |",
		"| Generated pages in total | 1,000,000 |",
	} {
		if !strings.Contains(contract, want) {
			t.Errorf("docs/PLAN.md lacks %q", want)
		}
	}

	bounds := map[string]struct{ got, want int }{
		"plan bytes": {
			plan.MaxPlanBytes,
			64 << 20,
		},
		"group depth":   {assembly.MaxGroupDepth, 64},
		"contributions": {assembly.MaxContributions, 100_000},
		"nodes": {
			assembly.MaxNodes,
			250_000,
		},
		"blank count":     {assembly.MaxBlankCount, 1_000_000},
		"generated pages": {assembly.MaxGeneratedPages, 1_000_000},
	}

	for name, bound := range bounds {
		if bound.got != bound.want {
			t.Errorf("%s is %d; docs/PLAN.md documents %d: update both together", name, bound.got, bound.want)
		}
	}
}
