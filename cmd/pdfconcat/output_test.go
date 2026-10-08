// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestTextFormatIsAHumanRenderingOfTheSameResult(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 2, "a")

	for _, args := range [][]string{
		{commandCheck, flagFormat, formatText, fileA, flagBlank},
		{commandCheck, flagFormat, formatText, flagDetails, fileA, flagBlank},
		{commandCheck, flagFormat, formatText, flagReport, shortReportPath, fileA},
		{commandVersion, flagFormat, formatText},
		{commandHelp, flagFormat, formatText},
		{commandHelp, commandBuild, flagFormat, formatText},
		{commandCheck, flagFormat, formatText, fileMissing},
		{commandCheck, flagFormat, formatText},
	} {
		res := run(t, dir, "", args...)

		human := res.stdout
		if args[0] == commandReport {
			_, human, _ = strings.Cut(human, "\n")
		}

		if (strings.HasPrefix(human, "{") != slices.Contains(args, flagDetails)) || res.stdout == "" {
			t.Errorf("%v: not text: %.200q", args, res.stdout)
		}
	}

	checked := run(t, dir, "", commandCheck, flagFormat, formatText, fileA, flagBlank)
	for _, want := range []string{"check ok", "pages: 2 source, 1 generated, 3 total", "summary only: 2 parts, 0 diagnostics"} {
		if !strings.Contains(checked.stdout, want) {
			t.Errorf("text summary lacks %q:\n%s", want, checked.stdout)
		}
	}

	requireExit(t, run(t, dir, "", commandCheck, flagReport, fileSavedReport, fileA, flagBlank), 0)

	for _, args := range [][]string{
		{commandReport, fileSavedReport, flagFormat, formatText},
		{commandReport, fileSavedReport, flagFormat, formatText, flagView, viewParts, flagDetails},
		{commandReport, fileSavedReport, flagFormat, formatText, flagPage, "3"},
		{commandReport, fileSavedReport, flagFormat, formatText, partFlag, directBlankPart, flagDetails},
		{commandReport, fileSavedReport, flagFormat, formatText, flagView, viewParts},
		{commandReport, fileSavedReport, flagFormat, formatText, flagView, diagnosticsView},
	} {
		res := run(t, dir, "", args...)
		requireExit(t, res, 0)

		human := res.stdout
		if args[0] == commandReport {
			_, human, _ = strings.Cut(human, "\n")
		}

		if (strings.HasPrefix(human, "{") != slices.Contains(args, flagDetails)) || res.stdout == "" {
			t.Errorf("%v: %.200q", args, res.stdout)
		}
	}
}

func TestDetailsReturnsTheCompleteReport(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	res := run(t, dir, "", commandCheck, flagDetails, fileA, flagBlank)
	requireExit(t, res, 0)

	complete := generic(t, res.stdout)
	if complete["kind"] != kindReport || len(listAt(t, complete, keyParts)) != 2 || len(listAt(t, complete, stylesView)) != 1 {
		t.Errorf("--details: %v", complete)
	}
}

func TestOutputPathsResolveAgainstTheirOwnBase(t *testing.T) {
	t.Parallel()

	root := tempDir(t)
	plans := filepath.Join(root, plansDirectory)
	ensure(t, os.MkdirAll(plans, 0o700))
	writePDFs(t, plans, 1, "a")
	writeFile(t, plans, fileJob, planJSON(t, obj{keyVersion: 1, keyOutput: planOutputPath, keyItems: []any{fileA}}))

	// A plan's output is relative to the plan's directory, not to the working directory.
	requireExit(t, run(t, root, "", commandBuild, flagPlan, "plans/job.json"), 0)
	verifyPages(t, filepath.Join(plans, planOutputPath), sourceAMarker)
	requireAbsent(t, filepath.Join(root, planOutputPath))

	// -o is relative to the working directory, and wins over the plan.
	requireExit(t, run(t, root, "", commandBuild, flagPlan, "plans/job.json", "-o", "override.pdf"), 0)
	verifyPages(t, filepath.Join(root, "override.pdf"), sourceAMarker)

	// A plan on standard input takes its base from --base-dir; -o still means the working directory.
	stdin := planJSON(t, obj{keyVersion: 1, keyOutput: stdinOutputPath, keyItems: []any{fileA}})
	requireExit(t, run(t, root, stdin, commandBuild, flagPlan, "-", flagBaseDir, plansDirectory), 0)
	verifyPages(t, filepath.Join(plans, stdinOutputPath), sourceAMarker)

	requireExit(t, run(t, root, stdin, commandBuild, flagPlan, "-", flagBaseDir, plansDirectory, "-o", "cwd.pdf"), 0)
	verifyPages(t, filepath.Join(root, "cwd.pdf"), sourceAMarker)

	// Without --base-dir the working directory is the base, so the source is not found.
	requireExit(t, run(t, root, stdin, commandCheck, flagPlan, "-"), 1)
}

