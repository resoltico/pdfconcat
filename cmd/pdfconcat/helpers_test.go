// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

type (
	// result is the outcome of one executable run.
	result struct {
		stdout, stderr string
		// tmp is the temporary directory the run was given; a check keeps its scratch files there.
		tmp  string
		code int
	}

	// location is where a diagnostic points: a file and pointer, or a command-line operand.
	location struct {
		ArgvIndex *int   `json:"argv_index"`
		File      string `json:"file"`
		Pointer   string `json:"pointer"`
		Line      int    `json:"line"`
	}

	// diagnostic is a diagnostic of a summary or a report, with only the members the tests read.
	diagnostic struct {
		Location *location `json:"location"`
		Stage    string    `json:"stage"`
		Code     string    `json:"code"`
		Path     string    `json:"path"`
		Message  string    `json:"message"`
	}

	// counts are the page totals of a summary; nil until layout resolved them.
	counts struct {
		Source    *int64 `json:"source_pages"`
		Generated *int64 `json:"generated_pages"`
		Total     *int64 `json:"total_pages"`
	}

	// publication is what a summary says was made visible.
	publication struct {
		Output         string `json:"output"`
		ReportStatus   string `json:"report_status"`
		ReportPath     string `json:"report_path"`
		RecoveryReport string `json:"recovery_report"`
		Published      bool   `json:"published"`
	}

	// summary is the compact result of a build, check, or any failure.
	summary struct {
		Phases             map[string]string `json:"phases"`
		Counts             counts            `json:"counts"`
		Kind               string            `json:"kind"`
		Status             string            `json:"status"`
		Command            string            `json:"command"`
		Diagnostics        []diagnostic      `json:"diagnostics"`
		Next               []string          `json:"next"`
		Publication        publication       `json:"publication"`
		PartCount          int               `json:"part_count"`
		DiagnosticCount    int               `json:"diagnostic_count"`
		DiagnosticsOmitted int               `json:"diagnostics_omitted"`
	}

	// obj is shorthand for a JSON object.
	obj = map[string]any
)

// commandTimeout bounds one executable run; a hung command fails the test instead of the whole suite.
const commandTimeout = 2 * time.Minute

func TestMain(m *testing.M) {
	m.Run()
	exectest.Cleanup()
}

func binary(tb testing.TB) string {
	tb.Helper()

	return exectest.Build(tb, "./cmd/pdfconcat")
}

// start prepares a run of the executable in dir, bounded by commandTimeout.
func start(tb testing.TB, dir string, args ...string) *exec.Cmd {
	tb.Helper()

	path := binary(tb)
	ctx, cancel := context.WithTimeout(tb.Context(), commandTimeout)
	tb.Cleanup(cancel)

	command := exectest.Command(ctx, path, args...)
	command.Dir = dir

	tmp := tb.TempDir()
	command.Env = append(command.Env, "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)

	return command
}

// run executes the command line in dir with stdin and returns what it printed.
func run(tb testing.TB, dir, stdin string, args ...string) result {
	tb.Helper()

	command := start(tb, dir, args...)
	command.Stdin = strings.NewReader(stdin)

	return finish(tb, command)
}

// finish runs a prepared command with captured streams.
func finish(tb testing.TB, command *exec.Cmd) result {
	tb.Helper()

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	return outcome(tb, command.Run(), &stdout, &stderr, tempOf(command))
}

// tempOf returns the TMPDIR a prepared command was given.
func tempOf(command *exec.Cmd) string {
	last := ""

	for _, entry := range command.Env {
		if value, found := strings.CutPrefix(entry, "TMPDIR="); found {
			last = value // the last assignment wins, as it does in the child
		}
	}

	return last
}

func outcome(tb testing.TB, err error, stdout, stderr *bytes.Buffer, tmp string) result {
	tb.Helper()

	code := 0

	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		tb.Fatalf("run pdfconcat: %v", err)
	}

	return result{stdout: stdout.String(), stderr: stderr.String(), code: code, tmp: tmp}
}

