// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/report"
)

type declarationFault struct {
	name, document, code, pointer string
	line, column, exit            int
	bounded                       bool
}

const firstPlanItemPointer = "/items/0"

// contractObject intentionally reads the outer operation envelope, unlike projection helpers.
func contractObject(tb testing.TB, data string) map[string]any {
	tb.Helper()

	var value map[string]any
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		tb.Fatal(err)
	}

	return value
}

func schemaFromExecutable(tb testing.TB, dir, name string) *jsonschema.Schema {
	tb.Helper()
	output := run(tb, dir, "", commandSchema, name)
	requireExit(tb, output, 0)
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(output.stdout))
	ensure(tb, err)

	compiler := jsonschema.NewCompiler()
	ensure(tb, compiler.AddResource("https://test.invalid/schema", value))
	schema, err := compiler.Compile("https://test.invalid/schema")
	ensure(tb, err)

	return schema
}

func validateContract(tb testing.TB, schema *jsonschema.Schema, data string) {
	tb.Helper()

	value, err := jsonschema.UnmarshalJSON(strings.NewReader(data))
	ensure(tb, err)

	if validationErr := schema.Validate(value); validationErr != nil {
		tb.Fatalf("schema rejects actual response: %v\n%.2000s", validationErr, data)
	}
}

func TestExecutableResponseAndSavedReportContracts(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	responseSchema := schemaFromExecutable(t, dir, responseSchemaName)
	reportSchema := schemaFromExecutable(t, dir, commandReport)
	writePDFs(t, dir, 1, "a")

	cases := []struct {
		args []string
		exit int
	}{
		{args: []string{}, exit: 2},
		{args: []string{"wat"}, exit: 2},
		{args: []string{commandCheck, "--plna", "x"}, exit: 2},
		{args: []string{commandCheck, flagPlan, "absent.json"}, exit: 1},
		{args: []string{flagHelp}, exit: 0},
		{args: []string{commandBuild, flagHelp}, exit: 0},
		{args: []string{commandCheck, flagHelp}, exit: 0},
		{args: []string{commandReport, flagHelp}, exit: 0},
		{args: []string{commandVersion}, exit: 0},
		{args: []string{commandCheck, inlinePlanFlag, "{"}, exit: 2},
		{args: []string{commandCheck, flagReport, failedReportPath, fileMissing}, exit: 1},
		{args: []string{commandCheck, flagReport, successfulReportPath, fileA, flagBlank}, exit: 0},
		{args: []string{commandCheck, flagDetails, fileA, flagBlank}, exit: 0},
		{args: []string{commandReport, failedReportPath}, exit: 0},
		{args: []string{commandReport, failedReportPath, flagView, diagnosticsView}, exit: 0},
		{args: []string{commandReport, successfulReportPath, flagPage, "2"}, exit: 0},
		{args: []string{commandReport, successfulReportPath, partFlag, directBlankPart, flagDetails}, exit: 0},
		{args: []string{commandReport, successfulReportPath, flagView, viewParts, flagDetails}, exit: 0},

		{args: []string{
			commandCheck, inlinePlanFlag, `{"version":1,"items":[{"blank":{"size":"A4","text":{"value":"אב"}}}]}`,
			flagReport, "unsupported.json",
		}, exit: 2},
		{args: []string{
			commandCheck, inlinePlanFlag, `{"version":1,"items":[{"blank":{"size":"A4","text":{"value":"wide","x":"500mm"}}}]}`,
			flagReport, "overflow.json",
		}, exit: 2},
	}

	for _, test := range cases {
		output := run(t, dir, "", test.args...)
		requireExit(t, output, test.exit)
		validateContract(t, responseSchema, output.stdout)

		envelope := contractObject(t, output.stdout)
		if numberAt(t, envelope, "format_version") != float64(report.Version) {
			t.Fatalf("format identity: %v", envelope)
		}

		requireErrorEnvelope(t, envelope)
	}

	for _, file := range []string{failedReportPath, successfulReportPath, "unsupported.json", "overflow.json"} {
		validateContract(t, reportSchema, string(readFile(t, filepath.Join(dir, file))))
	}
}

