// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package app

import (
	"context"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestCanceledFontLoadingDoesNotFanOutDiagnostics(t *testing.T) {
	items := strings.TrimSuffix(strings.Repeat(`{"blank":{"size":"100x100","text":{"value":"hello","font":{"file":"missing.ttf"}}}},`, 20), ",")
	job, err := plan.Decode(t.Context(), plan.Input{Name: "fonts.json", BaseDir: t.TempDir()}, strings.NewReader(`{"version":1,"items":[`+items+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	flat, err := assembly.Flatten(job)
	if err != nil {
		t.Fatal(err)
	}
	workspace := openScratch(t)
	current := &pipeline{flat: flat, registry: capture.NewRegistry(), captures: capture.NewSet(t.Context(), workspace), workspace: workspace, builder: report.NewBuilder("check")}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if loadingErr := current.loadFonts(ctx); loadingErr == nil {
		t.Fatal("cancelled loading succeeded")
	}
	if current.status != report.StatusInterrupted {
		t.Fatalf("status=%s, want interrupted", current.status)
	}
}
