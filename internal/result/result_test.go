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

	"github.com/resoltico/pdfconcat/internal/diag"
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
	summary := r.Summary()
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
	capped := one.Summary()
	if len(capped.Diagnostics[0].Items) != 20 || capped.Diagnostics[0].ItemsOmitted != 80 {
		t.Fatalf("consumer cap %+v", capped.Diagnostics[0])
	}
	validate(t, capped)
}

func TestJSONAndTextOutcomes(t *testing.T) {
	r := result.New("build")
	r.Diagnostics = []diag.Diagnostic{diag.New("source_unreadable", "not readable")}
	var b bytes.Buffer
	if err := result.Render(&b, r, "json"); err != nil {
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
	if err := result.Render(&b, r, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "build failed") || !strings.Contains(b.String(), "source_unreadable") {
		t.Fatal(b.String())
	}
	validate(t, result.Version{FormatVersion: 3, Command: "version", Version: "devel", Go: "go1.27.2", Platform: "linux/amd64"})
}
