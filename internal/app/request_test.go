// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

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

	"github.com/resoltico/pdfconcat/internal/cli"
	exchange "github.com/resoltico/pdfconcat/internal/wire"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func schemaValid(t *testing.T, value any) {
	r := asWire(value)
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var schema, instance any
	raw, err := app.Schema(app.ResultSchema)
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
func code(value any, want string) bool {
	var ds []app.Diagnostic
	switch r := value.(type) {
	case app.Result:
		ds = r.Diagnostics
	case exchange.Result:
		ds = r.Diagnostics
	default:
		panic("unexpected result test value")
	}
	for _, d := range ds {
		if d.Code.String() == want {
			return true
		}
	}
	return false
}

func TestAdmissionRejectsBeforeIOAndEncodes(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "must-not-exist.json")
	zero := packet.FitPolicy{}
	invalid := []app.Request{
		{},
		{Operation: "bad", Input: app.Items{app.BlankItem{Ref: "blank"}}},
		{Operation: app.Check},
		{Operation: app.Check, Input: app.Items{nil}},
		{Operation: app.Check, Input: app.Items{}},
		{Operation: app.Check, Input: app.PlanFile{}},
		{Operation: app.Check, Input: app.Items{app.SourceItem{Ref: "same", Path: "a"}, app.SourceItem{Ref: "same", Path: "b"}}},
		{Operation: app.Check, Input: app.Items{app.SourceItem{Ref: "same", Path: "a"}, app.BlankItem{Ref: "same"}}},
		{Operation: app.Check, Input: app.Items{app.SourceItem{Ref: "", Path: "a"}}},
		{Operation: app.Check, Input: app.Items{app.BlankItem{Ref: ""}}},
		{Operation: app.Check, Input: app.Items{app.BlankItem{Ref: "bad\x00"}}},
		{Operation: app.Check, Input: app.PlanBytes{Label: "\xff"}},
		{Operation: app.Check, Input: app.Items{app.BlankItem{Ref: "blank"}}, WorkingDir: "relative"},
		{Operation: app.Check, Input: app.Items{app.BlankItem{Ref: "blank"}}, Fitting: &zero},
	}
	for n, request := range invalid {
		if request.WorkingDir == "" {
			request.WorkingDir = dir
		}
		request.Report = report
		r := app.Execute(t.Context(), request)
		if !code(r, "request_invalid") || cli.ExitCode(r.Status) != 2 {
			t.Fatalf("%d: %+v", n, r)
		}
		schemaValid(t, r)
	}
	if _, err := os.Stat(report); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid request performed report I/O: %v", err)
	}
	for _, r := range []app.Result{cli.InvocationFailure("bad", cli.InvalidValue, "x", ""), cli.InvocationFailure("", cli.InvocationFaultKind("bad"), "x", ""), cli.InvocationFailure(app.Build, cli.InvalidValue, "\xff", ""), cli.InvocationFailure("", cli.UnknownCommand, "unknown", "argv:0")} {
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
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.Items{app.SourceItem{Path: "a.pdf", Ref: "first"}, app.SourceItem{Path: "b.pdf", Ref: "second"}}, WorkingDir: dir})
	if r.Status != "ok" || len(r.Parts) != 2 || r.Parts[0].Item != "first" || r.Parts[1].Item != "second" || *r.Parts[0].InputPages != 1 || *r.Parts[1].InputPages != 2 {
		t.Fatal(r)
	}
	schema, _ := app.Schema(app.PlanSchema)
	schema[0] = '!'
	fresh, _ := app.Schema(app.PlanSchema)
	if fresh[0] == '!' {
		t.Fatal("schema alias")
	}
	if _, err := app.Schema("unknown"); err == nil {
		t.Fatal("unknown schema")
	}
	if _, err := cli.Version(cli.BuildMetadata{}, "\xff"); err == nil {
		t.Fatal("invalid metadata")
	}
}

