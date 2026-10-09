// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestFitProjectionPreservesAuthoredCanvasAndClipsFinalInk(t *testing.T) {
	t.Parallel()

	current := &pipeline{
		command: &cli.Command{Name: cli.NameCheck, FitTo: assembly.FitLegal},
		builder: report.NewBuilder(checkName),
		job:     &assembly.Job{Source: assembly.ArgumentSource{}, FitTo: assembly.Set(assembly.FitA4, assembly.Origin{})},
		env:     Env{Arguments: []string{checkName, "--fit-to=Legal"}},
		layout: &assembly.Layout{
			Specs: []assembly.ResolvedSpec{{Spec: assembly.BlankSpec{Dim: assembly.PageDim{Width: 150, Height: 300}}}},
		},
	}
	if err := current.resolveFit(); err != nil {
		t.Fatal(err)
	}

	if err := current.captureGeneratedFits(); err != nil {
		t.Fatal(err)
	}

	style := report.Style{
		Text: &report.Text{
			Size:      10,
			Bounds:    &report.Rect{X: 20, Y: 30, Width: 40, Height: 50},
			InkBounds: &report.Rect{X: -10, Y: 20, Width: 30, Height: 40},
		},
	}
	current.fitStyle(&style, 0)

	if style.Text.Size != 10 || style.Text.InkBounds.X != -10 {
		t.Fatal("fitting changed authored text")
	}

	requireFitProjection(t, style.FinalText)

	assertCapturedFitDeclaration(t, current.builder.Build(report.StatusOK))

	empty := report.Style{}
	current.fitStyle(&empty, 0)

	if empty.Geometry == nil || empty.FinalText != nil {
		t.Fatal("empty canvas acquired text")
	}

	style.Text.InkBounds = &report.Rect{X: -40, Y: 20, Width: 10, Height: 10}
	current.fitStyle(&style, 0)

	if style.FinalText.InkBounds != nil {
		t.Fatal("wholly hidden authored ink became visible")
	}

	style.Text.Bounds, style.Text.InkBounds = nil, nil
	current.fitStyle(&style, 0)

	if style.FinalText.Bounds != nil || style.FinalText.InkBounds != nil {
		t.Fatal("unmeasured text acquired invented physical bounds")
	}
}

func assertCapturedFitDeclaration(t *testing.T, recorded *report.Report) {
	t.Helper()

	if recorded.Fit.Paper != "Legal" || recorded.Fit.Location == nil || recorded.Fit.Location.ArgvIndex == nil ||
		*recorded.Fit.Location.ArgvIndex != 1 {
		t.Fatal("CLI override lost effective declaration")
	}

	if len(recorded.Geometries) != 1 || recorded.Geometries[0].Width != 150 || recorded.Geometries[0].Height != 300 {
		t.Fatal("original canvas facts lost")
	}
}

func requireFitProjection(t *testing.T, placement *report.TextPlacement) {
	t.Helper()

	if placement == nil || placement.FontSize != 33.6 {
		t.Fatalf("physical font size: %+v", placement)
	}

	expected := report.Rect{X: 121.2, Y: 100.8, Width: 134.4, Height: 168}
	for _, pair := range [][2]float64{
		{placement.Bounds.X, expected.X},
		{placement.Bounds.Y, expected.Y},
		{placement.Bounds.Width, expected.Width},
		{placement.Bounds.Height, expected.Height},
	} {
		if pair[0]-pair[1] > 1e-10 || pair[1]-pair[0] > 1e-10 {
			t.Fatalf("wrong projected bounds: %+v", placement.Bounds)
		}
	}

	if placement.InkBounds == nil || placement.InkBounds.X < 54-1e-10 || placement.InkBounds.X > 54+1e-10 ||
		placement.InkBounds.Width < 67.2-1e-10 ||
		placement.InkBounds.Width > 67.2+1e-10 {
		t.Fatalf("visible clipped ink: %+v", placement.InkBounds)
	}
}

func TestFitProjectionRejectsInvalidResolvedCanvas(t *testing.T) {
	t.Parallel()

	current := &pipeline{
		command: &cli.Command{Name: cli.NameCheck},
		builder: report.NewBuilder(checkName),
		job:     &assembly.Job{Source: assembly.ArgumentSource{}},
		layout:  &assembly.Layout{Specs: []assembly.ResolvedSpec{{}}},
	}
	if err := current.resolveFit(); err != nil {
		t.Fatal(err)
	}

	if err := current.captureGeneratedFits(); err != nil {
		t.Fatal(err)
	}

	current.job.FitTo = assembly.Set(assembly.FitLegal, assembly.Origin{})
	if err := current.resolveFit(); err != nil {
		t.Fatal(err)
	}

	if err := current.captureGeneratedFits(); !errors.Is(err, errStopped) {
		t.Fatalf("invalid canvas accepted: %v", err)
	}

	current.command.FitTo = assembly.FitTarget("Letter")
	if err := current.resolveFit(); !errors.Is(err, errStopped) {
		t.Fatalf("invalid target accepted: %v", err)
	}
}
