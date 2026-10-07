// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

const canceledLayoutText = "original unplaced text"

func TestCanceledResolvedLayoutSavesTextWithoutInventingPlacement(t *testing.T) {
	t.Parallel()

	for _, command := range []cli.Name{cli.NameCheck, cli.NameBuild} {
		t.Run(string(command), func(t *testing.T) {
			t.Parallel()
			assertCanceledResolvedLayout(t, command)
		})
	}
}

func assertCanceledResolvedLayout(t *testing.T, command cli.Name) {
	t.Helper()

	origin := assembly.Origin{Ref: 9}
	style := assembly.BlankStyle{
		Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: 100, Height: 100}}, origin),
		Text: assembly.TextStyle{Value: assembly.Set(canceledLayoutText, origin)},
	}

	item, err := assembly.NewBlankItem(origin, &style, 2)
	if err != nil {
		t.Fatal(err)
	}

	job := &assembly.Job{Source: assembly.ArgumentSource{}, Base: t.TempDir(), Items: []assembly.Item{item}}

	flat, err := assembly.Flatten(job)
	if err != nil {
		t.Fatal(err)
	}

	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}

	current := &pipeline{
		job:         job,
		flat:        flat,
		command:     &cli.Command{Name: command},
		fonts:       loadedFonts{byPath: map[string]*typeset.Font{"": font}},
		builder:     report.NewBuilder(string(command)),
		progress:    newProgress(nil, time.Now),
		workspace:   openScratch(t),
		publication: report.Publication{ReportStatus: report.ReportNotRequested},
		phases: report.Phases{
			Instructions:       report.PhaseComplete,
			InputInspection:    report.PhaseComplete,
			Layout:             report.PhaseIncomplete,
			OutputVerification: report.PhaseNotRun,
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if resolveErr := current.resolveLayout(ctx); !errors.Is(resolveErr, errStopped) {
		t.Fatalf("canceled layout: %v", resolveErr)
	}

	if current.layout == nil || current.resource != nil {
		t.Fatal("resolved layout or canceled resource state lost")
	}

	current.describe()

	assertSerializedCanceledLayout(t, current)
}

func assertSerializedCanceledLayout(t *testing.T, current *pipeline) {
	t.Helper()

	var saved bytes.Buffer
	if _, writeErr := report.Write(&saved, current.snapshot(current.status), report.MaxReportBytes); writeErr != nil {
		t.Fatal(writeErr)
	}

	decoded, err := report.Decode(t.Context(), "canceled.json", &saved)
	if err != nil {
		t.Fatal(err)
	}

	assertCanceledLayoutReport(t, decoded)
}

func assertCanceledLayoutReport(t *testing.T, saved *report.Report) {
	t.Helper()

	if saved.Status != report.StatusInterrupted || saved.Phases.Layout != report.PhaseIncomplete || len(saved.Parts) != 1 ||
		len(saved.Styles) != 1 {
		t.Fatalf("canceled report state: %+v", saved)
	}

	assertCanceledReportPart(t, &saved.Parts[0])
	assertCanceledReportStyle(t, &saved.Styles[0])

	if len(saved.Diagnostics) != 1 || saved.Diagnostics[0].Code != codeInterrupted {
		t.Fatalf("cancellation diagnostic: %+v", saved.Diagnostics)
	}
}

func assertCanceledReportPart(t *testing.T, part *report.Part) {
	t.Helper()

	if part.ID != "argv:9" || part.Origin.File != "argv" || part.Pages == nil || *part.Pages != 2 || part.Range == nil ||
		*part.Range != (report.PageRange{Start: 1, End: 2}) {
		t.Fatalf("original contribution lost: %+v", part)
	}
}

func assertCanceledReportStyle(t *testing.T, style *report.Style) {
	t.Helper()

	if style.Size.Width != 100 || style.Size.Height != 100 || style.Text == nil || style.Text.Value != canceledLayoutText {
		t.Fatalf("resolved appearance lost: %+v", style)
	}

	if style.Text.Bounds != nil || style.Text.InkBounds != nil || len(style.Text.Findings) != 0 {
		t.Fatalf("unavailable placement invented: %+v", style.Text)
	}
}