// summaryOf decodes a result's standard output as a summary and checks that it is one JSON line.
func summaryOf(tb testing.TB, res result) summary {
	tb.Helper()

	var parsed summary

	decodeLine(tb, res.stdout, &parsed)

	return parsed
}

// decodeLine decodes text as exactly one compact JSON line into target.
func decodeLine(tb testing.TB, text string, target any) {
	tb.Helper()

	if !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 1 {
		tb.Fatalf("standard output is not one line of JSON: %.300q", text)
	}

	var envelope struct {
		Kind   string          `json:"kind"`
		Result json.RawMessage `json:"result"`
	}

	err := json.Unmarshal([]byte(text), &envelope)
	if err != nil {
		tb.Fatal(err)
	}

	payload := []byte(text)
	if envelope.Kind == "report_query" {
		payload = envelope.Result
	}

	err = json.Unmarshal(payload, target)
	if err != nil {
		tb.Fatalf("standard output is not valid JSON: %v\n%.300q", err, text)
	}
}

// generic decodes one JSON line into maps and slices.
func generic(tb testing.TB, text string) map[string]any {
	tb.Helper()

	var value map[string]any

	decodeLine(tb, text, &value)

	return value
}

// requireExit fails unless the run ended with code, showing both streams.
func requireExit(tb testing.TB, res result, code int) {
	tb.Helper()

	if res.code != code {
		tb.Fatalf("exit %d, want %d\nstdout: %.600s\nstderr: %.600s", res.code, code, res.stdout, res.stderr)
	}
}

// requireCode fails unless the summary lists a diagnostic with code.
func requireCode(tb testing.TB, parsed *summary, code string) {
	tb.Helper()

	for _, found := range parsed.Diagnostics {
		if found.Code == code {
			return
		}
	}

	tb.Fatalf("no diagnostic with code %q in %+v", code, parsed.Diagnostics)
}

// writePDFs writes one fixture per name into dir: "<name>.pdf" with the given number of pages, marked "<name> p<N>".
func writePDFs(tb testing.TB, dir string, pages int, names ...string) {
	tb.Helper()

	for _, name := range names {
		path := filepath.Join(dir, name+".pdf")

		err := os.MkdirAll(filepath.Dir(path), 0o700)
		if err != nil {
			tb.Fatal(err)
		}

		err = pdffixture.Pages(name, pages).WriteFile(path)
		if err != nil {
			tb.Fatal(err)
		}
	}
}

// writeFile writes content to dir/name, creating parent directories.
func writeFile(tb testing.TB, dir, name, content string) string {
	tb.Helper()

	path := filepath.Join(dir, name)

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		tb.Fatal(err)
	}

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		tb.Fatal(err)
	}

	return path
}

// pageTexts loads path with the independent tools and returns the first text line of every page.
func pageTexts(tb testing.TB, path string) []string {
	tb.Helper()

	tools := pdforacle.RequireTools(tb)

	doc, err := pdforacle.Load(tools, path)
	if err != nil {
		tb.Fatalf("load %s: %v", path, err)
	}

	texts := make([]string, doc.PageCount())
	for index := range texts {
		texts[index] = doc.FirstLine(index + 1)
	}

	return texts
}

// verifyPages checks the structure of path and that its pages begin with the given first lines, using
// qpdf and Poppler, which share no code with the executable.
func verifyPages(tb testing.TB, path string, want ...string) {
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
		tb.Fatalf("independent verification of %s failed: %v\npages: %q", path, findings, pageTexts(tb, path))
	}
}

// writeSparseFile creates a file of the given size without writing its content, so it is cheap to create but
// takes time to copy.
func writeSparseFile(tb testing.TB, path string, size int64) {
	tb.Helper()

	file, err := os.Create(filepath.Clean(path))
	ensure(tb, err)
	ensure(tb, file.Truncate(size))
	ensure(tb, file.Close())
}

// readFile returns a file's content.
func readFile(tb testing.TB, path string) []byte {
	tb.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		tb.Fatal(err)
	}

	return data
}

// requireAbsent fails if path exists.
func requireAbsent(tb testing.TB, path string) {
	tb.Helper()

	_, err := os.Lstat(path)
	if err == nil {
		tb.Fatalf("%s exists", path)
	}

	if !errors.Is(err, os.ErrNotExist) {
		tb.Fatal(err)
	}
}

