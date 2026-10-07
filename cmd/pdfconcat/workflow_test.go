// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

// latvian is decomposed Latvian text: a with macron, l with cedilla (base letter plus combining mark).
const latvian = "Pa\u0304rbaude l\u0327aut"

func TestCheckBuildAndQueryWorkflow(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 2, "a", "b")
	writeFile(t, dir, fileJob, planJSON(t, obj{
		keyVersion: 1,
		keyOutput:  fileOut,
		keyBlank:   obj{"background": "#eeeeee", keyText: obj{keyValue: dividerText}},
		keyItems: []any{
			fileA,
			obj{keyBlank: obj{}, "count": 2},
			fileB,
			obj{keyBlank: obj{keyText: obj{keyValue: latvian}}},
		},
	}))

	requireCheckKeepsNoOutput(t, dir)

	built := run(t, dir, "", commandBuild, flagPlan, fileJob, flagReport, buildReportPath)
	requireExit(t, built, 0)

	parsed := summaryOf(t, built)
	if parsed.Status != "ok" || !parsed.Publication.Published || parsed.Publication.ReportStatus != reportWritten {
		t.Fatalf("build summary: %+v", parsed)
	}

	if parsed.Phases["output_verification"] != "complete" {
		t.Fatalf("output verification: %v", parsed.Phases)
	}

	verifyPages(
		t,
		filepath.Join(dir, fileOut),
		sourceAMarker,
		"a p2",
		dividerText,
		dividerText,
		sourceBFirstMarker,
		sourceBSecondMarker,
		latvian,
	)
	requireNoScratch(t, dir)

	queryBuildReport(t, dir)
	requireOracleNoticesSwappedPages(t, filepath.Join(dir, fileOut))
}

// requireCheckKeepsNoOutput checks the workflow's plan and saves the report; a check publishes nothing.
func requireCheckKeepsNoOutput(t *testing.T, dir string) {
	t.Helper()

	checked := run(t, dir, "", commandCheck, flagPlan, fileJob, flagReport, "job.report.json")
	requireExit(t, checked, 0)

	parsed := summaryOf(t, checked)
	if parsed.Status != "ok" || parsed.Command != commandCheck || parsed.Kind != "summary" {
		t.Fatalf("check summary: %+v", parsed)
	}

	if *parsed.Counts.Source != 4 || *parsed.Counts.Generated != 3 || *parsed.Counts.Total != 7 || parsed.PartCount != 4 {
		t.Errorf("check counts: %+v parts %d", parsed.Counts, parsed.PartCount)
	}

	if parsed.Publication.ReportStatus != reportWritten || parsed.Publication.Published {
		t.Errorf("check publication: %+v", parsed.Publication)
	}

	requireAbsent(t, filepath.Join(dir, fileOut))
	requireNoScratch(t, checked.tmp)
}

// queryBuildReport asks targeted questions of the saved build report, without reopening any PDF.
func queryBuildReport(t *testing.T, dir string) {
	t.Helper()

	page := generic(t, run(t, dir, "", commandReport, buildReportPath, flagPage, "4").stdout)
	if page["kind"] != "page" || page["page_in_part"] != float64(2) {
		t.Errorf("page 4: %v", page)
	}

	queryInheritedPart(t, dir)

	view := generic(t, run(t, dir, "", commandReport, buildReportPath, flagView, viewParts, offsetFlag, "1", limitFlag, "2").stdout)
	if view["total"] != float64(4) || view["returned"] != float64(2) || view["next_offset"] != float64(3) {
		t.Errorf("parts view: %v", view)
	}

	again := run(t, dir, "", commandReport, buildReportPath)
	requireExit(t, again, 0)

	if saved := summaryOf(t, again); saved.Status != "ok" || saved.Command != commandBuild {
		t.Errorf("saved summary: %+v", saved)
	}
}