func requireErrorEnvelope(tb testing.TB, envelope obj) {
	tb.Helper()

	if textAt(tb, envelope, "kind") != "error" {
		return
	}

	for _, field := range []string{"counts", "phases", keyPublication, "part_count"} {
		if _, found := envelope[field]; found {
			tb.Fatalf("command error has irrelevant %s", field)
		}
	}
}

func TestHelpInlineTemplatesPassActualDecoder(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	responseSchema := schemaFromExecutable(t, dir, responseSchemaName)
	help := contractObject(t, run(t, dir, "", commandBuild, flagHelp).stdout)
	checkedTemplates := 0

	for _, entry := range listAt(t, help, "templates") {
		args := listAt(t, entry, "argv")
		if len(args) <= 2 || args[1] != inlinePlanFlag {
			continue
		}

		checked := run(t, dir, "", commandCheck, inlinePlanFlag, textAt(t, args[2]))
		requireExit(t, checked, 0)
		validateContract(t, responseSchema, checked.stdout)

		checkedTemplates++
	}

	if checkedTemplates == 0 {
		t.Fatal("no inline starter template was checked")
	}
}

func exactNext(tb testing.TB, value map[string]any) []string {
	tb.Helper()

	values := listAt(tb, value, "next")

	result := make([]string, len(values))
	for index, item := range values {
		result[index] = textAt(tb, item)
	}

	return result
}

func executeContinuation(tb testing.TB, cwd string, args []string) result {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), commandTimeout)
	defer cancel()

	command := exectest.Command(ctx, args[0], args[1:]...)
	command.Dir = cwd

	return finish(tb, command)
}

func TestReportContinuationBindsExecutableArtifactAndAttempt(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	other := tempDir(t)
	writePDFs(t, dir, 1, "a")
	failed := run(t, dir, "", commandCheck, flagReport, fileJob, fileMissing)
	requireExit(t, failed, 1)
	first := contractObject(t, failed.stdout)
	attempt := textAt(t, first, keyAttemptID)
	saved := readFile(t, filepath.Join(dir, fileJob))
	writeFile(t, dir, copiedFailureReport, string(saved))
	success := run(t, dir, "", commandCheck, flagReport, fileJob, flagOverwrite, fileA)
	requireExit(t, success, 0)

	if contractObject(t, success.stdout)[keyAttemptID] == attempt {
		t.Fatal("attempt identity reused")
	}

	copied := run(t, other, "", commandReport, filepath.Join(dir, copiedFailureReport))
	requireExit(t, copied, 0)

	envelope := contractObject(t, copied.stdout)
	if envelope["status"] != "ok" || envelope["command"] != commandReport {
		t.Fatal("query outcome confused with saved failure")
	}

	savedRun := objAt(t, envelope, keySavedRun)
	if savedRun[keyAttemptID] != attempt || savedRun["status"] != "failed" {
		t.Fatal("lost saved identity")
	}

	next := exactNext(t, objAt(t, envelope, "result"))
	if !filepath.IsAbs(next[0]) || next[0] != binary(t) || next[2] != filepath.Join(dir, copiedFailureReport) {
		t.Fatalf("wrong continuation authority: %q", next)
	}

	resumed := executeContinuation(t, other, next)
	requireExit(t, resumed, 0)

	if !strings.Contains(resumed.stdout, codeSourceUnreadable) {
		t.Fatal("continuation followed successful original")
	}

	writeFile(t, dir, copiedFailureReport, string(readFile(t, filepath.Join(dir, fileJob))))
	mismatch := executeContinuation(t, other, next)
	requireExit(t, mismatch, 2)

	if !strings.Contains(mismatch.stdout, "report_attempt_mismatch") {
		t.Fatalf("missing guard: %s", mismatch.stdout)
	}
}

