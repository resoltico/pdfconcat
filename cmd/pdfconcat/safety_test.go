// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

// aliasCase is a command line that names one file in two roles, and the files it must leave unmodified.
type aliasCase struct {
	name string
	args []string
	keep []string
}

// fontPath is the repository's embedded font, used as a supplied font file in tests.
const fontPath = "../../internal/typeset/fontdata/NotoSans-Regular.ttf"

// aliasCases are the role conflicts the fixture in TestAliasedFilesAreRejected can express.
func aliasCases() []aliasCase {
	return []aliasCase{
		{"output is a source", []string{commandBuild, "-o", fileA, flagOverwrite, fileA, fileB}, []string{fileA}},
		{"output is a font", []string{commandBuild, flagPlan, fontPlanPath, "-o", fileFont, flagOverwrite}, []string{fileFont}},
		{"report is the plan", []string{commandCheck, flagPlan, fileJob, flagReport, fileJob, flagOverwrite}, []string{fileJob}},
		{"report is a source", []string{commandCheck, flagPlan, fileJob, flagReport, fileB, flagOverwrite}, []string{fileB}},
		{"report is the output", []string{commandBuild, flagPlan, fileJob, "-o", newOutputPath, flagReport, newOutputPath}, nil},
		{"report is a font", []string{commandCheck, flagPlan, fontPlanPath, flagReport, fileFont, flagOverwrite}, []string{fileFont}},
	}
}

// hardLinkCase is the conflict of an output that is another name for a source; it needs a file system with hard links.
func hardLinkCase() aliasCase {
	return aliasCase{
		"output is a hard link of a source",
		[]string{commandBuild, "-o", hardlinkPath, flagOverwrite, fileA, fileB},
		[]string{fileA, hardlinkPath},
	}
}

func TestAliasedFilesAreRejected(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b")

	ensure(t, os.WriteFile(filepath.Join(dir, fileFont), readFile(t, fontPath), 0o600))
	writeFile(t, dir, fontPlanPath, planJSON(t, obj{
		keyVersion: 1, keyBlank: obj{keyText: obj{keyValue: "x", keyFont: obj{keyFile: fileFont}}},
		keyItems: []any{fileA, obj{keyBlank: obj{}}},
	}))
	writeFile(t, dir, fileJob, planJSON(t, itemsOf(fileA, fileB)))

	cases := aliasCases()

	ensure(t, os.Link(filepath.Join(dir, fileA), filepath.Join(dir, hardlinkPath)))

	cases = append(cases, hardLinkCase())

	for _, test := range cases {
		before := map[string][]byte{}
		for _, name := range test.keep {
			before[name] = readFile(t, filepath.Join(dir, name))
		}

		res := run(t, dir, "", test.args...)
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		requireCode(t, &parsed, codeAliasConflict)

		if parsed.Publication.Published {
			t.Errorf("%s: reported as published", test.name)
		}

		for name, content := range before {
			if !bytes.Equal(readFile(t, filepath.Join(dir, name)), content) {
				t.Errorf("%s: %s was modified", test.name, name)
			}
		}
	}

	requireAbsent(t, filepath.Join(dir, newOutputPath))
	requireNoScratch(t, dir)
}

func TestSymbolicLinks(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b")

	err := os.Symlink(filepath.Join(dir, fileA), filepath.Join(dir, aliasPath))
	if err != nil {
		t.Fatalf("required symbolic-link capability unavailable: %v", err)
	}

	// A source may be a symbolic link to a regular file; the link is followed.
	linkedSource := run(t, dir, "", commandBuild, "-o", fileOut, aliasPath, fileB)
	requireExit(t, linkedSource, 0)
	verifyPages(t, filepath.Join(dir, fileOut), sourceAMarker, sourceBFirstMarker)

	// A named output may not be a symbolic link, even with --overwrite, and its target is untouched.
	before := readFile(t, filepath.Join(dir, fileA))

	res := run(t, dir, "", commandBuild, "-o", aliasPath, flagOverwrite, fileB)
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, outputInvalidCode)

	if !bytes.Equal(readFile(t, filepath.Join(dir, fileA)), before) {
		t.Error("the target of the output symbolic link was modified")
	}

	// The same rule holds for a dangling link and for a link as the report target.
	ensure(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.pdf")))
	requireExit(t, run(t, dir, "", commandBuild, "-o", "dangling.pdf", flagOverwrite, fileB), 2)
	requireAbsent(t, filepath.Join(dir, "gone"))

	linkedReport := run(t, dir, "", commandCheck, flagReport, aliasPath, flagOverwrite, fileB)
	requireExit(t, linkedReport, 2)

	if !bytes.Equal(readFile(t, filepath.Join(dir, fileA)), before) {
		t.Error("the target of the report symbolic link was modified")
	}
}

