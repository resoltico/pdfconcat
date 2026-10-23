// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func schemaValid(t *testing.T, r core.Result) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var schema, instance any
	raw, err := core.Schema(core.ResultSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &instance); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err = compiler.AddResource("urn:result", schema); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("urn:result")
	if err != nil {
		t.Fatal(err)
	}
	if err = compiled.Validate(instance); err != nil {
		t.Fatal(err)
	}
}
func code(r core.Result, want string) bool {
	for _, d := range r.Diagnostics {
		if d.Code.String() == want {
			return true
		}
	}
	return false
}

func TestAdmissionRejectsBeforeIOAndEncodes(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "must-not-exist.json")
	zero := core.Sheet{}
	invalid := []core.Request{
		{},
		{Operation: "bad", Input: core.Items{core.BlankItem{Ref: "blank"}}},
		{Operation: core.Check},
		{Operation: core.Check, Input: core.Items{nil}},
		{Operation: core.Check, Input: core.Items{}},
		{Operation: core.Check, Input: core.PlanFile{}},
		{Operation: core.Check, Input: core.Items{core.SourceItem{Ref: "same", Path: "a"}, core.SourceItem{Ref: "same", Path: "b"}}},
		{Operation: core.Check, Input: core.Items{core.SourceItem{Ref: "same", Path: "a"}, core.BlankItem{Ref: "same"}}},
		{Operation: core.Check, Input: core.Items{core.SourceItem{Ref: "", Path: "a"}}},
		{Operation: core.Check, Input: core.Items{core.BlankItem{Ref: ""}}},
		{Operation: core.Check, Input: core.Items{core.BlankItem{Ref: "bad\x00"}}},
		{Operation: core.Check, Input: core.PlanBytes{Label: "\xff"}},
		{Operation: core.Check, Input: core.Items{core.BlankItem{Ref: "blank"}}, WorkingDir: "relative"},
		{Operation: core.Check, Input: core.Items{core.BlankItem{Ref: "blank"}}, FitTo: &zero},
	}
	for n, request := range invalid {
		if request.WorkingDir == "" {
			request.WorkingDir = dir
		}
		request.Report = report
		r := core.Execute(t.Context(), request)
		if !code(r, "request_invalid") || core.ExitCode(r) != 2 {
			t.Fatalf("%d: %+v", n, r)
		}
		schemaValid(t, r)
	}
	if _, err := os.Stat(report); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid request performed report I/O: %v", err)
	}
	for _, r := range []core.Result{core.InvocationFailure("bad", core.InvalidValue, "x", ""), core.InvocationFailure("", core.InvocationFaultKind("bad"), "x", ""), core.InvocationFailure(core.Build, core.InvalidValue, "\xff", ""), core.InvocationFailure("", core.UnknownCommand, "unknown", "argv:0")} {
		schemaValid(t, r)
	}
}

func TestDirectReferencesAndQueries(t *testing.T) {
	dir := t.TempDir()
	if err := pdffixture.Pages("A", 1).WriteFile(filepath.Join(dir, "a.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := pdffixture.Pages("B", 2).WriteFile(filepath.Join(dir, "b.pdf")); err != nil {
		t.Fatal(err)
	}
	r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.Items{core.SourceItem{Path: "a.pdf", Ref: "first"}, core.SourceItem{Path: "b.pdf", Ref: "second"}}, WorkingDir: dir})
	if r.Status != "ok" || len(r.Parts) != 2 || r.Parts[0].Item != "first" || r.Parts[1].Item != "second" || *r.Parts[0].InputPages != 1 || *r.Parts[1].InputPages != 2 {
		t.Fatal(r)
	}
	schema, _ := core.Schema(core.PlanSchema)
	schema[0] = '!'
	fresh, _ := core.Schema(core.PlanSchema)
	if fresh[0] == '!' {
		t.Fatal("schema alias")
	}
	if _, err := core.Schema("unknown"); err == nil {
		t.Fatal("unknown schema")
	}
	if _, err := core.Version("\xff"); err == nil {
		t.Fatal("invalid metadata")
	}
}

func TestPlanByteLimitAndCanceledPhysicalInputs(t *testing.T) {
	dir := t.TempDir()
	valid := []byte(`{"version":4,"items":[{"page":{"size":"A4"}}]}`)
	exact := append(bytes.Clone(valid), bytes.Repeat([]byte(" "), (64<<20)-len(valid))...)
	r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanBytes{Data: exact, Label: "limit", BaseDir: dir}, WorkingDir: dir})
	if r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	oversized := make([]byte, core.MaxPlanBytes+1)
	r = core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanBytes{Data: oversized, Label: "limit"}, WorkingDir: dir})
	if !code(r, "plan_too_large") {
		t.Fatal(r.Diagnostics)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for n, input := range []core.Input{core.PlanFile{Path: "missing.json"}, core.Items{core.SourceItem{Path: "missing.pdf", Ref: "row"}}} {
		r = core.Execute(ctx, core.Request{Operation: core.Check, Input: input, WorkingDir: dir, Report: filepath.Join(dir, fmt.Sprintf("cancel-%d.json", n))})
		if !code(r, "interrupted") || !r.Report.Written {
			t.Fatal(r)
		}
	}
}

