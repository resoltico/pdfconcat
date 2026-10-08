// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestPathCommandsRejectNonUnicodeWorkingDirectoryEnvironment(t *testing.T) {
	t.Parallel()
	actual := workDir(t)
	writePDF(t, actual, sourceA)

	for _, command := range []string{commandBuild, commandCheck, commandReport} {
		args := []string{command, sourceA}
		if command == commandBuild {
			args = append(args, outputFlag, outputFile)
		}

		env := app.Env{WorkingDir: filepath.Join(actual, string([]byte{0xff}))}
		result := executeWith(t.Context(), t, appOf(newFake(t)), env, args...)
		result.requireCode(t, 1, "working_directory_invalid_utf8")

		if !utf8.ValidString(result.stdout) || strings.ContainsRune(result.stdout, '�') {
			t.Fatal("invalid CWD escaped lossily")
		}
	}

	requireMissing(t, filepath.Join(actual, outputFile))
}

// An unresolved working directory cannot turn an explicit report request into no request.
func TestUnavailableWorkingDirectoryRetainsRequestedReportReceipt(t *testing.T) {
	t.Parallel()
	actual := workDir(t)
	writePDF(t, actual, sourceA)
	target := filepath.Join(actual, reportFile)
	writeFile(t, target, "historical report")

	for _, directory := range []string{"", filepath.Join(actual, string([]byte{0xff}))} {
		for _, command := range []string{commandBuild, commandCheck} {
			args := []string{command, sourceA, reportFlag, target, overwriteFlag}
			if command == commandBuild {
				args = append(args, outputFlag, outputFile)
			}

			result := executeWith(t.Context(), t, appOf(newFake(t)), app.Env{WorkingDir: directory}, args...)
			if result.code != 1 {
				t.Fatalf("expected rejected CWD: %s", result.stdout)
			}

			assertUnresolvedReportReceipt(t, result.stdout)

			if readFile(t, target) != "historical report" {
				t.Fatal("unresolved receipt replaced historical report")
			}
		}
	}

	requireMissing(t, filepath.Join(actual, outputFile))
}

func assertUnresolvedReportReceipt(t *testing.T, stdout string) {
	t.Helper()

	var summary report.Summary
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatal(err)
	}

	pub := summary.Publication

	valid := summary.AttemptID != "" && pub.ReportStatus == report.ReportFailed && pub.ReportWrite == "not_written" &&
		pub.ReportTargetObservation == "unknown" && pub.ReportFrom == "original_argv.--report" && pub.ReportPath == ""
	if !valid {
		t.Fatalf("untruthful unresolved receipt: %+v", summary)
	}
}
