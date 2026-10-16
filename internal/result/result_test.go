// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package result_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/result"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func validate(t *testing.T, value any) {
	t.Helper()
	var document, instance any
	if err := json.Unmarshal(result.Schema, &document); err != nil {
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
	r := result.New("check")
	for i := range 25 {
		r.Diagnostics = append(r.Diagnostics, diag.New("text_overflow_allowed", strconv.Itoa(i), fmt.Sprintf("/items/%d", i)))
	}
	var items []string
	for i := range 100 {
		items = append(items, fmt.Sprintf("/items/%d", i))
	}
	warning := diag.New("source_page_labels_removed", "labels", items...)
	warning.File = "/abs/source.pdf"
	r.Diagnostics = append(r.Diagnostics, warning, warning, diag.New("plan_value", "bad value", "/page/text/size"))
	full := r.Full()
	summary := cli.Summary(r)
	if len(full.Diagnostics) != 27 || len(summary.Diagnostics) != 20 || summary.DiagnosticsOmitted != 7 || summary.Diagnostics[0].Code != "plan_value" {
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
	one := result.New("check")
	one.Diagnostics = []diag.Diagnostic{warning}
	capped := cli.Summary(one)
	if len(capped.Diagnostics[0].Items) != 20 || capped.Diagnostics[0].ItemsOmitted != 80 {
		t.Fatalf("consumer cap %+v", capped.Diagnostics[0])
	}
	validate(t, capped)
}

func TestJSONAndTextOutcomes(t *testing.T) {
	r := result.New("build")
	r.Diagnostics = []diag.Diagnostic{diag.New("source_unreadable", "not readable")}
	var b bytes.Buffer
	if err := cli.Render(&b, r, "json"); err != nil {
		t.Fatal(err)
	}
	var value result.Result
	if err := json.Unmarshal(b.Bytes(), &value); err != nil {
		t.Fatalf("not one JSON document: %v", err)
	}
	if value.Status != "failed" || value.Output != nil || value.Pages != nil {
		t.Fatalf("unknown facts invented: %+v", value)
	}
	validate(t, value)
	b.Reset()
	if err := cli.Render(&b, r, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "build failed") || !strings.Contains(b.String(), "source_unreadable") {
		t.Fatal(b.String())
	}
	validate(t, result.Version{FormatVersion: 4, Command: "version", Version: "devel", Go: "go1.27.2", Platform: "linux/amd64"})
}

func TestTextDiagnosticContextAndSafeCharacters(t *testing.T) {
	path := "/abs/Rīga-Ελλάδα-Київ\n\r\t\x1b.pdf"
	message := "Cannot read " + path
	r := result.New("build")
	d := diag.New("source_unreadable", message, "/items/0", "/items/1")
	d.File = path
	d.Location = &diag.Location{File: path, Line: 3, Column: 7, Pointer: "/items/0"}
	r.Diagnostics = []diag.Diagnostic{d}
	r.Output = &result.Output{Path: path}
	r.Report = &result.Report{Path: path}
	var text bytes.Buffer
	if err := cli.Render(&text, r, "text"); err != nil {
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
	if err := cli.Render(&encoded, r, "json"); err != nil {
		t.Fatal(err)
	}
	var roundtrip result.Result
	if err := json.Unmarshal(encoded.Bytes(), &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Diagnostics[0].Message != message || roundtrip.Diagnostics[0].File != path || roundtrip.Output.Path != path || roundtrip.Report.Path != path || r.Diagnostics[0].Message != message {
		t.Fatalf("rendering changed original values: %+v", roundtrip)
	}
}

func TestTextOmissionCounts(t *testing.T) {
	r := result.New("check")
	var items []string
	for i := range 23 {
		items = append(items, fmt.Sprintf("/items/%d", i))
	}
	for i := range 22 {
		r.Diagnostics = append(r.Diagnostics, diag.New("text_overflow", fmt.Sprintf("overflow %d", i), items...))
	}
	var text bytes.Buffer
	if err := cli.Render(&text, r, "text"); err != nil {
		t.Fatal(err)
	}
	got := text.String()
	if strings.Count(got, "3 more; use --report") != 20 || !strings.Contains(got, "2 more diagnostics;") || strings.Contains(got, `"/items/20"`) || strings.Contains(got, "overflow 20") {
		t.Fatalf("incorrect omission counts: %s", got)
	}
}

func TestCompositionPreviewCapsRowsWithoutChangingMachineValues(t *testing.T) {
	r := result.New("check")
	title := "Rīga Ελληνικά Кириллица\n\x1b"
	r.Sections = []result.Section{{Item: "/items/0", Title: title}}
	for i := range 25 {
		r.Parts = append(r.Parts, result.Part{Item: fmt.Sprintf("/items/%d", i+1), Kind: "source", Path: fmt.Sprintf("path%d\n.pdf", i), FieldsRenamed: []job.FieldRename{}})
	}
	var text bytes.Buffer
	if err := cli.Render(&text, r, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "6 more preview rows; use --report") || strings.Count(text.String(), "Source ") != 19 || strings.ContainsRune(text.String(), '\x1b') || !strings.Contains(text.String(), strconv.Quote(title)) {
		t.Fatalf("unsafe or uncapped preview: %q", text.String())
	}
	var machine bytes.Buffer
	if err := cli.Render(&machine, r, "json"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(machine.String(), `"parts"`) || strings.Contains(machine.String(), `"sections"`) {
		t.Fatal("summary exposed full composition")
	}
	var full bytes.Buffer
	if err := result.JSON(&full, r.Full()); err != nil {
		t.Fatal(err)
	}
	var decoded result.Result
	if err := json.Unmarshal(full.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Parts) != 25 || decoded.Sections[0].Title != title || decoded.Parts[0].Path != "path0\n.pdf" {
		t.Fatal("rendering changed machine values or full report")
	}
	validate(t, r.Full())
	validate(t, cli.Summary(r))
}