func TestCaseAliasOfAnExistingSource(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b")

	_, err := os.Stat(filepath.Join(dir, upperSourceName))
	insensitive := err == nil

	res := run(t, dir, "", commandBuild, "-o", upperSourceName, flagOverwrite, fileA, fileB)

	if insensitive {
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		requireCode(t, &parsed, codeAliasConflict)
		verifyPages(t, filepath.Join(dir, fileA), sourceAMarker)

		return
	}

	requireExit(t, res, 0)
	verifyPages(t, filepath.Join(dir, upperSourceName), sourceAMarker, sourceBFirstMarker)
}

func TestExistingOutputNeedsOverwrite(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b")
	existing := writeFile(t, dir, fileOut, "precious\n")

	for _, command := range []string{commandBuild, commandCheck} {
		res := run(t, dir, "", command, "-o", fileOut, fileA, fileB)
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		requireCode(t, &parsed, outputInvalidCode)

		if string(readFile(t, existing)) != "precious\n" {
			t.Fatalf("%s replaced the existing output without --overwrite", command)
		}
	}

	requireExit(t, run(t, dir, "", commandBuild, "-o", fileOut, flagOverwrite, fileA, fileB), 0)
	verifyPages(t, existing, sourceAMarker, sourceBFirstMarker)
	requireNoScratch(t, dir)

	// An output directory that does not exist is an invalid instruction too.
	res := run(t, dir, "", commandBuild, "-o", "missing/out.pdf", fileA)
	requireExit(t, res, 2)

	// A directory is not a destination.
	ensure(t, os.Mkdir(filepath.Join(dir, fixtureFolder), 0o700))
	requireExit(t, run(t, dir, "", commandBuild, "-o", fixtureFolder, flagOverwrite, fileA), 2)
}

func TestExistingReportNeedsOverwrite(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	existingReport := writeFile(t, dir, shortReportPath, "keep\n")

	res := run(t, dir, "", commandBuild, "-o", fileOut, flagReport, shortReportPath, fileA)
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	requireCode(t, &parsed, "report_destination_invalid")

	// The check failed before any work, so the PDF was not published either.
	requireAbsent(t, filepath.Join(dir, fileOut))

	if string(readFile(t, existingReport)) != "keep\n" {
		t.Fatal("the existing report was replaced without --overwrite")
	}

	requireExit(t, run(t, dir, "", commandBuild, "-o", fileOut, flagReport, shortReportPath, flagOverwrite, fileA), 0)

	saved := generic(t, string(readFile(t, existingReport)))
	if saved["kind"] != kindReport || saved["status"] != "ok" {
		t.Errorf("saved report: %v", saved)
	}
}

func TestFailureReportNeverReplacesAnUnknownFile(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	malformed := writeFile(t, dir, fileBadPlan, `{"version":1,"items":["a.pdf",]}`)

	// A failure report goes to a new file.
	fresh := run(t, dir, "", commandCheck, flagPlan, fileBadPlan, flagReport, "fresh.json")
	requireExit(t, fresh, 2)

	saved := generic(t, string(readFile(t, filepath.Join(dir, "fresh.json"))))
	if saved["status"] != statusInvalid || saved["kind"] != kindReport {
		t.Errorf("failure report: %v", saved)
	}

	// An existing file is not replaced, with or without --overwrite, because the malformed job might
	// name it as an input. The primary diagnostic and exit code survive; the report failure is secondary.
	for _, flags := range [][]string{nil, {flagOverwrite}} {
		args := append([]string{commandCheck, flagPlan, fileBadPlan, flagReport, fileBadPlan}, flags...)
		res := run(t, dir, "", args...)
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		primary := parsed.Status == statusInvalid && parsed.Diagnostics[0].Code == codeJSONSyntax

		if !primary || parsed.Publication.ReportStatus != reportFailed || parsed.DiagnosticCount != 2 {
			t.Fatalf("%v: %+v", flags, parsed)
		}

		if got := parsed.Diagnostics[1]; got.Code != codeAliasConflict && got.Code != codeReportWriteBad {
			t.Errorf("secondary diagnostic: %+v", got)
		}

		if string(readFile(t, malformed)) != `{"version":1,"items":["a.pdf",]}` {
			t.Fatal("the plan file was replaced by a failure report")
		}
	}

	// An existing file named by the job as a source is registered as a different role, so even
	// after the plan decoded it is never replaced.
	notes := writeFile(t, dir, notesPath, "notes\n")
	writeFile(t, dir, "uses-notes.json", planJSON(t, itemsOf(fileA, notesPath)))

	res := run(t, dir, "", commandCheck, flagPlan, "uses-notes.json", flagReport, notesPath, flagOverwrite)
	requireExit(t, res, 2)

	if string(readFile(t, notes)) != "notes\n" {
		t.Fatal("a file the job reads was replaced by the failure report")
	}
}

