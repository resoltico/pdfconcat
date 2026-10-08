// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package main_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLongContinuationCanBeReconstructedWithoutTruncatedPaths(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	// Quotes cost two encoded bytes but one filesystem byte, staying within native path limits.
	longDir := dir
	for range 4 {
		longDir = filepath.Join(longDir, strings.Repeat("\"", 150))
	}

	ensure(t, os.MkdirAll(longDir, 0o700))
	executable := filepath.Join(longDir, "local tool")
	source := binary(t)
	ensure(t, os.Link(source, executable))

	reportPath := filepath.Join(longDir, "report.json")
	args := []string{executable, commandCheck, inlinePlanFlag, `{"version":1,"items":[{"blank":{"size":"A4"}}]}`, flagReport, reportPath}
	checked := executeContinuation(t, dir, args)
	requireExit(t, checked, 0)

	receipt := contractObject(t, checked.stdout)
	if len([]byte(checked.stdout)) > 2048 || receipt["next"] != nil || !flagAt(t, receipt, keyNextOmitted) {
		t.Fatalf("long exact argv not omitted safely: %.1000s", checked.stdout)
	}

	reference := objAt(t, receipt, "next_reference")
	if reference["executable_from"] != invokingExecutableReference || reference["report_from"] != "original_argv.--report" {
		t.Fatal("missing caller-owned references")
	}

	next := []string{
		args[0],
		commandReport,
		args[5],
		"--expect-attempt",
		textAt(t, reference, keyExpectedAttempt),
		flagView,
		textAt(t, reference, "view"),
	}
	resumed := executeContinuation(t, tempDir(t), next)
	requireExit(t, resumed, 0)

	if textAt(t, contractObject(t, resumed.stdout), keySavedRun, keyAttemptID) != receipt[keyAttemptID] {
		t.Fatal("reconstruction changed attempt")
	}

	checkOmittedHelpContinuation(t, dir, executable)
}

func TestContinuationPreservesQueriedReportSymlink(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	other := tempDir(t)
	failed := run(t, dir, "", commandCheck, flagReport, fileJob, fileMissing)
	requireExit(t, failed, 1)
	saved := readFile(t, filepath.Join(dir, fileJob))
	linked := filepath.Join(dir, "linked report.json")
	ensure(t, os.Symlink(fileJob, linked))

	queried := run(t, other, "", commandReport, linked)
	requireExit(t, queried, 0)
	envelope := contractObject(t, queried.stdout)
	next := exactNext(t, objAt(t, envelope, "result"))

	if next[0] != binary(t) || next[2] != linked {
		t.Fatalf("continuation changed queried symlink authority: %q", next)
	}

	resumed := executeContinuation(t, other, next)
	requireExit(t, resumed, 0)

	if textAt(t, contractObject(t, resumed.stdout), keySavedRun, keyAttemptID) !=
		textAt(t, contractObject(t, failed.stdout), keyAttemptID) {
		t.Fatal("symlink continuation lost saved attempt")
	}

	if !bytes.Equal(readFile(t, linked), saved) {
		t.Fatal("symlink query modified saved evidence")
	}

	info, err := os.Lstat(linked)
	ensure(t, err)

	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("query replaced the report symlink")
	}
}

// checkOmittedHelpContinuation uses structured arguments, without parsing a rendered example.
func checkOmittedHelpContinuation(tb testing.TB, dir, executable string) {
	tb.Helper()

	help := executeContinuation(tb, dir, []string{executable, flagHelp})
	requireExit(tb, help, 0)
	receipt := contractObject(tb, help.stdout)

	if node(tb, receipt, "next") != nil || !flagAt(tb, receipt, keyNextOmitted) ||
		textAt(tb, receipt, "executable_from") != invokingExecutableReference {
		tb.Fatal("omitted help continuation lost caller authority")
	}

	arguments := listAt(tb, receipt, "next_args")
	if len(arguments) == 0 {
		tb.Fatal("omitted help continuation has no arguments")
	}

	next := make([]string, 1, 1+len(arguments))
	next[0] = executable

	for _, argument := range arguments {
		next = append(next, textAt(tb, argument))
	}

	responseSchema := schemaFromExecutable(tb, dir, responseSchemaName)
	validateContract(tb, responseSchema, help.stdout)
	resumed := executeContinuation(tb, tempDir(tb), next)
	requireExit(tb, resumed, 0)
	validateContract(tb, responseSchema, resumed.stdout)

	if textAt(tb, contractObject(tb, resumed.stdout), "kind") != kindHelp {
		tb.Fatal("omitted help continuation did not launch command help")
	}

	human := executeContinuation(tb, dir, []string{executable, flagHelp, flagFormat, formatText})
	requireExit(tb, human, 0)

	if !strings.Contains(human.stdout, "Next argv omitted: use invoking_executable") {
		tb.Fatal("human help omitted actionable invocation guidance")
	}
}