func TestHistoricalReportRejectionPreservesEvidence(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	requireExit(t, run(t, dir, "", commandBuild, "-o", fileOut, flagReport, fileJob, fileA), 0)
	beforePDF := readFile(t, filepath.Join(dir, fileOut))
	saved := readFile(t, filepath.Join(dir, fileJob))
	// Historical format rejection must not modify either evidence or output.
	old := strings.Replace(string(saved), `"format_version":3`, `"report_version":1`, 1)
	writeFile(t, dir, historicalReport, old)
	rejected := run(t, dir, "", commandReport, historicalReport)
	requireExit(t, rejected, 2)

	schema := schemaFromExecutable(t, dir, responseSchemaName)
	validateContract(t, schema, rejected.stdout)
	diagnostic := objAt(t, listAt(t, contractObject(t, rejected.stdout), diagnosticsView)[0])

	recovery := objAt(t, diagnostic, recoveryMember)
	if recovery["action"] != "choose_new_report" || recovery["report_from"] != "unused_report_target" {
		t.Fatalf("historical report has no safe structured replacement guidance: %v", diagnostic)
	}

	if !bytes.Equal(beforePDF, readFile(t, filepath.Join(dir, fileOut))) ||
		!bytes.Equal(saved, readFile(t, filepath.Join(dir, fileJob))) {
		t.Fatal("historical report query modified published artifacts")
	}

	if !strings.Contains(rejected.stdout, "NEW report target") || string(readFile(t, filepath.Join(dir, historicalReport))) != old {
		t.Fatal("old evidence guidance or preservation failed")
	}
}

func TestEarlyFailureReportReceiptAndSafeRecovery(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	initial := run(t, dir, "", commandBuild, "-o", fileOut, flagReport, fileJob, fileA, flagBlank)
	requireExit(t, initial, 0)
	beforePDF := readFile(t, filepath.Join(dir, fileOut))
	beforeReport := readFile(t, filepath.Join(dir, fileJob))
	malformed := run(t, dir, "{", commandBuild, flagPlan, "-", "-o", fileOut, flagReport, fileJob, flagOverwrite)
	requireExit(t, malformed, 2)

	if strings.Contains(malformed.stdout, "use --overwrite") {
		t.Fatal("early failure cause contradicts the no-overwrite policy")
	}

	current := contractObject(t, malformed.stdout)
	if current[keyAttemptID] == contractObject(t, initial.stdout)[keyAttemptID] {
		t.Fatal("failure identity reused")
	}

	if !bytes.Equal(beforePDF, readFile(t, filepath.Join(dir, fileOut))) ||
		!bytes.Equal(beforeReport, readFile(t, filepath.Join(dir, fileJob))) {
		t.Fatal("early failure replaced an artifact")
	}

	pub := objAt(t, current, keyPublication)
	if pub["report_write"] != "not_written" || pub["report_target_observation"] != "unknown" {
		t.Fatalf("unsafe receipt: %v", pub)
	}

	if !strings.Contains(malformed.stdout, "--overwrite cannot replace") || !strings.Contains(malformed.stdout, "choose_new_report") {
		t.Fatal("bounded recovery hides safety policy")
	}

	fresh := run(t, dir, "{", commandCheck, flagPlan, "-", flagReport, freshReportPath)
	requireExit(t, fresh, 2)
	receipt := contractObject(t, fresh.stdout)

	saved := contractObject(t, string(readFile(t, filepath.Join(dir, freshReportPath))))
	if receipt[keyAttemptID] != saved[keyAttemptID] {
		t.Fatal("failure report identity differs")
	}

	resumed := executeContinuation(t, dir, exactNext(t, receipt))
	requireExit(t, resumed, 0)
	fixed := run(
		t,
		dir,
		`{"version":1,"items":["a.pdf",{"blank":{}},{"blank":{}}]}`,
		commandBuild,
		flagPlan,
		"-",
		"-o",
		fileOut,
		flagReport,
		"repaired.json",
		flagOverwrite,
	)
	requireExit(t, fixed, 0)
	verifyPages(t, filepath.Join(dir, fileOut), "a p1", "", "")
}