// queryInheritedPart reads the generated part that inherited its page size from the preceding source.
func queryInheritedPart(t *testing.T, dir string) {
	t.Helper()

	part := generic(t, run(t, dir, "", commandReport, buildReportPath, partFlag, "/items/3", flagDetails).stdout)
	style := objAt(t, part, partField, keyStyle)
	text := objAt(t, style, keyText)

	if text[keyValue] != latvian || style["background"] != "#eeeeee" || textAt(t, text, keyFont, "name") != "Noto Sans" {
		t.Errorf("part /items/3: %v", part)
	}

	if size := objAt(t, style, keySize); size["origin"] != "preceding_source" || size[keyWidth] != float64(612) {
		t.Errorf("inherited size: %v", size)
	}
}

// requireOracleNoticesSwappedPages is a negative control: the independent oracle must notice a wrong expectation.
func requireOracleNoticesSwappedPages(t *testing.T, path string) {
	t.Helper()

	tools := pdforacle.RequireTools(t)

	doc, err := pdforacle.Load(tools, path)
	ensure(t, err)

	wrong := pdforacle.Expectation{Sources: map[string]pdforacle.SourceFact{}}

	markers := []string{sourceBFirstMarker, sourceAMarker, dividerText, dividerText, sourceBFirstMarker, sourceBSecondMarker, latvian}
	for _, text := range markers {
		wrong.Pages = append(wrong.Pages, pdforacle.ExpectedPage{Text: text})
	}

	if findings := doc.Verify(wrong); len(findings) == 0 {
		t.Error("the oracle accepted a swapped page order")
	}
}

func TestInputTransportsAreEquivalent(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 2, "a", "b")

	plan := planJSON(t, itemsOf(fileA, obj{keyBlank: obj{}}, fileB))
	writeFile(t, dir, fileJob, plan)

	variants := map[string][]string{
		transportFile:  {commandCheck, flagPlan, fileJob, flagDetails},
		transportStdin: {commandCheck, flagPlan, "-", flagBaseDir, dir, flagDetails},
		"inline":       {commandCheck, inlinePlanFlag, plan, flagBaseDir, dir, flagDetails},
		"operands":     {commandCheck, flagDetails, fileA, flagBlank, fileB},
	}

	var want map[string]any

	for name, args := range variants {
		stdin := ""
		if name == transportStdin {
			stdin = plan
		}

		res := run(t, dir, stdin, args...)
		requireExit(t, res, 0)

		got := resolvedSemantics(t, generic(t, res.stdout))
		if want == nil {
			want = got

			continue
		}

		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s resolves differently:\n got %v\nwant %v", name, got, want)
		}
	}

	// The same instructions give the same output pages through every transport.
	outputs := map[string][]string{
		transportFile:  {commandBuild, flagPlan, fileJob, "-o", "file.pdf"},
		transportStdin: {commandBuild, flagPlan, "-", flagBaseDir, dir, "-o", stdinOutputPath},
		"inline":       {commandBuild, inlinePlanFlag, plan, flagBaseDir, dir, "-o", "inline.pdf"},
		"operands":     {commandBuild, "-o", "operands.pdf", fileA, flagBlank, fileB},
	}

	for name, args := range outputs {
		stdin := ""
		if name == transportStdin {
			stdin = plan
		}

		requireExit(t, run(t, dir, stdin, args...), 0)
		verifyPages(t, filepath.Join(dir, name+".pdf"), sourceAMarker, "a p2", "", sourceBFirstMarker, sourceBSecondMarker)
	}
}

// resolvedSemantics keeps what a job means (sources, parts without their origins, styles, fonts) and
// drops where it was written.
func resolvedSemantics(tb testing.TB, report map[string]any) map[string]any {
	tb.Helper()

	parts := listAt(tb, report, keyParts)
	for _, raw := range parts {
		part := objAt(tb, raw)
		delete(part, "id")
		delete(part, "origin")
	}

	return obj{
		"counts": report["counts"], keyParts: parts, "sources": report["sources"],
		fontsView: report[fontsView], stylesView: report[stylesView],
	}
}

