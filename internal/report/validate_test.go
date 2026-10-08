// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
)

// invariantCase edits a valid report in memory and names the fault Validate must report.
type invariantCase struct {
	edit    func(r *report.Report)
	name    string
	pointer string
	code    report.Code
}

func headerInvariantCases() []invariantCase {
	oneDiagnostic := func(r *report.Report, diagnostic report.Diagnostic) {
		r.Status = report.StatusInvalid
		r.Diagnostics = []report.Diagnostic{diagnostic}
	}

	return []invariantCase{
		{func(r *report.Report) { r.FormatVersion = 3 }, "version", "/format_version", report.CodeUnsupportedVersion},
		{func(r *report.Report) { r.Kind = "summary" }, "kind", "/kind", report.CodeWrongKind},
		{func(r *report.Report) { r.Command = commandReport }, "command ok but not build or check", pointerStatus, report.CodeInvalidValue},
		{func(r *report.Report) {
			r.Phases = report.Phases{
				Instructions: report.PhaseComplete, InputInspection: report.PhaseComplete,
				Layout: report.PhaseIncomplete, OutputVerification: report.PhaseNotRun,
			}
		}, "ok with layout incomplete", pointerStatus, report.CodeInvalidValue},
		{func(r *report.Report) { r.Command = testCommandBuild }, "build ok but not published", pointerStatus, report.CodeInvalidValue},
		{func(r *report.Report) {
			r.Command = testCommandBuild
			r.Publication.Published, r.Publication.Output = true, "/o.pdf"
		}, "build verification incomplete", pointerStatus, report.CodeInvalidValue},
		{func(r *report.Report) {
			r.Phases.Layout, r.Phases.OutputVerification = report.PhaseIncomplete, report.PhaseComplete
		}, "verification complete before layout", "/phases/output_verification", report.CodeInvalidValue},
		{func(r *report.Report) {
			big := int64(1<<63 - 1)
			r.Counts = report.Counts{SourcePages: &big, GeneratedPages: &big, TotalPages: &big}
		}, "counts overflow", "/counts", report.CodeInvalidValue},
		{func(r *report.Report) { r.Counts.TotalPages = new(int64(-5)) }, "negative total", "/counts/total_pages", report.CodeInvalidValue},
		{
			func(r *report.Report) { r.Counts.GeneratedPages = new(int64(-5)) },
			"negative generated", "/counts/generated_pages", report.CodeInvalidValue,
		},
		{func(r *report.Report) { r.Counts.SourcePages = nil }, "counts missing at complete layout", "/counts", report.CodeInvalidValue},
		{func(r *report.Report) {
			oneDiagnostic(r, report.Diagnostic{
				Stage: "s", Code: "c", Message: "m",
				Location: &report.Location{File: "f", Offset: new(int64(-1)), Line: 1, Column: 1},
			})
		}, "location offset negative", "/diagnostics/0/location/offset", report.CodeInvalidValue},
		{func(r *report.Report) {
			oneDiagnostic(r, report.Diagnostic{
				Stage: "s", Code: "c", Message: "m", Location: &report.Location{File: "f", Line: 2},
			})
		}, "location line without offset", "/diagnostics/0/location/line", report.CodeInvalidValue},
		{func(r *report.Report) {
			oneDiagnostic(r, report.Diagnostic{Stage: "s", Code: "c", Message: "m", Location: &report.Location{}})
		}, "location without file", "/diagnostics/0/location/file", report.CodeInvalidValue},
		{func(r *report.Report) {
			oneDiagnostic(r, report.Diagnostic{Code: "c", Message: "m"})
		}, "stage empty", "/diagnostics/0/stage", report.CodeInvalidValue},
		{func(r *report.Report) {
			oneDiagnostic(r, report.Diagnostic{Stage: "s", Code: "a b", Message: "m"})
		}, "code spaces", "/diagnostics/0/code", report.CodeInvalidValue},
		{func(r *report.Report) { r.Status = report.StatusFailed }, "no diagnostics on failure", "/diagnostics", report.CodeInvalidValue},
	}
}

