// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestSourceFeedCancellationClosesAStalledQueue(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	queue := make(chan int)
	done := make(chan struct{})

	go func() { feedSourceIndexes(ctx, queue, 3); close(done) }()

	if first, open := <-queue; !open || first != 0 {
		t.Fatalf("first source index=%d open=%t", first, open)
	}

	cancel()
	requireClosedSourceFeed(t, queue, done)
}

func TestSourceFeedAlreadyCanceledEmitsNoIndex(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	queue := make(chan int)
	done := make(chan struct{})

	go func() { feedSourceIndexes(ctx, queue, 3); close(done) }()

	requireClosedSourceFeed(t, queue, done)
}

func requireClosedSourceFeed(t *testing.T, queue <-chan int, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled source producer remained blocked")
	}

	if index, open := <-queue; open {
		t.Fatalf("canceled queue emitted another index: %d", index)
	}
}

func TestBuiltinFontLoadsOnlyForAnActualDefaultFontAppearance(t *testing.T) {
	t.Parallel()

	font, pathErr := filepath.Abs("../typeset/fontdata/NotoSans-Regular.ttf")
	if pathErr != nil {
		t.Fatal(pathErr)
	}

	custom := `{"blank":{"size":"100x100","text":{"value":"custom","font":{"file":"` + filepath.ToSlash(font) + `"}}}}`
	for _, test := range []struct {
		items   string
		builtin bool
	}{
		{`"source.pdf"`, false},
		{custom, false},
		{custom + `,{"blank":{"size":"100x100","text":{"value":"default"}}}`, true},
	} {
		job, decodeErr := plan.Decode(t.Context(), plan.Input{Name: "fonts.json", BaseDir: t.TempDir()},
			strings.NewReader(`{"version":1,"items":[`+test.items+`]}`))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}

		flat, flattenErr := assembly.Flatten(job)
		if flattenErr != nil {
			t.Fatal(flattenErr)
		}

		workspace := openScratch(t)
		current := &pipeline{
			flat: flat, command: &cli.Command{Name: cli.NameCheck}, registry: capture.NewRegistry(),
			workspace: workspace, captures: capture.NewSet(workspace), builder: report.NewBuilder(checkName),
		}

		if fontErr := current.loadFonts(t.Context()); fontErr != nil {
			t.Fatal(fontErr)
		}

		if _, loaded := current.fonts.byPath[""]; loaded != test.builtin {
			t.Fatalf("default font loaded=%t want=%t for %s", loaded, test.builtin, test.items)
		}

		for index := range flat.Styles {
			if _, available := current.fonts.digest(flat.Styles[index].Style.Text.Font.Value.File); !available {
				t.Fatal("actual appearance has no resolved font digest")
			}
		}
	}
}