// requireNoScratch fails if dir holds a leftover job workspace.
func requireNoScratch(tb testing.TB, dir string) {
	tb.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		tb.Fatal(err)
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name(), "pdfconcat-job-") || strings.Contains(entry.Name(), ".pdfconcat-staged-") {
			tb.Fatalf("leftover scratch entry %s in %s", entry.Name(), dir)
		}
	}
}

// planJSON renders a plan object.
func planJSON(tb testing.TB, value any) string {
	tb.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		tb.Fatal(err)
	}

	return string(data)
}

// itemsOf is shorthand for a plan with the given items.
func itemsOf(items ...any) obj {
	return obj{keyVersion: 1, keyItems: items}
}

func ensure(tb testing.TB, err error) {
	tb.Helper()

	if err != nil {
		tb.Fatal(err)
	}
}

// writePDFsAt writes a one-page PDF marked tag at exactly path.
func writePDFsAt(tb testing.TB, path, tag string) {
	tb.Helper()

	ensure(tb, os.MkdirAll(filepath.Dir(path), 0o700))
	ensure(tb, pdffixture.Plain(tag).WriteFile(path))
}

// runTool runs an independent QA program and returns its standard output.
func runTool(tb testing.TB, program string, args ...string) string {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), commandTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer

	command := exectest.Command(ctx, program, args...)
	command.Stdout, command.Stderr = &stdout, &stderr

	err := command.Run()
	if err != nil {
		tb.Fatalf("%s: %v\n%.400s", program, err, stderr.String())
	}

	return stdout.String()
}

// tempDir is a temporary directory whose path has no symbolic-link component, so the path a test names is
// the path the executable reports (macOS keeps temporary directories behind /var -> /private/var).
func tempDir(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.EvalSymlinks(tb.TempDir())
	ensure(tb, err)

	return dir
}

func sprintMissing(index int) string { return "missing/" + strconv.Itoa(index) + ".pdf" }

func sprintInt(value int) string { return strconv.Itoa(value) }

// node follows keys through nested JSON objects of a decoded value and returns what it finds there.
func node(tb testing.TB, value any, keys ...string) any {
	tb.Helper()

	for _, key := range keys {
		object, ok := value.(obj)
		if !ok {
			tb.Fatalf("looking for %q: %v is not a JSON object", key, value)
		}

		value, ok = object[key]
		if !ok {
			tb.Fatalf("no member %q in %v", key, object)
		}
	}

	return value
}

// objAt is the JSON object at keys.
func objAt(tb testing.TB, value any, keys ...string) obj {
	tb.Helper()

	found, ok := node(tb, value, keys...).(obj)
	if !ok {
		tb.Fatalf("%v at %v is not a JSON object", value, keys)
	}

	return found
}

// listAt is the JSON array at keys.
func listAt(tb testing.TB, value any, keys ...string) []any {
	tb.Helper()

	found, ok := node(tb, value, keys...).([]any)
	if !ok {
		tb.Fatalf("%v at %v is not a JSON array", value, keys)
	}

	return found
}

// textAt is the JSON string at keys.
func textAt(tb testing.TB, value any, keys ...string) string {
	tb.Helper()

	found, ok := node(tb, value, keys...).(string)
	if !ok {
		tb.Fatalf("%v at %v is not a JSON string", value, keys)
	}

	return found
}

// numberAt is the JSON number at keys.
func numberAt(tb testing.TB, value any, keys ...string) float64 {
	tb.Helper()

	found, ok := node(tb, value, keys...).(float64)
	if !ok {
		tb.Fatalf("%v at %v is not a JSON number", value, keys)
	}

	return found
}

// flagAt is the JSON boolean at keys.
func flagAt(tb testing.TB, value any, keys ...string) bool {
	tb.Helper()

	found, ok := node(tb, value, keys...).(bool)
	if !ok {
		tb.Fatalf("%v at %v is not a JSON boolean", value, keys)
	}

	return found
}