func elementInvariantCases() []invariantCase {
	return []invariantCase{
		{func(r *report.Report) { r.Parts[0].Origin.File = "" }, "part origin file", "/parts/0/origin/file", report.CodeInvalidValue},
		{func(r *report.Report) { r.Parts[0].ID = "items/0" }, "part id", "/parts/0/id", report.CodeInvalidValue},
		{func(r *report.Report) { r.Parts[0].Kind = "x" }, "part kind", "/parts/0/kind", report.CodeInvalidValue},
		{func(r *report.Report) { r.Parts[0].Range.End = 9 }, "part range past pages", "/parts/0/range", report.CodeInvalidRange},
		{func(r *report.Report) {
			r.Parts[1].Pages = new(int64(assembly.MaxBlankCount + 1))
			r.Parts[1].Range = nil
			r.Phases.Layout = report.PhaseIncomplete
			r.Status = report.StatusFailed
			r.Diagnostics = []report.Diagnostic{{Stage: "s", Code: "c", Message: "m"}}
		}, "blank part too many pages", "/parts/1/pages", report.CodeInvalidValue},
		{func(r *report.Report) { r.Parts[0].Source = new(-1) }, "negative source index", "/parts/0/source", report.CodeDanglingReference},
		{func(r *report.Report) { r.Parts[1].Style = new(-1) }, "negative style index", "/parts/1/style", report.CodeDanglingReference},
		{func(r *report.Report) { r.Parts[0].Origin.Line = 0 }, "part location invalid", "/parts/0/origin/offset", report.CodeInvalidValue},
		{func(r *report.Report) { r.Styles[0].Background = "" }, "style background", "/styles/0/background", report.CodeInvalidValue},
		{func(r *report.Report) { r.Styles[0].Text.Bounds.X = nan() }, "text NaN bounds", "/styles/0/text/bounds", report.CodeInvalidValue},
		{func(r *report.Report) { r.Styles[0].Text.Size = nan() }, "text NaN size", "/styles/0/text/size", report.CodeInvalidValue},
		{func(r *report.Report) { r.Fonts[0].Digest = "x" }, "fonts digest", "/fonts/0/digest", report.CodeInvalidValue},
		{func(r *report.Report) {
			r.Styles[0].Text.Value = strings.Repeat("a", report.MaxTextRunes+1)
		}, "text too long", "/styles/0/text/value", report.CodeInvalidValue},
	}
}

// TestValidateRejectsEveryBrokenInvariant edits a valid report in memory, so rules the decoder reaches
// only through the prescan (or not at all) are also held for reports built by hand.
func TestValidateRejectsEveryBrokenInvariant(t *testing.T) {
	t.Parallel()

	for _, tc := range slices.Concat(headerInvariantCases(), elementInvariantCases()) {
		saved := mustDecode(t, completeCheck)
		tc.edit(saved)

		err := saved.Validate()

		found, ok := report.AsError(err)
		if !ok {
			t.Errorf("%s: %v is not a report error", tc.name, err)

			continue
		}

		located := found.Diagnostic.Location != nil && found.Diagnostic.Location.Pointer == tc.pointer
		if found.Diagnostic.Code != tc.code || !located || found.Status() != report.StatusInvalid {
			t.Errorf(namedErrorFormat, tc.name, err)
		}
	}
}

func TestValidAcceptsTheSamples(t *testing.T) {
	t.Parallel()

	for _, doc := range []string{completeCheck, failedCheck, richFailure} {
		if err := mustDecode(t, doc).Validate(); err != nil {
			t.Error(err)
		}
	}
}

func TestLocationOfConvertsAJobLocation(t *testing.T) {
	t.Parallel()

	got := report.LocationOf(assembly.Location{Source: jobName, Pointer: pointerItemTwo, Offset: 40, Line: 3, Column: 7})
	if got.File != jobName || got.Pointer != pointerItemTwo || *got.Offset != 40 || got.Line != 3 || got.Column != 7 {
		t.Errorf(detailFormat, got)
	}

	if got.ArgvIndex != nil {
		t.Errorf(detailFormat, got)
	}

	origin := report.OriginOf(assembly.Location{Source: jobName, Offset: 0, Line: 1, Column: 1})
	if origin.File != jobName || *origin.Offset != 0 {
		t.Errorf(detailFormat, origin)
	}
}

func TestLocationOfConvertsACommandLineOperand(t *testing.T) {
	t.Parallel()

	argv := report.LocationOf(assembly.Locate(assembly.ArgumentSource{}, assembly.Origin{Ref: 3}, ""))
	if argv.File != argvFile || *argv.ArgvIndex != 3 || argv.Pointer != "" || argv.Offset != nil || argv.Line != 0 {
		t.Errorf(detailFormat, argv)
	}

	odd := report.LocationOf(assembly.Location{Source: "x", Pointer: "argv:zz"})
	if odd.ArgvIndex != nil || odd.Pointer != "argv:zz" {
		t.Errorf(detailFormat, odd)
	}

	if report.ArgvID(4) != "argv:4" {
		t.Error("argv id")
	}
}

func TestLocationAndErrorTexts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		want string
		loc  report.Location
	}{
		{"f", report.Location{File: "f"}},
		{
			"f:2:3 (byte 9) at /items/1",
			report.Location{File: "f", Offset: new(int64(9)), Line: 2, Column: 3, Pointer: pointerItemOne},
		},
		{"argv argv:2", report.Location{File: argvFile, ArgvIndex: new(2)}},
	} {
		if got := tc.loc.String(); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}

	_, err := mustDecode(t, completeCheck).Query(report.Request{Part: new("nope")})
	if want := `no part has id "nope"; list ids with --view parts [report_part_not_found]`; err.Error() != want {
		t.Errorf("got %q", err.Error())
	}

	found, ok := report.AsError(errBroken)
	if ok || found != nil {
		t.Error("a foreign error is not a report error")
	}
}

