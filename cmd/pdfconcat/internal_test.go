// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// TestRunChecksASourceInProcess drives run itself, so that the wiring of the working directory and of the PDF
// engine is judged by this package's own tests: a check that reads a real PDF and saves its report only
// succeeds when both were wired. It is the only test of this package that replaces [os.Args].
func TestRunChecksASourceInProcess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "a.pdf")
	saved := filepath.Join(dir, "report.json")

	err := pdffixture.Pages("a", 1).WriteFile(source)
	if err != nil {
		t.Fatal(err)
	}

	previous := os.Args

	t.Cleanup(func() { os.Args = previous })

	os.Args = []string{"pdfconcat", "check", "--report", saved, "--format", "text", source}

	if code := run(); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}

	content, err := os.ReadFile(filepath.Clean(saved))
	if err != nil {
		t.Fatalf("the report was not saved beside the working directory: %v", err)
	}

	var parsed struct {
		Status string `json:"status"`
	}

	err = json.Unmarshal(content, &parsed)
	if err != nil || parsed.Status != "ok" {
		t.Errorf("saved report status %q (%v)", parsed.Status, err)
	}
}

func TestUsableExecutableRejectsPathReturnedWithError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pdfconcat")

	if got := usableExecutable(path, nil); got != path {
		t.Fatalf("successful executable path: %q", got)
	}

	if got := usableExecutable("relative/pdfconcat", os.ErrNotExist); got != "" {
		t.Fatalf("failed lookup supplied continuation authority: %q", got)
	}

	if got := usableExecutable(string([]byte{0xff})+"/pdfconcat", nil); got != "" {
		t.Fatalf("invalid UTF-8 path supplied lossy continuation authority: %q", got)
	}
}
