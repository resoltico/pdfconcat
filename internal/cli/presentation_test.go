// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func validate(t *testing.T, value any) {
	t.Helper()
	var document, instance any
	schemaBytes, err := core.Schema(core.ResultSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(schemaBytes, &document); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &instance); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err = compiler.AddResource("result.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("result.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(instance); err != nil {
		t.Fatalf("schema: %v\n%s", err, data)
	}
}

func TestCapsCoalescingAndFullReport(t *testing.T) {
	r := newResult(t, "check")
	for i := range 25 {
		r.Diagnostics = append(r.Diagnostics, diagnostic(t, "warning", "text_overflow_allowed", strconv.Itoa(i), fmt.Sprintf("/items/%d", i)))
	}
	var items []string
	for i := range 100 {
		items = append(items, fmt.Sprintf("/items/%d", i))
	}
	warning := diagnostic(t, "warning", "source_page_labels_removed", "labels", items...)
	warning.File = "/abs/source.pdf"
	r.Diagnostics = append(r.Diagnostics, warning, warning, diagnostic(t, "error", "plan_value", "bad value", "/page/text/size"))
	full := r.Full()
	summary := cli.Summary(r)
	if len(full.Diagnostics) != 27 || len(summary.Diagnostics) != 20 || summary.DiagnosticsOmitted != 7 || summary.Diagnostics[0].Code.String() != "plan_value" {
		t.Fatalf("caps/order %+v", summary)
	}
	if len(full.Diagnostics[26].Items) != 100 || full.Diagnostics[26].ItemsOmitted != 0 {
		t.Fatal("full report lost consumer pointers")
	}
	if len(r.Diagnostics) != 28 || len(r.Diagnostics[25].Items) != 100 {
		t.Fatal("rendering mutated the source result")
	}
	validate(t, full)
	validate(t, summary)
	one := newResult(t, "check")
	one.Diagnostics = []core.Diagnostic{warning}
	capped := cli.Summary(one)
	if len(capped.Diagnostics[0].Items) != 20 || capped.Diagnostics[0].ItemsOmitted != 80 {
		t.Fatalf("consumer cap %+v", capped.Diagnostics[0])
	}
	validate(t, capped)
}

func TestJSONAndTextOutcomes(t *testing.T) {
	r := newResult(t, "build")
	r.Diagnostics = []core.Diagnostic{diagnostic(t, "error", "source_unreadable", "not readable")}
	var b bytes.Buffer
	if err := cli.Render(&b, &b, r, true); err != nil {
		t.Fatal(err)
	}
	var value core.Result
	if err := json.Unmarshal(b.Bytes(), &value); err != nil {
		t.Fatalf("not one JSON document: %v", err)
	}
	if value.Status != "failed" || value.Output != nil || value.Pages != nil {
		t.Fatalf("unknown facts invented: %+v", value)
	}
	validate(t, value)
	b.Reset()
	if err := cli.Render(&b, &b, r, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "build failed") || !strings.Contains(b.String(), "source_unreadable") {
		t.Fatal(b.String())
	}
	validate(t, core.VersionInfo{FormatVersion: 6, Command: "version", Version: "devel", Go: "go1.27.3", Platform: "linux/amd64"})
}

func TestTextDiagnosticContextAndSafeCharacters(t *testing.T) {
	path := "/abs/Rīga-Ελλάδα-Київ\n\r\t\x1b.pdf"
	message := "Cannot read " + path
	r := newResult(t, "build")
	d := diagnostic(t, "error", "source_unreadable", message, "/items/0", "/items/1")
	d.File = path
	d.Location = &core.Location{File: path, Line: 3, Column: 7, Ref: "/items/0"}
	r.Diagnostics = []core.Diagnostic{d}
	r.Output = &core.Output{Path: path}
	r.Report = &core.Report{Path: path}
	var text bytes.Buffer
	if err := cli.Render(&text, &text, r, false); err != nil {
		t.Fatal(err)
	}
	got := text.String()
	for _, want := range []string{strconv.Quote(path) + ":3:7", `[pointer "/items/0"]`, `[items "/items/1"]`, `Rīga-Ελλάδα-Київ\n\r\t\x1b.pdf`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Count(got, "\n") != 4 || strings.ContainsAny(got, "\r\t\x1b") {
		t.Fatalf("unsafe terminal output: %q", got)
	}
	var encoded bytes.Buffer
	if err := cli.Render(&encoded, &encoded, r, true); err != nil {
		t.Fatal(err)
	}
	var roundtrip core.Result
	if err := json.Unmarshal(encoded.Bytes(), &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Diagnostics[0].Message != message || roundtrip.Diagnostics[0].File != path || roundtrip.Output.Path != path || roundtrip.Report.Path != path || r.Diagnostics[0].Message != message {
		t.Fatalf("rendering changed original values: %+v", roundtrip)
	}
}

func TestTextOmissionCounts(t *testing.T) {
	r := newResult(t, "check")
	var items []string
	for i := range 23 {
		items = append(items, fmt.Sprintf("/items/%d", i))
	}
	for i := range 22 {
		r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "text_overflow", fmt.Sprintf("overflow %d", i), items...))
	}
	var text bytes.Buffer
	if err := cli.Render(&text, &text, r, false); err != nil {
		t.Fatal(err)
	}
	got := text.String()
	if strings.Count(got, "3 more; use --report") != 20 || !strings.Contains(got, "2 more diagnostics;") || strings.Contains(got, `"/items/20"`) || strings.Contains(got, "overflow 20") {
		t.Fatalf("incorrect omission counts: %s", got)
	}
}

