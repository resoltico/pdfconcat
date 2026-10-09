// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

const capturedFitFile = "fit-report.json"

func capturedFitReport() *Report {
	builder := NewBuilder(commandCheck)
	builder.SetFit(&FitDeclaration{Paper: "Legal", Size: PageSize{Origin: SizeFitTarget, Width: 612, Height: 1008}})
	geometry := builder.Geometry(Geometry{
		Media: [4]float64{0, 0, 100, 200}, Crop: [4]float64{0, 0, 100, 200},
		Visible: [4]float64{0, 0, 100, 200}, Matrix: [6]float64{5.04, 0, 0, 5.04, 54, 0}, Width: 100, Height: 200, Scale: 5.04, UserUnit: 1,
	})
	source := builder.Source(Source{Path: "/fit.pdf", Geometries: []GeometryRange{{First: 1, Last: 3, Geometry: geometry}}})
	pages := int64(3)
	builder.AddPart(
		Part{
			ID:     "argv:0",
			Kind:   PartPDF,
			Source: &source,
			Pages:  &pages,
			Range:  &PageRange{Start: 1, End: 3},
			Origin: Position{File: featureArgv},
		},
	)

	zero := int64(0)
	builder.SetCounts(Counts{SourcePages: &pages, GeneratedPages: &zero, TotalPages: &pages})
	builder.SetPhases(
		Phases{Instructions: PhaseComplete, InputInspection: PhaseComplete, Layout: PhaseComplete, OutputVerification: PhaseNotRun},
	)

	return builder.Build(StatusOK)
}

func TestCapturedFitCodecCountsAndSelectedPageResolveSharedFacts(t *testing.T) {
	t.Parallel()

	report := capturedFitReport()

	var buffer bytes.Buffer
	if _, err := Write(&buffer, report, MaxReportBytes); err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(t.Context(), capturedFitFile, bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded.Geometries) != 1 || len(decoded.Sources[0].Geometries) != 1 {
		t.Fatal("captured fit lost range/table compression")
	}

	page := int64(2)

	response, err := decoded.Query(Request{Page: &page})
	if err != nil {
		t.Fatal(err)
	}

	selected, ok := ContentOf[PageResponse](response)
	if !ok || selected.Geometry == nil || selected.Geometry.Width != 100 || selected.Part.FinalSize.Height != 1008 {
		t.Fatalf("selected page did not resolve captured facts: %+v", selected)
	}

	limits := DefaultLimits()

	limits.MaxNodes = report.nodes()
	if _, err = DecodeLimited(t.Context(), capturedFitFile, bytes.NewReader(buffer.Bytes()), limits); err != nil {
		t.Fatalf("exact node budget rejected captured geometry: %v", err)
	}

	limits.MaxNodes--
	if _, err = DecodeLimited(t.Context(), capturedFitFile, bytes.NewReader(buffer.Bytes()), limits); err == nil {
		t.Fatal("one-less node budget accepted captured geometry")
	}
}

func TestCapturedFitRefusalsRejectMissingRangesAndDanglingReferences(t *testing.T) {
	t.Parallel()

	for _, edit := range []func(*Report){
		func(r *Report) { r.Sources[0].Geometries[0].Last = 2 },
		func(r *Report) { r.Sources[0].Geometries[0].Geometry = 99 },
		func(r *Report) { r.Geometries[0].Matrix = [6]float64{} },
		func(r *Report) { r.Fit.Size.Width = 600 },
		func(r *Report) { r.Fit = nil },
		func(r *Report) { r.Geometries[0].Matrix[0] = math.NaN() },
		func(r *Report) { r.Geometries[0].Media[0] = math.Inf(-1) },
	} {
		report := capturedFitReport()
		edit(report)

		if err := report.Validate(); err == nil {
			t.Fatal("invalid captured fit accepted")
		}
	}
}

func TestCapturedFitLocationNodeBudget(t *testing.T) {
	t.Parallel()

	for _, location := range []*Location{
		{File: featureArgv, ArgvIndex: new(2)},
		{File: "/plan.json", Pointer: "/fit_to", Line: 3, Column: 5, Offset: new(int64(24))},
	} {
		report := capturedFitReport()
		report.Fit.Location = location

		var buffer bytes.Buffer
		if _, err := Write(&buffer, report, MaxReportBytes); err != nil {
			t.Fatal(err)
		}

		limits := DefaultLimits()

		limits.MaxNodes = report.nodes()
		if _, err := DecodeLimited(t.Context(), "fit-location.json", bytes.NewReader(buffer.Bytes()), limits); err != nil {
			t.Fatalf("exact location node budget: %v", err)
		}

		limits.MaxNodes--
		if _, err := DecodeLimited(t.Context(), "fit-location.json", bytes.NewReader(buffer.Bytes()), limits); err == nil {
			t.Fatal("one-less location budget admitted")
		}
	}
}

func TestCapturedFitBuilderOwnsFinalPlacementAndDeclaration(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(commandCheck)
	declaration := &FitDeclaration{Location: &Location{File: featureArgv, ArgvIndex: new(2)}}
	builder.SetFit(declaration)
	geometry := builder.Geometry(Geometry{})
	style := &Style{Geometry: &geometry, FinalText: &TextPlacement{
		FontSize: 60,
		Bounds:   &Rect{X: 30, Width: 40}, InkBounds: &Rect{X: 31, Width: 39},
	}}

	first := builder.Style(style)
	if builder.Style(style) != first {
		t.Fatal("equal captured placement failed to intern")
	}

	style.FinalText.FontSize = 61
	if builder.Style(style) == first {
		t.Fatal("distinct final physical size conflated")
	}

	style.FinalText.Bounds.X, style.FinalText.InkBounds.X = 99, 99
	*style.Geometry = 99
	declaration.Location.File = "changed"

	snapshot := builder.Build(StatusInvalid)
	if snapshot.Styles[first].FinalText.Bounds.X != 30 || snapshot.Styles[first].FinalText.InkBounds.X != 31 ||
		*snapshot.Styles[first].Geometry != 0 || snapshot.Fit.Location.File != featureArgv {
		t.Fatal("caller mutation changed captured fit")
	}

	snapshot.Styles[first].FinalText.Bounds.X = 88

	snapshot.Fit.Location.File = "changed again"
	if fresh := builder.Build(StatusInvalid); fresh.Styles[first].FinalText.Bounds.X != 30 || fresh.Fit.Location.File != featureArgv {
		t.Fatal("snapshot mutation changed builder fit")
	}
}

func TestCapturedFitRepeatedSourceCannotDisagreeOnPageCount(t *testing.T) {
	t.Parallel()

	captured := capturedFitReport()
	repeated := captured.Parts[0]
	repeated.ID = "argv:1"
	repeated.Pages = new(int64(4))
	repeated.Range = &PageRange{Start: 4, End: 7}
	captured.Parts = append(captured.Parts, repeated)
	captured.Counts.SourcePages = new(int64(7))
	captured.Counts.TotalPages = new(int64(7))

	failure, found := errors.AsType[*Error](captured.Validate())
	if !found || failure.Diagnostic.Location == nil || failure.Diagnostic.Location.Pointer != "/parts/1/pages" {
		t.Fatalf("repeated source count contradiction was not located: %v", failure)
	}
}
