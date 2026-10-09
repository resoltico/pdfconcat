// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func fittedCapturedFixture(t *testing.T) *report.Report {
	t.Helper()
	saved := mustDecode(t, completeCheck)
	saved.Fit = &report.FitDeclaration{
		Paper: "Legal",
		Size:  report.PageSize{Origin: report.SizeFitTarget, Width: 612, Height: 1008},
	}
	saved.Geometries = []report.Geometry{{
		Media: [4]float64{0, 0, 100, 200}, Crop: [4]float64{0, 0, 100, 200},
		Visible: [4]float64{0, 0, 100, 200}, Matrix: [6]float64{5.04, 0, 0, 5.04, 54, 0},
		Width: 100, Height: 200, Scale: 5.04, UserUnit: 1,
	}}
	saved.Sources[0].Geometries = []report.GeometryRange{{First: 1, Last: 3, Geometry: 0}}
	style := &saved.Styles[0]
	style.Geometry = new(0)
	style.Size = report.PageSize{Origin: report.SizeExplicit, Width: 100, Height: 200}
	style.Text.Width = 80
	style.Text.Bounds = &report.Rect{X: 10, Y: 20, Width: 30, Height: 14}
	style.FinalText = &report.TextPlacement{
		FontSize: 60.48,
		Bounds:   &report.Rect{X: 104.4, Y: 100.8, Width: 151.2, Height: 70.56},
	}

	return saved
}

func TestFittedCapturedReportAndEverySelectedQueryMatchSchemas(t *testing.T) {
	t.Parallel()
	saved := fittedCapturedFixture(t)

	encoded := encodeReport(t, saved)
	if schemaVerdict(t, reportSchema(t), []byte(encoded)) != schemaAccept {
		t.Fatal("report schema refused captured fit")
	}

	decoded := mustDecode(t, encoded)
	responseSchema := compileSchema(t, report.ResponseSchema(), responseSchemaURL)

	for _, request := range []report.Request{
		{Page: new(int64(2))},
		{Page: new(int64(4)), Details: true},
		{Part: new("/items/0"), Details: true},
		{Part: new(pointerItemOne), Details: true},
		{View: report.ViewParts, Details: true},
		{View: report.ViewDiagnostics},
	} {
		response, err := decoded.Query(request)
		if err != nil {
			t.Fatal(err)
		}

		query := report.QueryResultOf(decoded, response, "pdfconcat", "/saved-fit.json")

		payload, err := report.Encode(query)
		if err != nil || schemaVerdict(t, responseSchema, payload) != schemaAccept {
			t.Fatalf("selected fit schema refused request %+v: %v", request, err)
		}
	}

	selected, err := decoded.Query(report.Request{Page: new(int64(4)), Details: true})
	if err != nil {
		t.Fatal(err)
	}

	page, ok := report.ContentOf[report.PageResponse](selected)
	if !ok || page.Part.Style.Size.Width != 612 || page.Part.Style.CanvasSize.Width != 100 ||
		page.Part.Style.FinalText.FontSize != 60.48 || page.Part.Style.Text.Size != 12 {
		t.Fatal("query conflated authored and final physical placement")
	}
}

func TestCapturedFitRejectsMalformedGeometryAndFinalPlacement(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(*report.Report){
		"unsupported target":              func(r *report.Report) { r.Fit.Paper = "Letter" },
		"empty original box":              func(r *report.Report) { r.Geometries[0].Media[2] = 0 },
		"visible outside crop":            func(r *report.Report) { r.Geometries[0].Visible[0] = -1 },
		"undeclared crop difference":      func(r *report.Report) { r.Geometries[0].Crop[2] = 101 },
		"zero physical width":             func(r *report.Report) { r.Geometries[0].Width = 0 },
		"zero physical scale":             func(r *report.Report) { r.Geometries[0].Scale = 0 },
		"unresolved rotation":             func(r *report.Report) { r.Geometries[0].Rotation = 450 },
		"orientation reversing matrix":    func(r *report.Report) { r.Geometries[0].Matrix[0] = -5.04 },
		"nonconsecutive range":            func(r *report.Report) { r.Sources[0].Geometries[0].First = 2 },
		"negative reference":              func(r *report.Report) { r.Styles[0].Geometry = new(-1) },
		"missing resolved canvas":         func(r *report.Report) { r.Styles[0].Geometry = nil; r.Styles[0].FinalText = nil },
		"placement without geometry":      func(r *report.Report) { r.Styles[0].Geometry = nil },
		"physical font zero":              func(r *report.Report) { r.Styles[0].FinalText.FontSize = 0 },
		"physical bounds invalid":         func(r *report.Report) { r.Styles[0].FinalText.Bounds.Width = -1 },
		"physical ink invalid":            func(r *report.Report) { r.Styles[0].FinalText.InkBounds = &report.Rect{Width: -1} },
		"placement without authored text": func(r *report.Report) { r.Styles[0].Text = nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			saved := fittedCapturedFixture(t)
			if err := saved.Validate(); err != nil {
				t.Fatalf("valid baseline: %v", err)
			}

			edit(saved)

			if err := saved.Validate(); err == nil {
				t.Fatal("malformed captured fitting facts accepted")
			}
		})
	}
}

func TestCapturedFitBlankWithoutTextKeepsPhysicalCanvasOnly(t *testing.T) {
	t.Parallel()
	saved := fittedCapturedFixture(t)
	saved.Styles[0].Text = nil

	saved.Styles[0].FinalText = nil
	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	response, err := saved.Query(report.Request{Page: new(int64(4)), Details: true})
	if err != nil {
		t.Fatal(err)
	}

	page, ok := report.ContentOf[report.PageResponse](response)
	if !ok || page.Part.Style.FinalText != nil || page.Part.Style.Text != nil || page.Part.FinalSize.Height != 1008 {
		t.Fatal("empty fitted canvas acquired text or lost sheet geometry")
	}
}