// renderQuery renders the answer to a request as text.
func renderQuery(tb testing.TB, rep *report.Report, req report.Request) string {
	tb.Helper()

	var out bytes.Buffer

	err := queryOK(tb, rep, req).RenderText(&out)
	if err != nil {
		tb.Fatal(err)
	}

	return out.String()
}

// failedSynthetic is the synthetic failure with a failed report write that left a recovery report.
func failedSynthetic() *report.Report {
	failed, _ := syntheticFailure()
	failed.Publication.ReportStatus = report.ReportFailed
	failed.Publication.Published, failed.Publication.Output, failed.Publication.RecoveryReport = true, "/o.pdf", "/rec.json"
	failed.Publication.RecoveryState = report.RecoveryCurrent

	return failed
}

func TestRenderSummaryText(t *testing.T) {
	t.Parallel()

	summary := renderQuery(t, failedSynthetic(), report.Request{})

	counted := containsAll(summary, "summary only: 10000 parts, 5000 diagnostics", "recovery reference (current): /rec.json")
	if len(summary) > expectedSummaryBytes || strings.Count(summary, "\n- [") > 5 || !counted {
		t.Errorf("summary text (%d bytes):\n%s", len(summary), summary)
	}
}

func TestRenderPartAndPageText(t *testing.T) {
	t.Parallel()

	failed := failedSynthetic()

	part := renderQuery(t, failed, report.Request{Part: new(pointerItemOne)})
	if !strings.Contains(part, "/items/1 blank pages 2-3: 595x842 pt, background #ffffff, text ") {
		t.Errorf("part text: %s", part)
	}

	detail := renderQuery(t, failed, report.Request{Part: new(pointerItemOne), Details: true})
	if !strings.Contains(detail, `"name": "NotoSans-Regular"`) {
		t.Errorf("detailed part text: %.200s", detail)
	}

	pdf := renderQuery(t, failed, report.Request{Part: new(firstItemPointer), Details: true})
	if !strings.Contains(pdf, "/work/inputs/chapter-00000.pdf") {
		t.Errorf("pdf: %s", pdf)
	}

	page := renderQuery(t, failed, report.Request{Page: new(int64(3))})
	if !strings.HasPrefix(page, "page 3 is page 2 of its part\n/items/1 blank") {
		t.Errorf("page text: %.100s", page)
	}
}

func TestRenderViewText(t *testing.T) {
	t.Parallel()

	failed := failedSynthetic()

	const wantParts = "parts: 2 of 10000 from offset 0 (next offset 2)\n/items/0 pdf pages 1-1: /work/inputs/chapter-00000.pdf\n"

	parts := renderQuery(t, failed, report.Request{View: report.ViewParts, Limit: new(int64(2))})
	if !strings.HasPrefix(parts, wantParts) {
		t.Errorf("parts text: %.200s", parts)
	}

	const wantDiagnostics = "diagnostics: 1 of 5000 from offset 4999 (no more)\n- [layout/text_overflow] diagnostic 4999: "

	last := report.Request{View: report.ViewDiagnostics, Offset: new(int64(4999)), Limit: new(int64(5))}

	diagnostics := renderQuery(t, failed, last)
	if !strings.HasPrefix(diagnostics, wantDiagnostics) || !strings.Contains(diagnostics, "[message cut]") {
		t.Errorf("diagnostics text: %.300s", diagnostics)
	}
}

func TestRenderFullReportText(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := mustDecode(t, failedCheck).RenderText(&out)
	if err != nil {
		t.Fatal(err)
	}

	decoded := mustDecode(t, out.String())
	if decoded.Counts.TotalPages != nil || decoded.Parts[0].Range != nil || *decoded.Diagnostics[1].Location.ArgvIndex != 2 {
		t.Fatalf("lost complete report facts: %+v", decoded)
	}
}

func TestRenderOversizedRecordText(t *testing.T) {
	t.Parallel()

	huge := mustDecode(t, failedCheck)
	huge.Diagnostics[0].Message = strings.Repeat("x", 400_000)

	text := renderQuery(t, huge, report.Request{View: report.ViewDiagnostics, Details: true})
	if !strings.Contains(text, `"oversized_record": true`) {
		t.Errorf("oversized text: %.200s", text)
	}
}

func TestRenderTextReportsAWriteFailure(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, completeCheck)

	for _, req := range []report.Request{{}, {Part: new(firstItemPointer)}, {Page: new(int64(1))}, {View: report.ViewParts}} {
		err := queryOK(t, saved, req).RenderText(failingWriter{})
		if !errors.Is(err, errBroken) {
			t.Errorf("%+v: %v", req, err)
		}
	}
}

func TestCompleteHumanDetailsPropagateEncodingAndWriteFailures(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, completeCheck)

	saved.Styles[0].Size.Width = math.NaN()
	if err := saved.RenderText(io.Discard); err == nil {
		t.Fatal("invalid computed data encoded")
	}

	saved = mustDecode(t, completeCheck)
	if err := saved.RenderText(failingWriter{}); err == nil {
		t.Fatal("broken detail stream succeeded")
	}
}