// requireSavedAttempt correlates the operation receipt and a query of its persisted evidence.
func requireSavedAttempt(tb testing.TB, dir, path string, receipt obj, status string) {
	tb.Helper()

	attempt := textAt(tb, receipt, keyAttemptID)
	if attempt == "" {
		tb.Fatal("missing attempt identity")
	}

	saved := contractObject(tb, string(readFile(tb, filepath.Join(dir, path))))
	if textAt(tb, saved, keyAttemptID) != attempt || textAt(tb, saved, "status") != status {
		tb.Fatal("saved report differs from attempt receipt")
	}

	resumed := executeContinuation(tb, dir, reportContinuation(tb, receipt, filepath.Join(dir, path)))
	requireExit(tb, resumed, 0)
	queried := contractObject(tb, resumed.stdout)

	if textAt(tb, queried, keySavedRun, keyAttemptID) != attempt || textAt(tb, queried, keySavedRun, "status") != status {
		tb.Fatal("continuation differs from saved attempt")
	}
}

func TestEmptyTextValueClearsDefaultsThroughExecutable(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	plan := obj{
		keyVersion: 1,
		keyBlank:   obj{keySize: "A4", keyBackground: "#eef4ff", keyText: obj{keyValue: dividerText}},
		keyItems:   []any{obj{keyBlank: obj{}}, obj{keyBlank: obj{keyText: obj{keyValue: ""}, keyBackground: "none"}}},
	}
	checked := run(t, dir, "", commandCheck, inlinePlanFlag, planJSON(t, plan), flagReport, successfulReportPath)
	requireExit(t, checked, 0)
	requireSavedAttempt(t, dir, successfulReportPath, contractObject(t, checked.stdout), "ok")
	built := run(t, dir, "", commandBuild, inlinePlanFlag, planJSON(t, plan), "-o", fileOut, flagReport, fileSavedReport)
	requireExit(t, built, 0)
	verifyPages(t, filepath.Join(dir, fileOut), dividerText, "")
	cleared := generic(t, run(t, dir, "", commandReport, fileSavedReport, flagPage, "2", flagDetails).stdout)
	style := objAt(t, cleared, partField, keyStyle)

	if textAt(t, style, keyBackground) != "none" {
		t.Fatal("background default was not cleared")
	}

	if _, retained := style[keyText]; retained {
		t.Fatal("text default was not cleared")
	}
}

// reportContinuation resolves the receipt using only the caller's executable and report argument.
func reportContinuation(tb testing.TB, receipt obj, path string) []string {
	tb.Helper()

	if receipt["next"] != nil {
		return exactNext(tb, receipt)
	}

	if !flagAt(tb, receipt, keyNextOmitted) {
		tb.Fatal("missing continuation without omission receipt")
	}

	reference := objAt(tb, receipt, "next_reference")
	if textAt(tb, reference, "executable_from") != invokingExecutableReference ||
		textAt(tb, reference, "report_from") != "original_argv.--report" ||
		textAt(tb, reference, "action") != "inspect_report" ||
		textAt(tb, reference, keyExpectedAttempt) != textAt(tb, receipt, keyAttemptID) {
		tb.Fatal("continuation reference lost invocation authority")
	}

	return []string{
		binary(tb), commandReport, path, expectedAttemptFlag, textAt(tb, reference, keyExpectedAttempt),
		flagView, textAt(tb, reference, "view"),
	}
}

