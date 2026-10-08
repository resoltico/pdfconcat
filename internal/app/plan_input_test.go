// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

// Words of plans that several tests in this package write.
const (
	planFile       = "plan.json"
	planFlag       = "--plan"
	outputFlag     = "-o"
	outputFile     = "out.pdf"
	overwriteFlag  = "--overwrite"
	reportFlag     = "--report"
	sourceA        = "a.pdf"
	sourceB        = "b.pdf"
	statusOK       = "ok"
	failedState    = "failed"
	codeAlias      = "alias_conflict"
	codeOutputBad  = "output_destination_invalid"
	privateFileBit = 0o600

	// The members of plans, and what the tests expect to find in reports.
	keyItems    = "items"
	keyBlank    = "blank"
	keyText     = "text"
	keyValue    = "value"
	keyFont     = "font"
	keyFile     = "file"
	stateNotRun = "not_run"
	textHello   = "Hello"
	codeInvalid = "font_invalid"
	someoneElse = "someone else\n"
)

// planJSON is a version 1 plan with the given members, as JSON.
func planJSON(tb testing.TB, members map[string]any) string {
	tb.Helper()

	members["version"] = 1

	data, err := json.Marshal(members)
	if err != nil {
		tb.Fatal(err)
	}

	return string(data)
}

// writeFile creates path with content.
func writeFile(tb testing.TB, path, content string) {
	tb.Helper()

	err := os.WriteFile(path, []byte(content), privateFileBit)
	if err != nil {
		tb.Fatal(err)
	}
}

// requirePages checks, with qpdf and Poppler, which share no code with the program, that the pages of path
// begin with the given lines.
func requirePages(tb testing.TB, path string, want ...string) {
	tb.Helper()

	tools := pdforacle.RequireTools(tb)

	doc, err := pdforacle.Load(tools, path)
	if err != nil {
		tb.Fatalf("load %s: %v", path, err)
	}

	expectation := pdforacle.Expectation{Sources: map[string]pdforacle.SourceFact{}}
	for _, text := range want {
		expectation.Pages = append(expectation.Pages, pdforacle.ExpectedPage{Text: text})
	}

	findings := doc.Verify(expectation)
	if len(findings) > 0 {
		tb.Fatalf("independent verification of %s failed: %v", path, findings)
	}
}

func TestAPlanFileDrivesABuildAndNamesItsOutput(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writePDF(t, dir, sourceB)
	writeFile(t, filepath.Join(dir, planFile), planJSON(t, map[string]any{keyItems: []any{sourceB, sourceA}, "output": planOutputPath}))

	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandBuild, planFlag, planFile)
	parsed := res.requireCode(t, 0, "")

	if parsed.Publication.Output != filepath.Join(dir, planOutputPath) || !parsed.Publication.Published {
		t.Errorf(publicationFailureFormat, parsed.Publication)
	}

	requirePages(t, filepath.Join(dir, planOutputPath), "b p1", sourceAMarker)
}

func TestTheOutputOptionOverridesTheOutputOfThePlan(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writeFile(t, filepath.Join(dir, planFile), planJSON(t, map[string]any{keyItems: []any{sourceA}, "output": planOutputPath}))

	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandBuild, planFlag, planFile, outputFlag, outputFile)
	res.requireCode(t, 0, "")

	requirePages(t, filepath.Join(dir, outputFile), sourceAMarker)
	requireMissing(t, filepath.Join(dir, planOutputPath))
}

func TestAPlanFileThatCannotBeUsedIsReported(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writeFile(t, filepath.Join(dir, "broken.json"), `{"version":`)

	runner := appOf(newFake(t))

	missing := execute(t.Context(), t, runner, dir, commandCheck, planFlag, missingPlanPath)
	parsed := missing.requireCode(t, 1, "plan_unreadable")

	if parsed.Diagnostics[0].Path != filepath.Join(dir, missingPlanPath) {
		t.Errorf("the diagnostic names %q", parsed.Diagnostics[0].Path)
	}

	broken := execute(t.Context(), t, runner, dir, commandCheck, planFlag, "broken.json")
	broken.requireCode(t, 2, "")

	// A plan that is also the destination is a file used in two roles.
	writeFile(t, filepath.Join(dir, planFile), planJSON(t, map[string]any{keyItems: []any{sourceA}}))

	alias := execute(t.Context(), t, runner, dir, commandBuild, planFlag, planFile, outputFlag, planFile, overwriteFlag)
	alias.requireCode(t, 2, codeAlias)
}

func TestTheBaseDirectoryOfAnInlinePlanMustBeADirectory(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writeFile(t, filepath.Join(dir, plainMessage), "")

	err := os.Mkdir(filepath.Join(dir, sourcesView), 0o700)
	if err != nil {
		t.Fatal(err)
	}

	writePDF(t, filepath.Join(dir, sourcesView), sourceA)

	inline := planJSON(t, map[string]any{keyItems: []any{sourceA}})
	runner := appOf(newFake(t))

	// Relative paths resolve against the base directory, not the working directory.
	ok := execute(t.Context(), t, runner, dir, commandCheck, inlinePlanFlag, inline, baseDirectoryFlag, sourcesView)
	parsed := ok.requireCode(t, 0, "")

	if parsed.Status != statusOK {
		t.Errorf(summaryFailureFormat, parsed)
	}

	for name, base := range map[string]string{"a file": plainMessage, "nothing": "absent"} {
		res := execute(t.Context(), t, runner, dir, commandCheck, inlinePlanFlag, inline, baseDirectoryFlag, base)
		found := res.requireCode(t, 2, "base_dir_invalid")

		message := found.Diagnostics[0].Message
		wrongPath := found.Diagnostics[0].Path != filepath.Join(dir, base)

		if !strings.HasPrefix(message, "--base-dir must name an existing directory: ") || wrongPath {
			t.Errorf(namedFailureFormat, name, found.Diagnostics[0])
		}
	}

	// The reason for a file is that it is not a directory.
	res := execute(t.Context(), t, runner, dir, commandCheck, inlinePlanFlag, inline, baseDirectoryFlag, plainMessage)
	if message := res.summary(t).Diagnostics[0].Message; !strings.HasSuffix(message, ": not a directory") {
		t.Errorf("message %q", message)
	}
}

func TestADestinationInAMissingDirectoryIsAnInvalidDestination(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))

	res := execute(t.Context(), t, runner, dir, commandCheck, reportFlag, "nowhere/r.json", sourceA)
	res.requireCode(t, 2, codeOutputBad)

	res = execute(t.Context(), t, runner, dir, commandBuild, outputFlag, "nowhere/out.pdf", sourceA)
	res.requireCode(t, 2, codeOutputBad)
}