func TestConcurrentFileBytesAndDirectRequests(t *testing.T) {
	dir := t.TempDir()
	tools := oracle.RequireTools(t)
	if err := pdffixture.Pages("SOURCE", 2).WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":4,"items":[{"source":"source.pdf"},{"page":{}}]}`)
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n, input := range []core.Input{core.PlanFile{Path: planPath}, core.PlanBytes{Data: data, Label: "editor", BaseDir: dir}, core.Items{core.SourceItem{Path: "source.pdf", Ref: "row"}, core.BlankItem{Ref: "blank"}}} {
		wg.Go(func() {
			path := filepath.Join(dir, fmt.Sprintf("concurrent-%d.pdf", n))
			r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: input, WorkingDir: dir, Output: path, Report: filepath.Join(dir, fmt.Sprintf("concurrent-%d.json", n))})
			if r.Status != "ok" || !r.Output.Published {
				t.Errorf("%+v", r)
				return
			}
			document, err := oracle.Load(tools, path)
			if err != nil {
				t.Error(err)
				return
			}
			if document.PageCount() != 3 {
				t.Error(document.PageCount())
			}
		})
	}
	wg.Wait()
}

func TestUncappedDiagnosticsRemainComplete(t *testing.T) {
	dir := t.TempDir()
	var items []core.Item
	for n := range 30 {
		items = append(items, core.SourceItem{Path: fmt.Sprintf("missing-%d.pdf", n), Ref: fmt.Sprintf("row:%d", n)})
	}
	r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.Items(items), WorkingDir: dir, Report: filepath.Join(t.TempDir(), "full.json")})
	if len(r.Diagnostics) != 30 || len(r.Parts) != 30 || !r.Report.Written || core.ExitCode(r) != 1 {
		t.Fatal(r)
	}
	data, err := os.ReadFile(r.Report.Path)
	if err != nil {
		t.Fatal(err)
	}
	var full core.Result
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Diagnostics) != 30 || full.DiagnosticsOmitted != 0 {
		t.Fatal("report capped")
	}

	// A late higher-priority error must determine exit even beyond a presentation cap.
	r.Diagnostics = append(slices.Repeat([]core.Diagnostic{core.InvocationFailure(core.Check, core.InvalidValue, "invalid", "").Diagnostics[0]}, 25), r.Diagnostics[0])
	if core.ExitCode(r) != 1 {
		t.Fatal("exit used cap")
	}
	if strings.Contains(string(data), "request_invalid") {
		t.Fatal("unexpected envelope refusal")
	}
}

func TestEffectiveFontReservationsAndDirectBudgets(t *testing.T) {
	dir := t.TempDir()
	if err := pdffixture.Plain("SOURCE").WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		items string
		want  string
	}{
		{`[{"source":"source.pdf"}]`, ""},
		{`[{"page":{}}]`, "font_unreadable"},
	} {
		data := []byte(`{"version":4,"page_defaults":{"size":"A4","text":{"font":{"file":"missing.ttf"},"value":""}},"items":` + tc.items + `}`)
		report := filepath.Join(dir, "report.json")
		r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanBytes{Data: data, Label: "editor", BaseDir: dir}, WorkingDir: dir, Report: report, Overwrite: true})
		if tc.want == "" {
			if r.Status != "ok" || !r.Report.Written {
				t.Fatal(r)
			}
		} else if !code(r, tc.want) || !r.Report.Written {
			t.Fatal(r)
		}
	}
	items := make([]core.Item, 100001)
	for n := range items {
		items[n] = core.BlankItem{Ref: fmt.Sprintf("blank:%d", n)}
	}
	r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.Items(items), WorkingDir: dir})
	if !code(r, "plan_limit") {
		t.Fatal(r.Diagnostics)
	}

}

func TestInvocationFailureOmitsEmptyConsumersAndKeepsMetadataStrict(t *testing.T) {
	r := core.InvocationFailure(core.Check, core.WorkingDirectoryUnavailable, "Cannot read cwd.", "")
	if core.ExitCode(r) != 1 || len(r.Diagnostics) != 1 || len(r.Diagnostics[0].Items) != 0 {
		t.Fatal(r)
	}
	schemaValid(t, r)
	r = core.InvocationFailure(core.Build, core.MissingValue, "Missing option value.", "argv:3")
	if !slices.Equal(r.Diagnostics[0].Items, []string{"argv:3"}) || core.ExitCode(r) != 2 {
		t.Fatal(r)
	}
	schemaValid(t, r)
	for _, invalid := range []string{"bad\xff", "bad\x00context"} {
		for _, r := range []core.Result{core.InvocationFailure(core.Check, core.WorkingDirectoryUnavailable, invalid, ""), core.InvocationFailure(core.Check, core.InvalidValue, "bad value", invalid)} {
			if !code(r, "request_invalid") || core.ExitCode(r) != 2 {
				t.Fatal(r)
			}
			schemaValid(t, r)
		}
	}
}

func TestSourceInspectionDiagnosticsFollowFirstUse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.pdf"), []byte("not a PDF"), 0600); err != nil {
		t.Fatal(err)
	}
	request := core.Request{Operation: core.Check, WorkingDir: dir, Input: core.Items{
		core.SourceItem{Ref: "first", Path: "missing-first.pdf"},
		core.SourceItem{Ref: "broken", Path: "broken.pdf"},
		core.SourceItem{Ref: "repeat", Path: "missing-first.pdf"},
		core.SourceItem{Ref: "last", Path: "missing-last.pdf"},
	}}
	result := core.Execute(t.Context(), request)
	if len(result.Diagnostics) != 3 {
		t.Fatal(result.Diagnostics)
	}
	for i, want := range []struct {
		file, code string
		items      []string
	}{
		{"missing-first.pdf", "source_unreadable", []string{"first", "repeat"}},
		{"broken.pdf", "source_invalid", []string{"broken"}},
		{"missing-last.pdf", "source_unreadable", []string{"last"}},
	} {
		got := result.Diagnostics[i]
		if filepath.Base(got.File) != want.file || got.Code.String() != want.code || !slices.Equal(got.Items, want.items) {
			t.Fatalf("finding %d: %+v", i, got)
		}
	}
}
