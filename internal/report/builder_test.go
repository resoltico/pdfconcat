// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestBuilderInternsStylesAndFonts(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	font := builder.Font(report.Font{Digest: digestB, Name: fontName})
	size := report.PageSize{Origin: report.SizeExplicit, Width: 595, Height: 842}
	text := report.Text{
		Value:    "x",
		Color:    fixtureBlackColor,
		Anchor:   alignCenter,
		Align:    "left",
		Overflow: "error",
		Size:     12,
		Width:    400,
		Leading:  1.2,
		Font:     font,
	}
	styled := report.Style{Background: colorWhite, Size: size, Text: &text}

	first, again := builder.Style(&styled), builder.Style(&styled)
	plain := builder.Style(&report.Style{Background: report.BackgroundNone, Size: size})

	if first != again {
		t.Errorf("an equal style interned twice: %d and %d", first, again)
	}

	if first == plain {
		t.Errorf("a style with text and one without share index %d", first)
	}

	if builder.Font(report.Font{Digest: digestB, Name: fontName}) != font {
		t.Error("an equal font interned twice")
	}

	built := builder.Build(report.StatusInvalid)
	if len(built.Styles) != 2 || len(built.Fonts) != 1 {
		t.Errorf("tables: %d styles %d fonts", len(built.Styles), len(built.Fonts))
	}

	if built.Styles[first].Text == nil || built.Styles[plain].Text != nil {
		t.Errorf("the styles lost their text: %+v", built.Styles)
	}
}

func TestBuilderSnapshotsOptionalGeometryByValue(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	geometry := report.Rect{X: 1, Y: 2, Width: 3, Height: 4}
	first := report.Style{Text: &report.Text{Bounds: &geometry, InkBounds: &geometry}}
	separate := geometry
	second := report.Style{Text: &report.Text{Bounds: &separate, InkBounds: &separate}}

	index := builder.Style(&first)
	if got := builder.Style(&second); got != index {
		t.Fatalf("equal geometry through different pointers did not intern: %d %d", index, got)
	}

	geometry.X = 99

	built := builder.Build(report.StatusInvalid)
	if built.Styles[index].Text.Bounds.X != 1 || built.Styles[index].Text.InkBounds.X != 1 {
		t.Fatal("caller mutation changed captured geometry")
	}

	built.Styles[index].Text.Bounds.X = 88
	if builder.Build(report.StatusInvalid).Styles[index].Text.Bounds.X != 1 {
		t.Fatal("report mutation changed builder snapshot")
	}

	unknown := second
	settings := *second.Text
	settings.Bounds = nil

	unknown.Text = &settings
	if builder.Style(&unknown) == index {
		t.Fatal("unknown geometry interned as known geometry")
	}
}

func TestEveryStyleIdentityComponentDistinguishesInternedReports(t *testing.T) {
	t.Parallel()

	base := report.Style{
		Background: colorWhite, Size: report.PageSize{Origin: report.SizeExplicit, Width: 300, Height: 200},
		Text: &report.Text{
			Value: "x", Color: fixtureBlackColor, Anchor: alignCenter, Align: "left", Overflow: "error",
			Size: 12, Width: 200, Leading: 1.2, Bounds: &report.Rect{}, InkBounds: &report.Rect{},
		},
	}

	for _, edit := range []func(*report.Style){
		func(style *report.Style) { style.Background = report.BackgroundNone },
		func(style *report.Style) { style.Size.Width++ },
		func(style *report.Style) { style.Text.Value = "y" },
		func(style *report.Style) { style.Text.Font++ },
		func(style *report.Style) { style.Text = nil },
		func(style *report.Style) { style.Text.Bounds = &report.Rect{Width: 1} },
		func(style *report.Style) { style.Text.InkBounds = &report.Rect{Width: 1} },
		func(style *report.Style) { style.Text.Bounds = nil },
		func(style *report.Style) { style.Text.InkBounds = nil },
	} {
		builder := report.NewBuilder(testCommandCheck)
		original := builder.Style(&base)
		changed := base
		text := *base.Text
		changed.Text = &text
		edit(&changed)

		if builder.Style(&changed) == original {
			t.Fatal("distinct semantic style values share an interned record")
		}
	}
}

func TestBuilderSnapshotsAndDistinguishesComputedFindings(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	finding := report.TextFinding{Kind: findingOutsideHorizontal, Line: -1, Detail: "ink leaves page"}
	first := report.Style{Text: &report.Text{Findings: []report.TextFinding{finding}}}
	second := report.Style{Text: &report.Text{Findings: []report.TextFinding{finding}}}

	index := builder.Style(&first)
	if builder.Style(&second) != index {
		t.Fatal("equal separately allocated findings did not intern")
	}

	second.Text.Findings[0].Detail = "different geometry finding"
	if builder.Style(&second) == index {
		t.Fatal("different computed findings lost their own report style")
	}

	first.Text.Findings[0].Detail = "caller changed finding"

	built := builder.Build(report.StatusInvalid)
	if built.Styles[index].Text.Findings[0] != finding {
		t.Fatal("caller mutation changed captured finding")
	}

	built.Styles[index].Text.Findings[0].Detail = "report changed finding"
	if builder.Build(report.StatusInvalid).Styles[index].Text.Findings[0] != finding {
		t.Fatal("report mutation changed builder finding")
	}
}