func TestDirectiveShapedFilenamesAreLiteralPaths(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	for _, name := range []string{flagBlank, "-o", blankOperand, flagPlan} {
		writePDFsNamed(t, dir, name)
	}

	res := run(t, dir, "", commandBuild, "-o", fileOut, fileA, "--", flagBlank, "-o", blankOperand, flagPlan)
	requireExit(t, res, 0)

	verifyPages(t, filepath.Join(dir, fileOut), sourceAMarker, "name --blank", "name -o", "name @blank", "name --plan")

	// In a plan a bare string is always a path, whatever it looks like.
	writeFile(t, dir, fileJob, planJSON(t, itemsOf(flagBlank, fileA, blankOperand)))
	requireExit(t, run(t, dir, "", commandBuild, flagPlan, fileJob, "-o", "plan.pdf"), 0)
	verifyPages(t, filepath.Join(dir, "plan.pdf"), "name --blank", sourceAMarker, "name @blank")

	// Before the separator the same word is the directive: a blank page, which cannot inherit a size here.
	lonely := run(t, dir, "", commandCheck, flagBlank)
	requireExit(t, lonely, 2)

	parsed := summaryOf(t, lonely)
	requireCode(t, &parsed, "size_unresolved")
}

// writePDFsNamed writes a one-page PDF with exactly the given file name, marked "name <file name>".
func writePDFsNamed(tb testing.TB, dir, name string) {
	tb.Helper()

	writePDFsAt(tb, filepath.Join(dir, name), "name "+name)
}

func TestUnicodeAndSpacedPathsAndText(t *testing.T) {
	t.Parallel()

	root := tempDir(t)
	dir := filepath.Join(root, latvianText, "ā ļ b")
	ensure(t, os.MkdirAll(dir, 0o700))

	writePDFsAt(t, filepath.Join(dir, "Pielikums ā.pdf"), "Pielikums")
	writePDFsAt(t, filepath.Join(dir, "Άλφα Βήτα.pdf"), "Alpha")

	plan := planJSON(t, obj{
		keyVersion: 1,
		keyOutput:  "Rezultāts ē.pdf",
		keyDir:     "ā ļ b",
		keyBlank: obj{
			keyText: obj{keyValue: "Precomposed: \u0101 \u013c\nDecomposed: " + latvian + "\nΕλληνικά Привет"},
		},
		keyItems: []any{"Pielikums ā.pdf", obj{keyBlank: obj{}}, "Άλφα Βήτα.pdf"},
	})
	writeFile(t, filepath.Join(root, latvianText), "plāns.json", plan)

	res := run(t, filepath.Join(root, latvianText), "", commandBuild, flagPlan, "plāns.json")
	requireExit(t, res, 0)

	out := filepath.Join(root, latvianText, "Rezultāts ē.pdf")
	texts := pageTexts(t, out)

	if len(texts) != 3 || texts[0] != "Pielikums" || texts[2] != "Alpha" {
		t.Fatalf("pages: %q", texts)
	}

	if texts[1] != "Precomposed: \u0101 \u013c" {
		t.Errorf("precomposed line: %q", texts[1])
	}

	tools := pdforacle.RequireTools(t)

	text := runTool(t, tools.PDFToText, "-layout", out, "-")

	for _, want := range []string{"Decomposed: " + latvian, "Ελληνικά Привет"} {
		if !strings.Contains(text, want) {
			t.Errorf("extracted text lacks %q:\n%s", want, text)
		}
	}
}

func TestLeadingBlankInheritsTheFirstSourcePage(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	res := run(t, dir, "", commandCheck, flagDetails, flagBlank, fileA, flagBlank)
	requireExit(t, res, 0)

	origins := map[string]bool{}

	for _, raw := range listAt(t, generic(t, res.stdout), stylesView) {
		origins[textAt(t, raw, keySize, "origin")] = true
	}

	if !origins["following_source"] || !origins["preceding_source"] || len(origins) != 2 {
		t.Errorf("size origins: %v", origins)
	}
}
