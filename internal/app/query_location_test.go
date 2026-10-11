// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestReportQueryFailuresPreserveKnownLocationsAndAvoidInventedOptions(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	runner := nativeInventoryApp()
	data := planJSON(t, map[string]any{keyItems: []any{blank(map[string]any{keySize: "100x100"})}})
	execute(t.Context(), t, runner, dir, commandCheck, inlinePlanFlag, data, reportFlag, reportFile).requireCode(t, 0, "")
	original := readFile(t, filepath.Join(dir, reportFile))
	saved, err := report.Decode(t.Context(), reportFile, strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	expectation := strings.Repeat("A", 26)
	if saved.AttemptID == expectation {
		expectation = strings.Repeat("B", 26)
	}

	for _, args := range [][]string{{"--expect-attempt", expectation}, {"--expect-attempt=" + expectation}} {
		res := execute(t.Context(), t, runner, dir, append([]string{commandReport, reportFile}, args...)...)
		found := decodedQueryFailure(t, res, report.CodeAttemptMismatch, report.StageRead)
		if found.Location == nil || found.Location.File != "argv" || found.Location.ArgvIndex == nil ||
			*found.Location.ArgvIndex != 2 || found.Location.Pointer != "" {
			t.Fatalf("known option declaration lost its actual argv location: %+v", found)
		}
	}

	// '=' is a valid nonempty part ID. This failure has no option provenance;
	// it cannot authorize an argv location just because a value begins with '='.
	res := execute(t.Context(), t, runner, dir, commandReport, reportFile, "--part", "=")
	found := decodedQueryFailure(t, res, report.CodePartNotFound, report.StageUsage)
	if found.Location != nil || !strings.Contains(found.Message, `"="`) {
		t.Fatalf("unknown part acquired invented option provenance or lost its name: %+v", found)
	}

	faultFile := "unsupported-query-report.json"
	fault := fmt.Sprintf(`{"format_version":%d,"kind":"report"}`, report.Version+1)
	writeFile(t, filepath.Join(dir, faultFile), fault)
	path := filepath.Join(dir, faultFile)
	_, decodeErr := report.Decode(t.Context(), path, strings.NewReader(fault))
	known, ok := report.AsError(decodeErr)
	if !ok || known.Diagnostic.Code != report.CodeUnsupportedVersion || known.Diagnostic.Location == nil {
		t.Fatalf("real report decoder did not locate the version fault: %v", decodeErr)
	}
	res = execute(t.Context(), t, runner, dir, commandReport, faultFile, "--expect-attempt="+expectation)
	found = decodedQueryFailure(t, res, report.CodeUnsupportedVersion, report.StageShape)
	if !reflect.DeepEqual(found.Location, known.Diagnostic.Location) {
		t.Fatalf("query adapter replaced saved-file provenance: got=%+v want=%+v", found.Location, known.Diagnostic.Location)
	}

	if readFile(t, filepath.Join(dir, reportFile)) != original || readFile(t, path) != fault {
		t.Fatal("rejected read-only query rewrote an artifact")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("rejected queries left duplicate artifacts: %v/%v", entries, err)
	}
}

func decodedQueryFailure(t *testing.T, result outcome, code report.Code, stage report.Stage) report.DiagnosticView {
	t.Helper()
	var failure report.CommandError
	if err := json.Unmarshal([]byte(result.stdout), &failure); err != nil {
		t.Fatal(err)
	}
	if result.code != 2 || failure.Status != report.StatusInvalid || len(failure.Diagnostics) != 1 ||
		failure.Diagnostics[0].Code != code || failure.Diagnostics[0].Stage != stage {
		t.Fatalf("unexpected query rejection: exit=%d result=%+v", result.code, failure)
	}
	return failure.Diagnostics[0]
}