func TestBaseDirectoryAndOutputMustBeUsable(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	plan := planJSON(t, itemsOf(fileA))

	for _, base := range []string{"nowhere", fileA} {
		res := run(t, dir, plan, commandCheck, flagPlan, "-", flagBaseDir, base)
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		requireCode(t, &parsed, "base_dir_invalid")
	}

	res := run(t, dir, "", commandBuild, fileA)
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, "output_missing")

	// A check needs no output, and creates nothing.
	requireExit(t, run(t, dir, "", commandCheck, fileA), 0)

	entries, err := os.ReadDir(dir)
	ensure(t, err)

	if len(entries) != 1 {
		t.Errorf("a check left %d entries in the directory", len(entries))
	}

	// A plan's own output path is resolved lexically and may be rejected with its location.
	bad := run(t, dir, "", commandCheck, inlinePlanFlag, `{"version":1,"output":"\u0000","items":["a.pdf"]}`)
	requireExit(t, bad, 2)
}

func TestGeneratedOnlyJobsAndInheritedSizes(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	// A job that is all generated pages needs sizes it can resolve, and does not need a dummy source.
	plan := planJSON(t, itemsOf(
		obj{keyBlank: obj{keySize: "200x100", keyText: obj{keyValue: "first", keySize: 10}}},
		obj{keyBlank: obj{keySize: "A4", keyBackground: "#ff0000"}},
	))
	requireExit(t, run(t, dir, "", commandBuild, inlinePlanFlag, plan, "-o", "generated.pdf"), 0)

	tools := pdforacle.RequireTools(t)
	doc, err := pdforacle.Load(tools, filepath.Join(dir, "generated.pdf"))
	ensure(t, err)

	findings := doc.Verify(pdforacle.Expectation{
		Sources: map[string]pdforacle.SourceFact{},
		Pages: []pdforacle.ExpectedPage{
			{Text: "first", Geometry: &pdforacle.Geometry{MediaBox: []float64{0, 0, 200, 100}}},
			{Text: "", Geometry: &pdforacle.Geometry{MediaBox: []float64{0, 0, 595.276, 841.89}}},
		},
	})

	if len(findings) > 0 {
		t.Errorf("generated-only output: %v", findings)
	}

	// The red page really is red, by an independent rasterizer.
	pixel, err := doc.PixelColor(2, 10, 10)
	ensure(t, err)

	if pixel[0] < 240 || pixel[1] > 15 || pixel[2] > 15 {
		t.Errorf("pixel is %v, want red", pixel)
	}

	// An inherited size with no source page is refused with the repair.
	lonely := run(t, dir, "", commandCheck, inlinePlanFlag, planJSON(t, itemsOf(obj{keyBlank: obj{}})))
	requireExit(t, lonely, 2)

	parsed := summaryOf(t, lonely)
	if !strings.Contains(parsed.Diagnostics[0].Message, "explicit size") {
		t.Errorf("the message does not say how to repair it: %s", parsed.Diagnostics[0].Message)
	}
}

func TestSourcesThatAreNotRegularFilesAreRejected(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	ensure(t, os.Mkdir(filepath.Join(dir, "folder.pdf"), 0o700))

	res := run(t, dir, "", commandCheck, fileA, "folder.pdf")
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, "source_not_regular")
}

func TestJobsOptionNeverChangesTheResult(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b", "c")

	var first string

	for _, jobs := range []string{"1", "2", "16"} {
		res := run(t, dir, "", commandCheck, flagDetails, jobsFlag, jobs, fileA, flagBlank, fileB, "c.pdf", fileA)
		requireExit(t, res, 0)

		resolved, err := json.Marshal(resolvedSemantics(t, generic(t, res.stdout)))
		ensure(t, err)

		if first == "" {
			first = string(resolved)
		} else if first != string(resolved) {
			t.Errorf("--jobs %s changed the result", jobs)
		}
	}

	requireExit(t, run(t, dir, "", commandCheck, jobsFlag, "0", fileA), 2)
}

