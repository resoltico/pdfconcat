// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/plan"
)

// TestDocumentedExamplePlanDecodes keeps the example in docs/PLAN.md valid.
func TestDocumentedExamplePlanDecodes(t *testing.T) {
	t.Parallel()

	contract, err := os.ReadFile(filepath.Join("..", "..", "docs", "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}

	_, afterFence, found := strings.Cut(string(contract), "```json\n")
	if !found {
		t.Fatal("docs/PLAN.md has no json example")
	}

	example, _, found := strings.Cut(afterFence, "\n```")
	if !found {
		t.Fatal("docs/PLAN.md json example is not terminated")
	}

	document, err := decode(t, example)
	if err != nil {
		t.Fatalf("documented example does not decode: %v", err)
	}

	if document.Sequence.Items[0].Path != abs("source", "cover.pdf") || len(document.Sequence.Items) != 8 {
		t.Errorf("documented example decoded to %+v", document.Sequence.Items)
	}

	if document.Output != abs("Annex.pdf") || plan.Version != 1 {
		t.Errorf("documented example output = %q", document.Output)
	}
}