func TestCompositionPreviewCapsRowsWithoutChangingMachineValues(t *testing.T) {
	r := newResult(t, "check")
	title := "Rīga Ελληνικά Кириллица\n\x1b"
	r.Sections = []core.Section{{Item: "/items/0", Title: title}}
	for i := range 25 {
		r.Parts = append(r.Parts, core.Part{Item: fmt.Sprintf("/items/%d", i+1), Kind: "source", Path: fmt.Sprintf("path%d\n.pdf", i), FieldsRenamed: []core.FieldRename{}})
	}
	var text bytes.Buffer
	if err := cli.Render(&text, &text, r, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "6 more preview rows; use --report") || strings.Count(text.String(), "Source ") != 19 || strings.ContainsRune(text.String(), '\x1b') || !strings.Contains(text.String(), strconv.Quote(title)) {
		t.Fatalf("unsafe or uncapped preview: %q", text.String())
	}
	var machine bytes.Buffer
	if err := cli.Render(&machine, &machine, r, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(machine.String(), `"parts"`) || strings.Contains(machine.String(), `"sections"`) {
		t.Fatal("summary exposed full composition")
	}
	var full bytes.Buffer
	if err := cli.JSON(&full, r.Full()); err != nil {
		t.Fatal(err)
	}
	var decoded core.Result
	if err := json.Unmarshal(full.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Parts) != 25 || decoded.Sections[0].Title != title || decoded.Parts[0].Path != "path0\n.pdf" {
		t.Fatal("rendering changed machine values or full report")
	}
	validate(t, r.Full())
	validate(t, cli.Summary(r))
}

func newResult(t *testing.T, command string) core.Result {
	t.Helper()
	version, err := core.Version("")
	if err != nil {
		t.Fatal(err)
	}
	return core.Result{FormatVersion: version.FormatVersion, Command: command, Status: "ok", Diagnostics: []core.Diagnostic{}}
}
func diagnostic(t *testing.T, severity core.Severity, name, message string, items ...string) core.Diagnostic {
	t.Helper()
	var code core.Code
	if err := code.UnmarshalText([]byte(name)); err != nil {
		t.Fatal(err)
	}
	return core.Diagnostic{Severity: severity, Code: code, Message: message, Items: items}
}

func TestResultStreams(t *testing.T) {
	r := newResult(t, "check")
	r.Diagnostics = []core.Diagnostic{diagnostic(t, "error", "source_unreadable", "not readable")}
	var stdout, stderr bytes.Buffer
	if err := cli.Render(&stdout, &stderr, r, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "check failed") || strings.Contains(stdout.String(), "source_unreadable") || strings.Count(stderr.String(), "\n") != 1 || !strings.Contains(stderr.String(), "source_unreadable") {
		t.Fatalf("text streams: %q / %q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := cli.Render(&stdout, &stderr, r, true); err != nil {
		t.Fatal(err)
	}
	var decoded core.Result
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal("not exactly one document", err)
	}
	if stderr.Len() != 0 || decoded.Status != "failed" {
		t.Fatalf("JSON streams: %q / %q", stdout.String(), stderr.String())
	}
}

func TestPDFDiagnosticDetailsAreSafeAndRemainInJSON(t *testing.T) {
	r := newResult(t, "check")
	d := diagnostic(t, "error", "source_invalid", "Source validation failed.")
	d.Detail = "backend\n\x1b[2Jfailure"
	d.PDFLocation = &core.PDFLocation{Object: 12, Page: 3, Path: "Pages.Contents\n"}
	r.Diagnostics = []core.Diagnostic{d}
	var stdout, stderr bytes.Buffer
	if err := cli.Render(&stdout, &stderr, r, false); err != nil {
		t.Fatal(err)
	}
	if strings.Count(stderr.String(), "\n") != 1 || strings.ContainsRune(stderr.String(), '\x1b') || !strings.Contains(stderr.String(), "PDF object 12") || !strings.Contains(stderr.String(), "PDF page 3") {
		t.Fatalf("unsafe or incomplete text: %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := cli.Render(&stdout, &stderr, r, true); err != nil {
		t.Fatal(err)
	}
	var decoded core.Result
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	validate(t, decoded)
	if len(decoded.Diagnostics) != 1 || decoded.Diagnostics[0].Detail != d.Detail || decoded.Diagnostics[0].PDFLocation == nil || *decoded.Diagnostics[0].PDFLocation != *d.PDFLocation {
		t.Fatalf("JSON lost backend evidence: %+v", decoded)
	}
}

func TestCLIAddsSyntaxToCoreRequirements(t *testing.T) {
	for _, tc := range []struct{ code, hint string }{
		{"output_required", "-o FILE"}, {"page_size_required", "--fit-to SIZE"},
		{"output_exists", "--overwrite"}, {"report_exists", "--overwrite"},
	} {
		r := newResult(t, "build")
		r.Diagnostics = []core.Diagnostic{diagnostic(t, "error", tc.code, "Core requirement.")}
		var stdout, stderr bytes.Buffer
		if err := cli.Render(&stdout, &stderr, r, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stderr.String(), tc.hint) {
			t.Fatalf("missing CLI hint: %q", stderr.String())
		}
		if r.Diagnostics[0].Message != "Core requirement." {
			t.Fatal("renderer changed core diagnostics")
		}
	}
}

func TestCapsPreserveTheOutcomeCausingDiagnostic(t *testing.T) {
	r := newResult(t, "check")
	for i := range 24 {
		r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "source_invalid", fmt.Sprintf("invalid %d", i)))
	}
	r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "report_write_failed", "Report writing failed."))
	summary := cli.Summary(r)
	if summary.Status != "failed" || len(summary.Diagnostics) != 20 || summary.DiagnosticsOmitted != 5 || summary.Diagnostics[0].Code.String() != "report_write_failed" || summary.Diagnostics[1].Message != "invalid 0" {
		t.Fatalf("causal failure or source order was hidden: %+v", summary)
	}
	r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "interrupted", "Interrupted."))
	summary = cli.Summary(r)
	if summary.Status != "interrupted" || summary.Diagnostics[0].Code.String() != "report_write_failed" || summary.Diagnostics[1].Code.String() != "interrupted" {
		t.Fatalf("cancellation outcome or publication visibility was lost: %+v", summary)
	}
}