func TestSavedReportsAreUntrustedAndQueriesFailCleanly(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	requireExit(t, run(t, dir, "", commandCheck, flagReport, successfulReportPath, fileA, flagBlank), 0)
	requireExit(t, run(t, dir, "", commandCheck, flagReport, failedReportPath, fileA, flagBlank, fileMissing), 1)

	saved := string(readFile(t, filepath.Join(dir, successfulReportPath)))
	writeFile(t, dir, "oldversion.json", strings.Replace(saved, `"format_version":2`, `"format_version":1`, 1))
	writeFile(t, dir, "trailing.json", saved+"x")
	writeFile(t, dir, "empty.json", "")
	writeFile(t, dir, "unknown.json", strings.Replace(saved, `"kind":"report"`, `"kind":"report","extra":1`, 1))
	writeFile(t, dir, "dangling.json", strings.Replace(saved, `"source":0`, `"source":9`, 1))

	cases := []struct {
		name string
		code string
		args []string
		exit int
	}{
		{"missing file", "report_read_failed", []string{commandReport, "absent.json"}, 1},
		{"a directory", "", []string{commandReport, "."}, 1},
		{"unsupported version", "report_unsupported_version", []string{commandReport, "oldversion.json"}, 2},
		{"trailing data", "report_json_trailing_data", []string{commandReport, "trailing.json"}, 2},
		{"empty", "report_json_empty", []string{commandReport, "empty.json"}, 2},
		{"unknown member", "report_unknown_member", []string{commandReport, "unknown.json"}, 2},
		{"dangling reference", "report_dangling_reference", []string{commandReport, "dangling.json"}, 2},
		{"unknown part", "report_part_not_found", []string{commandReport, successfulReportPath, partFlag, "/items/99"}, 2},
		{"page past the end", "report_page_out_of_range", []string{commandReport, successfulReportPath, flagPage, "99"}, 2},
		{"page of an incomplete layout", "report_layout_incomplete", []string{commandReport, failedReportPath, flagPage, "1"}, 2},
	}

	for _, test := range cases {
		res := run(t, dir, "", test.args...)
		requireExit(t, res, test.exit)

		parsed := summaryOf(t, res)
		if test.code != "" {
			requireCode(t, &parsed, test.code)
		}

		if parsed.Command != commandReport || parsed.Status == "ok" {
			t.Errorf(namedDiagnosticFormat, test.name, parsed)
		}
	}

	// Querying never modifies the saved report.
	if string(readFile(t, filepath.Join(dir, successfulReportPath))) != saved {
		t.Error("a query changed the saved report")
	}

	// An offset at or past the end is an empty page that cannot loop.
	view := generic(t, run(t, dir, "", commandReport, successfulReportPath, flagView, viewParts, offsetFlag, "5").stdout)
	if view["returned"] != float64(0) || view["next_offset"] != nil {
		t.Errorf("view past the end: %v", view)
	}

	// The partial report still answers part queries, by origin.
	part := generic(t, run(t, dir, "", commandReport, failedReportPath, partFlag, directBlankPart).stdout)
	if inner := objAt(t, part, partField); inner["range"] != nil || inner["path"] == nil {
		t.Errorf("part of an incomplete layout: %v", part)
	}
}

func TestSchemasAndHelpAreStructured(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, name := range []string{planSchemaName, commandReport} {
		res := run(t, dir, "", commandSchema, name)
		requireExit(t, res, 0)

		var schema obj

		ensure(t, json.Unmarshal([]byte(res.stdout), &schema))

		if schema["$schema"] == nil {
			t.Errorf("schema %s: %.100s", name, res.stdout)
		}
	}

	root := run(t, dir, "", commandHelp)
	requireExit(t, root, 0)

	help := generic(t, root.stdout)
	if help["kind"] != kindHelp || len(root.stdout) > 3000 {
		t.Errorf("root help is %d bytes: %v", len(root.stdout), help["kind"])
	}

	version := generic(t, run(t, dir, "", commandVersion).stdout)
	for _, key := range []string{keyVersion, "commit", "date", "go", "os", "arch"} {
		if _, found := version[key]; !found || version[key] == "" {
			t.Errorf("version lacks %s: %v", key, version)
		}
	}
}