func TestBuilderInternsSources(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	source := builder.Source(report.Source{Path: sourcePath, Digest: digestA, Bytes: new(int64(5))})
	again := builder.Source(report.Source{Path: sourcePath, Digest: digestA, Bytes: new(int64(5))})
	unsized := builder.Source(report.Source{Path: sourcePath})

	if source != again {
		t.Errorf("an equal source interned twice: %d and %d", source, again)
	}

	if source == unsized {
		t.Errorf("a source with a size and one without share index %d", source)
	}

	built := builder.Build(report.StatusInvalid)

	sized := built.Sources[source].Bytes
	if len(built.Sources) != 2 || sized == nil || *sized != 5 || built.Sources[unsized].Bytes != nil {
		t.Errorf("the sources lost their sizes: %+v", built.Sources)
	}
}

func TestBuilderBuildsAValidReport(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	source := builder.Source(report.Source{Path: sourcePath, Digest: digestA, Bytes: new(int64(5))})
	style := builder.Style(&report.Style{
		Background: "none", Size: report.PageSize{Origin: report.SizeExplicit, Width: 595, Height: 842},
	})
	origin := report.Position{File: argvFile}

	builder.AddPart(report.Part{
		ID: firstArgvID, Kind: report.PartPDF, Origin: origin,
		Range: &report.PageRange{Start: 1, End: 2}, Pages: new(int64(2)), Source: &source,
	})

	index := builder.AddPart(report.Part{
		ID: secondArgvID, Kind: report.PartBlank, Origin: origin,
		Range: &report.PageRange{Start: 3, End: 3}, Pages: new(int64(1)), Style: &style,
	})
	if index != 1 {
		t.Errorf("second part has index %d", index)
	}

	builder.SetPhases(report.Phases{
		Instructions:       report.PhaseComplete,
		InputInspection:    report.PhaseComplete,
		Layout:             report.PhaseComplete,
		OutputVerification: report.PhaseNotRun,
	})
	builder.SetCounts(report.Counts{SourcePages: new(int64(2)), GeneratedPages: new(int64(1)), TotalPages: new(int64(3))})
	builder.SetPublication(report.Publication{ReportStatus: report.ReportNotRequested})

	built := builder.Build(report.StatusOK)

	err := built.Validate()
	if err != nil {
		t.Fatal(err)
	}

	if built.Kind != report.KindReport || built.ReportVersion != report.Version {
		t.Errorf("kind %q version %d", built.Kind, built.ReportVersion)
	}

	mustDecode(t, encodeReport(t, built))
}

func TestBuilderOrdersDiagnosticsByInputNotByArrival(t *testing.T) {
	t.Parallel()

	const inputs, workers = 200, 8

	got := concurrentDiagnostics(inputs, workers)
	if len(got) != 2*inputs {
		t.Fatalf("%d diagnostics", len(got))
	}

	for i, diagnostic := range got {
		wantCode := []report.Code{"first", "second"}[i%2]
		if diagnostic.Message != strconv.Itoa(i/2) || diagnostic.Code != wantCode {
			t.Fatalf("diagnostic %d is input %s code %s", i, diagnostic.Message, diagnostic.Code)
		}
	}
}

// concurrentDiagnostics adds two diagnostics for each input from several workers that each take every
// workers-th input, in descending worker order, and returns the built list.
func concurrentDiagnostics(inputs, workers int) []report.Diagnostic {
	builder := report.NewBuilder(testCommandBuild)

	var group sync.WaitGroup

	for worker := range workers {
		group.Go(func() {
			for input := workers - 1 - worker; input < inputs; input += workers {
				for _, code := range []string{"first", "second"} {
					builder.AddDiagnostic(input, report.Diagnostic{Stage: "inspect", Code: report.Code(code), Message: strconv.Itoa(input)})
				}
			}
		})
	}

	group.Wait()

	return builder.Build(report.StatusInvalid).Diagnostics
}

func TestBuildDoesNotShareItsDiagnosticsAndKeepsAccumulating(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandBuild)
	builder.AddDiagnostic(1, report.Diagnostic{Stage: "s", Code: "late", Message: "m"})

	first := builder.Build(report.StatusInvalid)
	builder.AddDiagnostic(0, report.Diagnostic{Stage: "s", Code: "early", Message: "m"})

	second := builder.Build(report.StatusInvalid)
	if len(first.Diagnostics) != 1 || len(second.Diagnostics) != 2 || second.Diagnostics[0].Code != "early" {
		t.Errorf("%v / %v", first.Diagnostics, second.Diagnostics)
	}
}

func TestErrorReportIsTheOneErrorShape(t *testing.T) {
	t.Parallel()

	saved := report.NewErrorReport(commandReport, report.StatusInvalid,
		report.Diagnostic{Stage: report.StageUsage, Code: report.CodeSelectionConflict, Message: "one"},
		report.Diagnostic{Stage: report.StageUsage, Code: report.CodeInvalidPaging, Message: "two"},
	)

	err := saved.Validate()
	if err != nil {
		t.Fatal(err)
	}

	phasesOK := saved.Phases.Instructions == report.PhaseIncomplete && saved.Phases.Layout == report.PhaseNotRun
	if !phasesOK || saved.Counts.TotalPages != nil || saved.Diagnostics[1].Message != "two" {
		t.Errorf(detailFormat, saved)
	}

	if text := encodeReport(t, saved); mustDecode(t, text).Command != commandReport {
		t.Error("round trip lost the command")
	}
}

func TestHexDigest(t *testing.T) {
	t.Parallel()

	if got := report.HexDigest([32]byte{0xab, 0x01}); got != "ab01"+strings.Repeat("0", 60) {
		t.Errorf("got %s", got)
	}
}
