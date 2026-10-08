// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/report"
)

// outputReceiptWriter fails the real result write after recording the already committed PDF.
type outputReceiptWriter struct {
	path      string
	cause     error
	committed []byte
	calls     int
}

func (w *outputReceiptWriter) Write([]byte) (int, error) {
	data, err := os.ReadFile(w.path)
	if err != nil {
		return 0, fmt.Errorf("read committed output: %w", err)
	}

	w.committed = data
	w.calls++

	return 0, w.cause
}

func TestCommittedStateExactWireBoundaryKeepsCompletePublication(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("%s: %w", strings.Repeat("ā", 500), errInjected)
	// This independent wire envelope sizes a supported path; the actual operation supplies its attempt ID.
	base := map[string]any{
		"format_version": 2, "attempt_id": strings.Repeat("A", 26), "kind": "committed_state",
		"command": buildName, "status": "ok", "published": true, "output": "",
		"report_status": "not_requested", "stdout_error": "write result: " + cause.Error(),
	}
	dir := outputContractDirectory(t)
	target := outputContractPathWireContent(t, dir, "out.pdf", 2048-len(outputContractJSON(t, base)))

	state, data := runOutputFailure(t, dir, target, cause, "", nil)
	if len(data) != 2048 || state.Output != target || state.StdoutError != "write result: "+cause.Error() || state.Truncated {
		t.Fatalf("exact-boundary receipt changed complete values: bytes=%d state=%+v", len(data), state)
	}
}

func TestCommittedStateMarksOnlyShortenedPublicationPreviews(t *testing.T) {
	t.Parallel()
	dir := outputContractDirectory(t)
	target := outputContractPathWireContent(t, filepath.Join(dir, "output"), "out.pdf", 850)
	requested := outputContractPathWireContent(t, filepath.Join(dir, "report"), "r.json", 850)
	interrupt := func() {
		if err := os.Mkdir(requested, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	state, data := runOutputFailure(t, dir, target, errInjected, requested, interrupt)
	if len(data) > 2048 || !state.Truncated {
		t.Fatalf("shortened publication cue: bytes=%d state=%+v", len(data), state)
	}

	if state.Output == target || state.ReportPath == requested {
		t.Fatalf("over-limit publication paths unchanged: %+v", state)
	}

	if state.RecoveryReport != "" || state.RecoveryBasename == "" || state.RecoveryDirectoryFrom != "original_report_argument" {
		t.Fatalf("shortened recovery lacks lossless reference: %+v", state)
	}
}

func runOutputFailure(t *testing.T, dir, target string, cause error, requested string, interrupt func()) (committedState, []byte) {
	t.Helper()
	source := prepareOutputContractSource(t, dir, target, requested)
	runner := New(func() (Engine, error) { return pdfengine.New() })
	runner.afterPDFCommit = interrupt
	writer := &outputReceiptWriter{path: target, cause: cause}

	var stderr bytes.Buffer

	args := []string{buildName, "-o", target, source}
	if requested != "" {
		args = append(args, "--report", requested)
	}

	code := runner.Run(t.Context(), args, Env{
		WorkingDir: dir, Executable: filepath.Join(dir, "pdfconcat"), Stdout: writer, Stderr: &stderr,
	})
	if code != 1 || writer.calls != 1 || len(writer.committed) == 0 {
		t.Fatalf("broken stdout lost committed operation: exit=%d calls=%d stderr=%s", code, writer.calls, stderr.String())
	}

	after := readOutputContractPDF(t, target)
	if !bytes.Equal(after, writer.committed) {
		t.Fatal("broken stdout changed committed PDF")
	}

	validateOutputContract(t, stderr.Bytes())

	var state committedState
	if err := json.Unmarshal(stderr.Bytes(), &state); err != nil {
		t.Fatal(err)
	}

	if !state.Published || state.Command != buildName || state.AttemptID == "" {
		t.Fatalf("last committed state missing: %+v", state)
	}

	return state, stderr.Bytes()
}

func TestCommandErrorPreservesDeclaredRecoveryAndSuppliesHelpFallback(t *testing.T) {
	t.Parallel()

	index := 3

	declared := report.Recovery{Action: "edit_input", Command: checkName, Location: &report.Location{ArgvIndex: &index, File: "argv"}}
	for _, recovery := range []*report.Recovery{nil, &declared} {
		t.Run(strconv.FormatBool(recovery != nil), func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer

			code := emitError(Env{Executable: "/tools/pdfconcat", Stdout: &stdout}, cli.FormatJSON, checkName, report.StatusInvalid,
				report.Diagnostic{Stage: report.StageUsage, Code: "command_bad", Message: "repair this declaration", Recovery: recovery})
			if code != 2 {
				t.Fatalf("invalid command exit %d", code)
			}

			validateOutputContract(t, stdout.Bytes())

			var result report.CommandError
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}

			want := &report.Recovery{Action: "open_help", Command: checkName}
			if recovery != nil {
				want = &declared
			}

			if len(result.Diagnostics) != 1 || !reflect.DeepEqual(result.Diagnostics[0].Recovery, want) {
				t.Fatalf("declared recovery replaced or fallback missing: %+v", result.Diagnostics)
			}
		})
	}
}