func TestOmissionHintsReflectActualReportAvailability(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report *core.Report
		want   string
	}{
		{"not requested", nil, "use --report for full details"},
		{"saved", &core.Report{Path: "/chosen/report.json", Written: true}, "see the saved report for uncapped processing details"},
		{"unavailable", &core.Report{Path: "/chosen/report.json"}, "full report unavailable; resolve the report failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newResult(t, "check")
			r.Report = tc.report
			var items []string
			for i := range 23 {
				items = append(items, fmt.Sprintf("/items/%d", i))
			}
			for i := range 25 {
				r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "source_invalid", fmt.Sprintf("invalid %d", i), items...))
				r.Parts = append(r.Parts, core.Part{Kind: core.PartSource, Item: fmt.Sprintf("/items/%d", i)})
			}
			r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "report_write_failed", "Report writing or publication reported an error."))
			var stdout, stderr bytes.Buffer
			if err := cli.Render(&stdout, &stderr, r, false); err != nil {
				t.Fatal(err)
			}
			text := stdout.String() + stderr.String()
			if !strings.Contains(text, tc.want) || !strings.Contains(stderr.String(), "report_write_failed") {
				t.Fatalf("missing recovery context: %s", text)
			}
			if tc.report != nil && strings.Contains(text, "use --report") {
				t.Fatalf("requested report was replaced with a generic hint: %s", text)
			}
			if tc.report != nil && tc.report.Written && !strings.Contains(stdout.String(), "written: true") {
				t.Fatal("late failure erased visible report state")
			}
		})
	}
}

func TestPublicationFailuresRemainVisibleWhenPeersAlreadyDetermineOutcome(t *testing.T) {
	for _, name := range []string{"source_unreadable", "interrupted"} {
		r := newResult(t, "check")
		for i := range 24 {
			r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", name, fmt.Sprintf("earlier %d", i)))
		}
		r.Diagnostics = append(r.Diagnostics, diagnostic(t, "error", "report_write_failed", "Report publication failed."))
		full := r.Full()
		summary := cli.Summary(r)
		if summary.Diagnostics[0].Code.String() != "report_write_failed" || summary.Diagnostics[1].Message != "earlier 0" || summary.Status != full.Status || core.ExitCode(summary) != core.ExitCode(full) {
			t.Fatalf("publication or dominant outcome hidden: %+v", summary)
		}
	}
}