func TestPlanByteLimitAndCanceledPhysicalInputs(t *testing.T) {
	dir := t.TempDir()
	valid := []byte(`{"version":9,"items":[{"page":{"size":"A4"}}]}`)
	exact := append(bytes.Clone(valid), bytes.Repeat([]byte(" "), (64<<20)-len(valid))...)
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.PlanBytes{Data: exact, Label: "limit", BaseDir: dir}, WorkingDir: dir})
	if r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	oversized := make([]byte, app.MaxPlanBytes+1)
	r = app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.PlanBytes{Data: oversized, Label: "limit"}, WorkingDir: dir})
	if !code(r, "plan_too_large") {
		t.Fatal(r.Diagnostics)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for n, input := range []app.Input{app.PlanFile{Path: "missing.json"}, app.Items{app.SourceItem{Path: "missing.pdf", Ref: "row"}}} {
		r = app.Execute(ctx, app.Request{InspectionWorkers: 1, Operation: app.Check, Input: input, WorkingDir: dir, Report: filepath.Join(dir, fmt.Sprintf("cancel-%d.json", n))})
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
	data := []byte(`{"version":9,"items":[{"source":"source.pdf"},{"page":{}}]}`)
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n, input := range []app.Input{app.PlanFile{Path: planPath}, app.PlanBytes{Data: data, Label: "editor", BaseDir: dir}, app.Items{app.SourceItem{Path: "source.pdf", Ref: "row"}, app.BlankItem{Ref: "blank"}}} {
		wg.Go(func() {
			path := filepath.Join(dir, fmt.Sprintf("concurrent-%d.pdf", n))
			r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, Input: input, WorkingDir: dir, Output: path, Report: filepath.Join(dir, fmt.Sprintf("concurrent-%d.json", n))})
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
	var items []app.Item
	for n := range 30 {
		items = append(items, app.SourceItem{Path: fmt.Sprintf("missing-%d.pdf", n), Ref: fmt.Sprintf("row:%d", n)})
	}
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.Items(items), WorkingDir: dir, Report: filepath.Join(t.TempDir(), "full.json")})
	if len(r.Diagnostics) != 30 || len(r.Parts) != 30 || !r.Report.Written || cli.ExitCode(r.Status) != 2 {
		t.Fatal(r)
	}
	data, err := os.ReadFile(r.Report.Path)
	if err != nil {
		t.Fatal(err)
	}
	var full exchange.Result
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Diagnostics) != 30 || full.DiagnosticsOmitted != 0 {
		t.Fatal("report capped")
	}

	// A late higher-priority error must determine exit even beyond a presentation cap.
	r.Diagnostics = append(slices.Repeat([]app.Diagnostic{cli.InvocationFailure(app.Check, cli.InvalidValue, "invalid", "").Diagnostics[0]}, 25), cli.InvocationFailure(app.Check, cli.PlanUnreadable, "reader failed", "").Diagnostics[0])
	r = r.Full()
	if cli.ExitCode(r.Status) != 1 {
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
		{`[{"page":{}}]`, "font_input_invalid"},
	} {
		data := []byte(`{"version":9,"page_defaults":{"size":"A4","text":{"font":{"file":"missing.ttf"},"value":""}},"items":` + tc.items + `}`)
		report := filepath.Join(dir, "report.json")
		r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.PlanBytes{Data: data, Label: "editor", BaseDir: dir}, WorkingDir: dir, Report: report, Overwrite: true})
		if tc.want == "" {
			if r.Status != "ok" || !r.Report.Written {
				t.Fatal(r)
			}
		} else if !code(r, tc.want) || !r.Report.Written {
			t.Fatal(r)
		}
	}
	items := make([]app.Item, 100001)
	for n := range items {
		items[n] = app.BlankItem{Ref: fmt.Sprintf("blank:%d", n)}
	}
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.Items(items), WorkingDir: dir})
	if !code(r, "plan_limit") {
		t.Fatal(r.Diagnostics)
	}

}

func TestInvocationFailureOmitsEmptyConsumersAndKeepsMetadataStrict(t *testing.T) {
	r := cli.InvocationFailure(app.Check, cli.WorkingDirectoryUnavailable, "Cannot read cwd.", "")
	if cli.ExitCode(r.Status) != 1 || len(r.Diagnostics) != 1 || len(r.Diagnostics[0].Items) != 0 {
		t.Fatal(r)
	}
	schemaValid(t, r)
	r = cli.InvocationFailure(app.Build, cli.MissingValue, "Missing option value.", "argv:3")
	if !slices.Equal(r.Diagnostics[0].Items, []string{"argv:3"}) || cli.ExitCode(r.Status) != 2 {
		t.Fatal(r)
	}
	schemaValid(t, r)
	for _, invalid := range []string{"bad\xff", "bad\x00context"} {
		for _, r := range []app.Result{cli.InvocationFailure(app.Check, cli.WorkingDirectoryUnavailable, invalid, ""), cli.InvocationFailure(app.Check, cli.InvalidValue, "bad value", invalid)} {
			if !code(r, "request_invalid") || cli.ExitCode(r.Status) != 2 {
				t.Fatal(r)
			}
			schemaValid(t, r)
		}
	}
}

func TestSourceInspectionDiagnosticsFollowFirstUseWithinOutcomePriority(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.pdf"), []byte("not a PDF"), 0600); err != nil {
		t.Fatal(err)
	}
	request := app.Request{InspectionWorkers: 1, Operation: app.Check, WorkingDir: dir, Input: app.Items{
		app.SourceItem{Ref: "first", Path: "missing-first.pdf"},
		app.SourceItem{Ref: "broken", Path: "broken.pdf"},
		app.SourceItem{Ref: "repeat", Path: "missing-first.pdf"},
		app.SourceItem{Ref: "last", Path: "missing-last.pdf"},
	}}
	result := app.Execute(t.Context(), request)
	if len(result.Diagnostics) != 3 {
		t.Fatal(result.Diagnostics)
	}
	for i, want := range []struct {
		file, code string
		items      []string
	}{
		{"missing-first.pdf", "source_input_invalid", []string{"first", "repeat"}},
		{"broken.pdf", "source_not_pdf", []string{"broken"}},
		{"missing-last.pdf", "source_input_invalid", []string{"last"}},
	} {
		got := result.Diagnostics[i]
		if filepath.Base(got.File) != want.file || got.Code.String() != want.code || !slices.Equal(got.Items, want.items) {
			t.Fatalf("finding %d: %+v", i, got)
		}
	}
}

func asWire(value any) exchange.Result {
	switch r := value.(type) {
	case app.Result:
		return exchange.Project(r)
	case exchange.Result:
		return r
	default:
		panic("unexpected result test value")
	}
}
