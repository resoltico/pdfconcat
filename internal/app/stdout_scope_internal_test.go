// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	stdoutCaptureErrorOnly   = "error-only"
	stdoutCaptureWarningOnly = "warning-only"
	stdoutScopePlan          = `{"version":1,"items":[{"blank":{"size":"A4","text":{"value":"Clipped title",` +
		`"anchor":"bottom-left","x":-90,"y":100,"width":200,"overflow":"allow"}}}]}`
)

func TestBrokenStdoutKeepsCapturedErrorWarningAndMixedScopes(t *testing.T) {
	t.Parallel()

	for _, name := range []string{stdoutCaptureErrorOnly, stdoutCaptureWarningOnly, "mixed"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			state, data := runStdoutScope(t, name)
			if state.Status != report.StatusFailed || state.DiagnosticCount != 1 || state.ErrorCount != 1 || state.WarningCount != 0 {
				t.Fatalf("delivery failure absorbed saved diagnostics: %+v", state)
			}

			if len(state.Diagnostics) != 1 || state.Diagnostics[0].Severity != report.SeverityError ||
				state.Diagnostics[0].Code != stdoutWriteCode {
				t.Fatal("stdout failure lost its own severity and cause")
			}

			assertCapturedStdoutScope(t, name, state.SavedRun)
			validateOutputContract(t, data)
		})
	}
}

func runStdoutScope(t *testing.T, name string) (committedState, []byte) {
	t.Helper()
	dir := t.TempDir()
	runner := New(func() (Engine, error) { return pdfengine.New() })
	output := filepath.Join(dir, "scope.pdf")
	requested := filepath.Join(dir, "scope.json")

	args := []string{buildName, "-o", output, fixtureReportOption, requested, "--plan-json", stdoutScopePlan}
	if name == stdoutCaptureErrorOnly {
		args = []string{buildName, "-o", output, fixtureReportOption, requested, filepath.Join(dir, "absent.pdf")}
	}

	if name == "mixed" {
		runner.afterPDFCommit = func() {
			if err := os.Mkdir(requested, 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}

	var stderr bytes.Buffer

	writer := &failingWriter{}
	if code := runner.Run(t.Context(), args, Env{WorkingDir: dir, Stdout: writer, Stderr: &stderr}); code != 1 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}

	var state committedState
	if err := json.Unmarshal(stderr.Bytes(), &state); err != nil {
		t.Fatal(err)
	}

	if state.Published {
		if _, err := os.Stat(output); err != nil {
			t.Fatal("published-state rescue lost the actual PDF")
		}
	}

	return state, stderr.Bytes()
}

func assertCapturedStdoutScope(t *testing.T, name string, saved report.SavedRun) {
	t.Helper()

	expectedErrors, expectedWarnings := 1, 1
	if name == stdoutCaptureErrorOnly {
		expectedWarnings = 0
	}

	if name == stdoutCaptureWarningOnly {
		expectedErrors = 0
	}

	if saved.ErrorCount != expectedErrors || saved.WarningCount != expectedWarnings ||
		saved.DiagnosticCount != expectedErrors+expectedWarnings {
		t.Fatalf("captured scopes changed: %s: %+v", name, saved)
	}

	if (saved.Status == report.StatusOK) != (name == stdoutCaptureWarningOnly) || saved.Published != (name != stdoutCaptureErrorOnly) {
		t.Fatalf("captured outcome was replaced by stdout failure: %+v", saved)
	}
}

func TestCommittedStateSchemaRejectsMalformedDeliveryScopes(t *testing.T) {
	t.Parallel()

	state, _ := runStdoutScope(t, stdoutCaptureWarningOnly)
	for _, change := range []func(*committedState){
		func(s *committedState) { s.Status = report.StatusOK },
		func(s *committedState) { s.ErrorCount = 0 },
		func(s *committedState) { s.WarningCount = 1 },
		func(s *committedState) { s.DiagnosticCount = 2 },
		func(s *committedState) { s.Diagnostics[0].Severity = report.SeverityWarning },
	} {
		copyState := state
		copyState.Diagnostics = append([]report.Diagnostic{}, state.Diagnostics...)
		change(&copyState)

		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(outputContractJSON(t, copyState)))
		if err != nil {
			t.Fatal(err)
		}

		if outputResponseSchema(t).Validate(value) == nil {
			t.Fatal("malformed stdout-delivery scope accepted")
		}
	}
}