func declarationFaults() []declarationFault {
	missing := declarationFault{
		name: "missing file", document: "{\n \"version\":1,\n \"items\":[\"missing.pdf\"]\n}",
		code: codeSourceUnreadable, pointer: firstPlanItemPointer, line: 3, column: 11, exit: 1,
	}
	bounded := missing
	bounded.name, bounded.bounded = "bounded declaration", true

	return []declarationFault{
		{
			name: "plan", document: "{\n \"version\":2,\n \"items\":[\"a.pdf\"]\n}",
			code: "plan_unsupported_version", pointer: "/version", line: 2, column: 12, exit: 2,
		},
		missing,
		{
			name:     "overflow",
			document: "{\n \"version\":1,\n \"items\":[{\"blank\":{\"size\":\"A4\",\"text\":{\"value\":\"wide\",\"x\":\"500mm\"}}}]\n}",
			code:     "text_overflow", pointer: "/items/0/blank/text/x", line: 3, column: 60, exit: 2,
		},
		bounded,
	}
}

func TestKnownDeclarationFaultsRetainExactRecoveryThroughExecutable(t *testing.T) {
	t.Parallel()

	for _, test := range declarationFaults() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := tempDir(t)
			planPath := recoveryPlan(t, dir, &test)
			checked := run(t, dir, "", commandCheck, flagPlan, planPath, flagReport, "fault.report.json")
			requireExit(t, checked, test.exit)
			compact := contractObject(t, checked.stdout)
			saved := contractObject(t, string(readFile(t, filepath.Join(dir, "fault.report.json"))))

			for _, envelope := range []obj{compact, saved} {
				requireDeclarationRecovery(t, envelope, &test, planPath)
			}

			if len([]byte(checked.stdout)) > 2048 {
				t.Fatal("compact recovery exceeds its encoded byte budget")
			}
		})
	}
}

func recoveryPlan(t *testing.T, dir string, test *declarationFault) string {
	t.Helper()

	if test.bounded {
		for range 4 {
			dir = filepath.Join(dir, strings.Repeat("q", 150))
		}

		ensure(t, os.MkdirAll(dir, 0o700))
	}

	return writeFile(t, dir, "fault.json", test.document)
}

func requireDeclarationRecovery(t *testing.T, envelope obj, test *declarationFault, path string) {
	t.Helper()

	diagnostics := listAt(t, envelope, diagnosticsView)
	if len(diagnostics) == 0 {
		t.Fatal("known declaration fault disappeared")
	}

	diagnostic := objAt(t, diagnostics[0])
	if diagnostic["code"] != test.code {
		t.Fatalf("wrong fault: %v", diagnostic)
	}

	recovery := objAt(t, diagnostic, recoveryMember)
	if recovery["action"] != "edit_input" {
		t.Fatalf("known declaration has no edit recovery: %v", diagnostic)
	}

	if envelope["kind"] == "summary" && test.bounded {
		if recovery["location"] != nil || recovery["location_from"] != "complete_report.diagnostics/0/recovery/location" {
			t.Fatalf("bounded recovery lost its exact full-report authority: %v", recovery)
		}

		return
	}

	location := objAt(t, recovery, "location")
	if location["file"] != path || location["pointer"] != test.pointer ||
		location["line"] != float64(test.line) || location["column"] != float64(test.column) {
		t.Fatalf("recovery no longer names the exact declaration: %v", location)
	}
}

func TestNormalHelpKeepsConcreteExecutableContinuationWithinPublicLimits(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"", commandBuild, commandCheck, commandReport, commandSchema, commandVersion} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			args := []string{flagHelp}
			limit := 2048

			if command != "" {
				args = []string{command, flagHelp}
				limit = 4096
			}

			res := run(t, tempDir(t), "", args...)
			requireExit(t, res, 0)

			if len(res.stdout) > limit {
				t.Fatalf("help exceeds public UTF-8 budget: %d > %d", len(res.stdout), limit)
			}

			doc := contractObject(t, res.stdout)
			next := exactNext(t, doc)

			if next[0] != binary(t) || len(next) != 3 {
				t.Fatalf("normal-size help lost invoking executable authority: %q", next)
			}

			continued := executeContinuation(t, tempDir(t), next)
			requireExit(t, continued, 0)
		})
	}
}
