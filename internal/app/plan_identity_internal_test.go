// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestPlanOpenedIdentityRemainsProtectedAfterNativeReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	original := []byte(`{"version":1,"items":[{"blank":{"size":"100x100"}}]}`)
	replacement := []byte(`{"version":1,"output":"plan.json","items":[{"blank":{"size":"100x100"}}]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	current := &pipeline{command: &cli.Command{Name: cli.NameCheck, PlanSource: cli.PlanFile, PlanPath: path, Overwrite: true}, env: Env{WorkingDir: dir}, registry: capture.NewRegistry(), builder: report.NewBuilder("check")}
	if err := current.loadPlanFileWith(t.Context(), func(ctx context.Context, selected string) (*os.File, error) {
		if err := os.Rename(selected, filepath.Join(dir, "earlier-plan.json")); err != nil {
			return nil, err
		}
		if err := os.WriteFile(selected, replacement, 0o600); err != nil {
			return nil, err
		}
		return openInput(ctx, selected)
	}); err != nil {
		t.Fatal(err)
	}
	if err := current.resolveOutput(); err != nil {
		t.Fatal(err)
	}
	if err := current.flatten(); err != nil {
		t.Fatal(err)
	}
	if err := current.checkDestinations(); err == nil || current.status != report.StatusInvalid {
		t.Fatalf("opened plan identity was not protected: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	for name, want := range map[string][]byte{"plan.json": replacement, "earlier-plan.json": original} {
		actual, err := root.ReadFile(name)
		if err != nil || string(actual) != string(want) {
			t.Fatalf("plan replacement control changed %s: %q/%v", name, actual, err)
		}
	}
}
