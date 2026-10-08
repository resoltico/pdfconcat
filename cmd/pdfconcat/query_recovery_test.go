// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"bytes"
	"path/filepath"
	"testing"
)

const (
	recoveryMember      = "recovery"
	expectedAttemptFlag = "--expect-attempt"
)

func TestReportQuerySelectionErrorDoesNotSuggestReplacingSavedReport(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	requireExit(t, run(t, dir, "", commandCheck, flagReport, fileJob, fileA), 0)
	saved := readFile(t, filepath.Join(dir, fileJob))
	failed := run(t, dir, "", commandReport, fileJob, partFlag, "/items/99")
	requireExit(t, failed, 2)
	schema := schemaFromExecutable(t, dir, responseSchemaName)
	validateContract(t, schema, failed.stdout)

	diagnostic := objAt(t, listAt(t, contractObject(t, failed.stdout), diagnosticsView)[0])

	recovery := objAt(t, diagnostic, recoveryMember)
	if diagnostic["code"] != "report_part_not_found" || recovery["action"] != "open_help" || recovery["command"] != commandReport {
		t.Fatalf("selection error misclassified as replacing saved evidence: %v", diagnostic)
	}

	if !bytes.Equal(saved, readFile(t, filepath.Join(dir, fileJob))) {
		t.Fatal("failed query modified saved report")
	}
}

func TestReportAttemptMismatchNamesTheActualOptionDeclaration(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	requireExit(t, run(t, dir, "", commandCheck, flagReport, fileJob, fileA), 0)
	saved := readFile(t, filepath.Join(dir, fileJob))

	schema := schemaFromExecutable(t, dir, responseSchemaName)
	for _, test := range []struct {
		name  string
		args  []string
		index int
	}{
		{"separate value", []string{commandReport, fileJob, "--format", "json", expectedAttemptFlag, "different-attempt"}, 4},
		{"joined value", []string{commandReport, fileJob, "--view=parts", expectedAttemptFlag + "=different-attempt"}, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			failed := run(t, dir, "", test.args...)
			requireExit(t, failed, 2)
			validateContract(t, schema, failed.stdout)

			diagnostic := objAt(t, listAt(t, contractObject(t, failed.stdout), diagnosticsView)[0])
			if diagnostic["code"] != "report_attempt_mismatch" {
				t.Fatalf("missing attempt guard: %v", diagnostic)
			}

			location := objAt(t, diagnostic, "location")
			if location["file"] != "argv" || location["argv_index"] != float64(test.index) {
				t.Fatalf("attempt guard named the wrong argv declaration: %v; args=%v", location, test.args)
			}

			recovery := objAt(t, diagnostic, recoveryMember)
			if recovery["action"] != "inspect_report" || recovery["report_from"] != "original_argv.report_operand" {
				t.Fatalf("mismatch lost saved-evidence guidance: %v", recovery)
			}

			if !bytes.Equal(saved, readFile(t, filepath.Join(dir, fileJob))) {
				t.Fatal("mismatch modified saved report")
			}
		})
	}
}