func TestFailureReportMayOverwriteOnceInputsAreKnown(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	existing := writeFile(t, dir, shortReportPath, "old\n")

	overflowing := planJSON(t, itemsOf(fileA, obj{keyBlank: obj{keyText: obj{keyValue: unbreakableText, keyWidth: 20}}}))

	// Without --overwrite the existing report stays.
	res := run(t, dir, "", commandCheck, inlinePlanFlag, overflowing, flagReport, shortReportPath)
	requireExit(t, res, 2)

	if string(readFile(t, existing)) != "old\n" {
		t.Fatal("the report was replaced without --overwrite")
	}

	// With it, the failure report replaces the old file: every input was known.
	res = run(t, dir, "", commandCheck, inlinePlanFlag, overflowing, flagReport, shortReportPath, flagOverwrite)
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	if parsed.Publication.ReportStatus != reportWritten || parsed.Diagnostics[0].Code != textOverflowCode {
		t.Fatalf(summaryFailureFormat, parsed)
	}

	saved := generic(t, string(readFile(t, existing)))
	if saved["status"] != statusInvalid {
		t.Errorf("saved failure report: %v", saved)
	}
}

func TestExitCodesAndStableCodes(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	writeFile(t, dir, invalidPDFPath, "this is not a PDF")

	snake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	cases := []struct {
		name  string
		plan  string
		code  string
		phase string
		exit  int
	}{
		{"trailing data", `{"version":1,"items":["a.pdf"]} x`, "json_trailing_data", phaseInstructions, 2},
		{"unterminated", `{"version":1,"items":["a.pdf"]`, codeJSONSyntax, phaseInstructions, 2},
		{"duplicate member", `{"version":1,"version":1,"items":["a.pdf"]}`, "json_duplicate_member", phaseInstructions, 2},
		{"null", `{"version":1,"items":["a.pdf"],"blank":null}`, "plan_null_not_allowed", phaseInstructions, 2},
		{"wrong case", `{"Version":1,"items":["a.pdf"]}`, "plan_unknown_member", phaseInstructions, 2},
		{"unknown version", `{"version":2,"items":["a.pdf"]}`, "plan_unsupported_version", phaseInstructions, 2},
		{"empty items", `{"version":1,"items":[]}`, "plan_empty_items", phaseInstructions, 2},
		{"fractional count", `{"version":1,"items":[{"blank":{},"count":2.5}]}`, "plan_not_integer", phaseInstructions, 2},
		{"unpaired surrogate", `{"version":1,"items":["\ud800.pdf"]}`, codeJSONSyntax, phaseInstructions, 2},
		{"bad color", `{"version":1,"items":["a.pdf",{"blank":{"background":"red"}}]}`, "plan_bad_value", phaseInstructions, 2},
		{"inherit without source", `{"version":1,"items":[{"blank":{}}]}`, "size_unresolved", "layout", 2},
		{"missing source", `{"version":1,"items":["missing.pdf"]}`, "source_unreadable", "input_inspection", 1},
		{"malformed pdf", `{"version":1,"items":["garbage.pdf"]}`, "pdf_invalid", "input_inspection", 1},
	}

	for _, test := range cases {
		res := run(t, dir, "", commandCheck, inlinePlanFlag, test.plan)
		requireExit(t, res, test.exit)

		parsed := summaryOf(t, res)
		if parsed.Diagnostics[0].Code != test.code {
			t.Errorf("%s: code %q, want %q (%+v)", test.name, parsed.Diagnostics[0].Code, test.code, parsed.Diagnostics[0])
		}

		if parsed.Phases[test.phase] != phaseIncomplete {
			t.Errorf("%s: phase %s is %q, want incomplete", test.name, test.phase, parsed.Phases[test.phase])
		}

		for _, found := range parsed.Diagnostics {
			if !snake.MatchString(found.Code) || !snake.MatchString(found.Stage) {
				t.Errorf("%s: code %q or stage %q is not snake_case", test.name, found.Code, found.Stage)
			}
		}

		if parsed.Counts.Total != nil {
			t.Errorf("%s: total pages %d is claimed although layout is not complete", test.name, *parsed.Counts.Total)
		}
	}
}

func TestFailedRunsLeaveNothingBehind(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	writeFile(t, dir, invalidPDFPath, "this is not a PDF")

	for _, items := range [][]string{{fileA, invalidPDFPath}, {fileA, fileMissing}} {
		args := append([]string{commandBuild, "-o", fileOut}, items...)
		requireExit(t, run(t, dir, "", args...), 1)
	}

	overflow := planJSON(t, itemsOf(fileA, obj{keyBlank: obj{keyText: obj{keyValue: strings.Repeat("W", 40), keyWidth: 20}}}))
	requireExit(t, run(t, dir, "", commandBuild, "-o", fileOut, inlinePlanFlag, overflow), 2)

	requireAbsent(t, filepath.Join(dir, fileOut))
	requireNoScratch(t, dir)

	entries, err := os.ReadDir(dir)
	ensure(t, err)

	for _, entry := range entries {
		switch entry.Name() {
		case fileA, invalidPDFPath:
		default:
			t.Errorf("unexpected entry %s after failed runs", entry.Name())
		}
	}
}