func TestHelpContinuationUsesIndependentWireBoundsAndInvokingAuthority(t *testing.T) {
	t.Parallel()

	for _, name := range []cli.Name{"", cli.NameBuild, cli.NameReport} {
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()

			limit := 4096

			args := []string{"schema", "plan"}
			if name == "" {
				limit, args = 2048, []string{"build", "--help"}
			}

			if name == cli.NameReport {
				args = []string{"schema", "report"}
			}

			verifyHelpAuthorityCases(t, name, args, limit)
		})
	}
}

func verifyHelpAuthorityCases(t *testing.T, name cli.Name, args []string, limit int) {
	t.Helper()

	doc := cli.Help(name)

	candidate := append([]string{""}, args...)
	doc.Next = &candidate

	base := len(outputContractJSON(t, doc))
	for _, size := range []int{0, limit - base, limit - base + 1} {
		executable := ""
		if size > 0 {
			executable = outputContractPathWireContent(
				t,
				filepath.Join(filepath.VolumeName(outputContractDirectory(t))+string(filepath.Separator), "tools"),
				"pdfconcat",
				size,
			)
		}

		var expected *[]string

		if size != 0 && base+size <= limit {
			next := append([]string{executable}, args...)
			expected = &next
		}

		verifyHelpContinuation(t, name, args, limit, executable, expected)
	}
}

func verifyHelpContinuation(t *testing.T, name cli.Name, args []string, limit int, executable string, expected *[]string) {
	t.Helper()

	var stdout bytes.Buffer

	code := runHelp(&cli.Command{Name: cli.NameHelp, HelpFor: name, Format: cli.FormatJSON}, Env{Executable: executable, Stdout: &stdout})
	if code != 0 || stdout.Len() > limit {
		t.Fatalf("help wire bound: command=%s bytes=%d limit=%d exit=%d", name, stdout.Len(), limit, code)
	}

	validateOutputContract(t, stdout.Bytes())

	var got cli.HelpDoc
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if expected != nil {
		if got.Next == nil || !reflect.DeepEqual(got.Next, expected) || got.NextOmitted {
			t.Fatalf("fitting help lost actual continuation: executable=%q next=%+v", executable, got.Next)
		}

		return
	}

	if got.Next != nil || !got.NextOmitted || got.ExecutableFrom != "invoking_executable" || !reflect.DeepEqual(got.NextArgs, args) {
		t.Fatalf("omitted help lost caller-owned authority: %+v", got)
	}
}

func prepareOutputContractSource(t *testing.T, dir, target, requested string) string {
	t.Helper()

	for _, path := range []string{target, requested} {
		if path != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}

	source := filepath.Join(dir, "source.pdf")
	if err := pdffixture.Pages("source", 1).WriteFile(source); err != nil {
		t.Fatal(err)
	}

	return source
}

func readOutputContractPDF(t *testing.T, path string) []byte {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func outputContractDirectory(t *testing.T) string {
	t.Helper()

	directorySuffix := "wire\\path\u2028\u2029"

	base := filepath.Join(t.TempDir(), directorySuffix)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}

	dir, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

// outputContractPathWireContent fills short components to the chosen JSON string-content byte width.
func outputContractPathWireContent(t *testing.T, dir, name string, size int) string {
	t.Helper()

	remaining := size - outputContractStringContentBytes(t, filepath.Join(dir, name))
	if remaining < 0 {
		t.Fatalf("boundary does not fit path: size=%d directory=%s", size, dir)
	}

	for remaining > 180 {
		before := outputContractStringContentBytes(t, filepath.Join(dir, name))
		dir = filepath.Join(dir, strings.Repeat("d", 80))
		remaining -= outputContractStringContentBytes(t, filepath.Join(dir, name)) - before
	}

	path := filepath.Join(dir, strings.Repeat("x", remaining)+name)
	if got := outputContractStringContentBytes(t, path); got != size {
		t.Fatalf("boundary path has %d encoded content bytes, want %d", got, size)
	}

	return path
}

func outputContractJSON(t *testing.T, value any) []byte {
	t.Helper()
	// Standard v2 encoding defines the contract's literal UTF-8 wire form; no production encoder helper is used.
	data, err := jsonv2.Marshal(value, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		t.Fatal(err)
	}

	return append(data, '\n')
}

func outputContractStringContentBytes(t *testing.T, value string) int {
	t.Helper()
	// Remove the two enclosing quotes and the newline from the independent encoded string.
	return len(outputContractJSON(t, value)) - 3
}

func validateOutputContract(t *testing.T, data []byte) {
	t.Helper()

	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	source, err := jsonschema.UnmarshalJSON(strings.NewReader(report.ResponseSchema()))
	if err != nil {
		t.Fatal(err)
	}

	compiler := jsonschema.NewCompiler()
	if err = compiler.AddResource("https://test.invalid/response", source); err != nil {
		t.Fatal(err)
	}

	schema, err := compiler.Compile("https://test.invalid/response")
	if err != nil {
		t.Fatal(err)
	}

	if err = schema.Validate(value); err != nil {
		t.Fatal(err)
	}
}
