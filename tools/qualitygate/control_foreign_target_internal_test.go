// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestControlForeignTargetUsesValidatedStageAndRejectsModuleMismatch(t *testing.T) {
	t.Parallel()

	snapshot, staged := t.TempDir(), t.TempDir()
	source := repopolicy.ForeignSource{Root: "third_party/pdfcpu/pdfcpu", Module: "github.com/pdfcpu/pdfcpu"}
	stage := &foreignTestStage{
		sources: []repopolicy.ForeignSource{source},
		roots:   map[string]string{filepath.Join(snapshot, filepath.FromSlash(source.Root)): staged},
	}
	item := control{ID: "decoded-output-bound", Package: source.Module + "/pkg/filter", File: source.Root + "/pkg/filter/filter.go"}

	target, err := controlTarget(snapshot, &item, stage)
	if err != nil || target != filepath.Join(staged, "pkg", "filter", "filter.go") {
		t.Fatalf("control target = %q, %v; want staged source", target, err)
	}

	item.Package = "github.com/resoltico/pdfconcat/internal/pdfengine"
	if _, err = controlTarget(snapshot, &item, stage); err == nil {
		t.Fatal("foreign source mutation with owned package target accepted")
	}

	item.Package = source.Module + "/pkg/filter"

	item.File = "internal/pdfengine/engine.go"
	if _, err = controlTarget(snapshot, &item, stage); err == nil {
		t.Fatal("foreign package test with owned source mutation accepted")
	}

	item.File = source.Root + "/pkg/filter/filter.go"

	stage.roots = nil
	if _, err = controlTarget(snapshot, &item, stage); err == nil {
		t.Fatal("foreign control without validated staged root accepted")
	}

	for _, path := range []string{source.Root + "/../outside.go", "/outside.go", source.Root + `\outside.go`} {
		item.File = path
		if _, err = controlTarget(snapshot, &item, stage); err == nil {
			t.Fatalf("unsafe control path admitted: %s", path)
		}
	}
}