func TestAggregatedFailuresKeepInputOrder(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	items := make([]any, 0, 80)

	for index := range 40 {
		items = append(items, fileA, sprintMissing(index))
	}

	res := run(t, dir, "", commandCheck, inlinePlanFlag, planJSON(t, itemsOf(items...)), jobsFlag, "4", flagReport, shortReportPath)
	requireExit(t, res, 1)

	parsed := summaryOf(t, res)

	incorrectCounts := parsed.DiagnosticCount != 40 || len(parsed.Diagnostics) > report.PreviewDiagnostics ||
		parsed.DiagnosticsOmitted != 40-len(parsed.Diagnostics)
	if incorrectCounts {
		t.Fatalf("summary shows %d of %d (%d omitted)", len(parsed.Diagnostics), parsed.DiagnosticCount, parsed.DiagnosticsOmitted)
	}

	view := generic(t, run(t, dir, "", commandReport, shortReportPath, flagView, diagnosticsView, limitFlag, "100").stdout)

	for index, raw := range listAt(t, view, "records") {
		pointer := textAt(t, raw, locationMember, pointerMember)
		if want := "/items/" + sprintInt(2*index+1); pointer != want {
			t.Fatalf("diagnostic %d at %v, want %s", index, pointer, want)
		}
	}
}

func TestUnusableInstructionFilesAndDestinations(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	writeFile(t, dir, fileJob, planJSON(t, itemsOf(fileA)))
	writeFile(t, dir, fileBadPlan, `{"version":1,"items":["a.pdf"],"unknown":1}`)
	ensure(t, os.Mkdir(filepath.Join(dir, fixtureFolder), 0o700))

	cases := []struct {
		name string
		code string
		args []string
		exit int
	}{
		{
			"plan file is missing", "plan_unreadable",
			[]string{commandCheck, flagPlan, "nothing.json"},
			1,
		},
		{
			"plan file is a directory", "",
			[]string{commandCheck, flagPlan, fixtureFolder},
			1,
		},
		{
			"output is the plan", codeAliasConflict,
			[]string{commandBuild, flagPlan, fileJob, "-o", fileJob, flagOverwrite},
			2,
		},
		{
			"report directory is missing", outputInvalidCode,
			[]string{commandCheck, flagPlan, fileJob, flagReport, "nowhere/r.json"},
			2,
		},
		{
			"failure report directory is missing", codeReportWriteBad,
			[]string{commandCheck, flagPlan, fileBadPlan, flagReport, "nowhere/r.json"},
			2,
		},
		{
			"report is a directory", "artifact_target_invalid",
			[]string{commandCheck, flagPlan, fileJob, flagReport, fixtureFolder, flagOverwrite},
			2,
		},
	}

	for _, test := range cases {
		res := run(t, dir, "", test.args...)
		requireExit(t, res, test.exit)

		parsed := summaryOf(t, res)
		if test.code != "" {
			requireCode(t, &parsed, test.code)
		}
	}

	if string(readFile(t, filepath.Join(dir, fileJob))) != planJSON(t, itemsOf(fileA)) {
		t.Error("the plan was modified")
	}
}

func TestRepeatedAppearanceProblemsLocateEachDistinctDeclaration(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	overflow := obj{keyBlank: obj{keyText: obj{keyValue: unbreakableText, keyWidth: 20}}}
	res := run(t, dir, "", commandCheck, inlinePlanFlag, planJSON(t, itemsOf(fileA, overflow, overflow, fileA, overflow)), flagDetails)
	requireExit(t, res, 2)

	diagnostics := listAt(t, generic(t, res.stdout), diagnosticsView)
	if len(diagnostics) != 3 {
		t.Fatalf("distinct bad declarations must remain repairable: %v", diagnostics)
	}

	for index, item := range []int{1, 2, 4} {
		pointer := fmt.Sprintf("/items/%d/blank/text/width", item)
		if textAt(t, diagnostics[index], locationMember, pointerMember) != pointer {
			t.Fatalf("bad declaration %s lost: %v", pointer, diagnostics[index])
		}

		consumers := listAt(t, diagnostics[index], consumersMember)
		if len(consumers) != 1 || consumers[0] != fmt.Sprintf(itemIDFormat, item) {
			t.Fatalf("affected consumer lost: %v", consumers)
		}
	}
}
