// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
		if string(d.Code) == want {
			return true
		}
	}
	return false
}
func TestAdmissionRejectsBeforeIOAndEncodes(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "must-not-exist.json")
	zeroSheet := core.Sheet{}
	options := core.Options{WorkingDir: dir, Report: report}
	invalid := []core.Request{
		{}, core.NewRequest("bad", core.FromItems(core.Blank("")), options), core.NewRequest(core.Check, core.Input{}, options),
		core.NewRequest(core.Check, core.FromItems(core.Item{}), options), core.NewRequest(core.Check, core.FromItems(), options),
		core.NewRequest(core.Check, core.FromPlan(core.PlanSource{}), options), core.NewRequest(core.Check, core.FromDocument(core.PlanDocument{}), options),
		core.NewRequest(core.Check, core.FromPlan(core.LoadPlan("", "", nil)), options),
		core.NewRequest(core.Check, core.FromItems(core.Source("a", "same"), core.Source("b", "same")), options),
		core.NewRequest(core.Check, core.FromItems(core.Source("a", "same"), core.Blank("same")), options),
		core.NewRequest(core.Check, core.FromItems(core.Blank("bad\x00")), options),
		core.NewRequest(core.Check, core.FromPlan(core.BytesPlan(nil, "\xff", "")), options),
		core.NewRequest(core.Check, core.FromItems(core.Blank("")), core.Options{WorkingDir: "relative"}),
		core.NewRequest(core.Check, core.FromItems(core.Blank("")), core.Options{WorkingDir: dir, FitTo: &zeroSheet, Report: report}),
	}
	for n, request := range invalid {
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
func TestReferencesOwnershipStaticEditingAndScopes(t *testing.T) {
	dir := t.TempDir()
	if err := pdffixture.Pages("A", 1).WriteFile(filepath.Join(dir, "a.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := pdffixture.Pages("B", 2).WriteFile(filepath.Join(dir, "b.pdf")); err != nil {
		t.Fatal(err)
	}
	items := []core.Item{core.Source("a.pdf", "item:1"), core.Source("b.pdf", "")}
	req := core.NewRequest(core.Check, core.FromItems(items...), core.Options{WorkingDir: dir})
	items[0] = core.Item{}
	r := core.Execute(t.Context(), req)
	if r.Status != "ok" || len(r.Parts) != 2 || r.Parts[1].Item != "item:2" || *r.Parts[0].InputPages != 1 || *r.Parts[1].InputPages != 2 {
		t.Fatalf("references/facts: %+v", r)
	}
	data := []byte("\xef\xbb\xbf{\"version\":3, \"items\":[{\"source\":\"a.pdf\"}]}")
	original := bytes.Clone(data)
	input := core.BytesPlan(data, "editor", "")
	data[3] = '!'
	r = core.Execute(t.Context(), core.NewRequest(core.Check, core.FromPlan(input), core.Options{WorkingDir: dir}))
	if r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	d, ds := core.DecodePlan(original, "editor", dir)
	if len(ds) > 0 || !bytes.Equal(d.JSON(), original) {
		t.Fatal(ds)
	}
	returned := d.JSON()
	returned[3] = '!'
	if !bytes.Equal(d.JSON(), original) {
		t.Fatal("JSON aliases owned bytes")
	}
	if _, ds = d.ReplaceJSON([]byte("{}")); len(ds) == 0 {
		t.Fatal("invalid edit accepted")
	}
	if !bytes.Equal(d.JSON(), original) {
		t.Fatal("failed replacement mutated document")
	}
	named := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(named, original, 0600); err != nil {
		t.Fatal(err)
	}
	opened, ds := core.ReadPlan(t.Context(), core.FilePlan(named), dir)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	opened, ds = opened.ReplaceJSON([]byte(`{"version":3,"items":[{"page":{}}]}`))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	r = core.Execute(t.Context(), core.NewRequest(core.Check, core.FromDocument(opened), core.Options{WorkingDir: t.TempDir(), Report: named, Overwrite: true}))
	if !code(r, "report_is_input") || r.Report.Written {
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
func TestAcquisitionPrecedenceAndCanceledReservations(t *testing.T) {
	dir := t.TempDir()
	valid := []byte(`{"version":3,"items":[{"page":{"size":"A4"}}]}`)
	for _, tc := range []struct {
		name string
		data []byte
		err  error
		want string
	}{
		{"data plus error", valid, errors.New("broken\xff"), "plan_unreadable"},
		{"oversize plus error", make([]byte, (64<<20)+1), errors.New("broken"), "plan_too_large"},
		{"cancel plus oversize", make([]byte, (64<<20)+1), fmt.Errorf("wrapped: %w", context.Canceled), "interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			source := core.LoadPlan("host", "", func(_ context.Context, limit int64) ([]byte, error) {
				calls++
				if limit != 64<<20 {
					t.Fatal(limit)
				}
				return tc.data, tc.err
			})
			r := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromPlan(source), core.Options{WorkingDir: dir, Report: filepath.Join(t.TempDir(), "report.json")}))
			if calls != 1 || !code(r, tc.want) || r.Pages != nil || !r.Report.Written {
				t.Fatalf("%+v", r)
			}
			schemaValid(t, r)
		})
	}
	exact := append(bytes.Clone(valid), bytes.Repeat([]byte(" "), (64<<20)-len(valid))...)
	r := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromPlan(core.BytesPlan(exact, "limit", dir)), core.Options{WorkingDir: dir}))
	if r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	source := core.LoadPlan("host", "", func(context.Context, int64) ([]byte, error) { calls++; return valid, nil })
	r = core.Execute(ctx, core.NewRequest(core.Check, core.FromPlan(source), core.Options{WorkingDir: dir}))
	if calls != 0 || !code(r, "interrupted") {
		t.Fatal(r)
	}
	doc, ds := core.DecodePlan([]byte(`{"version":3,"items":[{"source":"missing.pdf"}]}`), "editor", dir)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for n, input := range []core.Input{core.FromPlan(core.FilePlan("missing.json")), core.FromItems(core.Source("missing.pdf", "row")), core.FromDocument(doc)} {
		report := filepath.Join(dir, fmt.Sprintf("cancel-%d.json", n))
		r = core.Execute(ctx, core.NewRequest(core.Check, input, core.Options{WorkingDir: dir, Report: report}))
		if !code(r, "interrupted") || !code(r, "report_identity_unverified") || r.Report.Written {
			t.Fatal(r)
		}
	}
}
func TestCoreDirectPlanParityAndConcurrentDocuments(t *testing.T) {
	dir := t.TempDir()
	tools := oracle.RequireTools(t)
	if err := pdffixture.Pages("SOURCE", 2).WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":3,"items":[{"source":"source.pdf"},{"page":{}}]}`)
	doc, ds := core.DecodePlan(data, "editor", dir)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n, input := range []core.Input{core.FromPlan(core.FilePlan(planPath)), core.FromPlan(core.BytesPlan(data, "editor", dir)), core.FromDocument(doc), core.FromDocument(doc), core.FromItems(core.Source("source.pdf", "row"), core.Blank(""))} {
		wg.Go(func() {
			path := filepath.Join(dir, fmt.Sprintf("concurrent-%d.pdf", n))
			r := core.Execute(t.Context(), core.NewRequest(core.Build, input, core.Options{WorkingDir: dir, Output: path, Report: filepath.Join(dir, fmt.Sprintf("concurrent-%d.json", n))}))
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
func TestSeparateCoreClientRichParity(t *testing.T) {
	bins := t.TempDir()
	clientPath := filepath.Join(bins, "client.exe")
	cli := filepath.Join(bins, "pdfconcat.exe")
	for _, build := range []*exec.Cmd{exec.CommandContext(t.Context(), "go", "build", "-o", clientPath, "./testdata/client"), exec.CommandContext(t.Context(), "go", "build", "-o", cli, "../../cmd/pdfconcat")} {
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("client build: %s %v", output, err)
		}
	}
	dir := t.TempDir()
	for name, fixture := range map[string]*pdffixture.Doc{"source.pdf": pdffixture.Outlined("SOURCE"), "form.pdf": pdffixture.Form("FORM")} {
		if err := fixture.WriteFile(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	font, err := os.ReadFile("../typeset/fontdata/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "custom.ttf"), font, 0600); err != nil {
		t.Fatal(err)
	}
	full := bytes.Clone(font)
	for n := range int(binary.BigEndian.Uint16(full[4:])) {
		rec := full[12+16*n:]
		if string(rec[:4]) == "OS/2" {
			offset := int(binary.BigEndian.Uint32(rec[8:]))
			binary.BigEndian.PutUint16(full[offset+8:], 0x0100)
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "full.ttf"), full, 0600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, recipe string
		pages        int
	}{
		{"rich", `{"version":3,"page":{"size":"A4"},"page_numbers":{},"items":[{"contents":{}},{"title":"Ā Ελληνικά","start_on":"odd","divider":{},"items":[{"source":"source.pdf","pages":[[2,2]]},{"dir":".","items":[{"source":"source.pdf"}]}]},{"page":{"text":{"value":"Custom Ā","font":{"file":"custom.ttf"}}}},{"page":{"text":{"value":"Full Ελληνικά","font":{"file":"full.ttf"}}}}]}`, 8},
		{"fitted", `{"version":3,"fit_to":"A4","page_numbers":{},"items":[{"source":"source.pdf"},{"source":"source.pdf","pages":[[2,2]]}]}`, 3},
		{"forms", `{"version":3,"items":[{"source":"form.pdf"},{"source":"form.pdf"}]}`, 4},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			plan := filepath.Join(dir, scenario.name+".json")
			if err := os.WriteFile(plan, []byte(scenario.recipe), 0600); err != nil {
				t.Fatal(err)
			}
			var reference core.Result
			for _, mode := range []string{"file", "bytes", "loader", "document", "cli"} {
				output, report := filepath.Join(dir, scenario.name+"-"+mode+".pdf"), filepath.Join(dir, scenario.name+"-"+mode+".json")
				cmd := exec.CommandContext(t.Context(), clientPath, mode, plan, dir, output, report)
				if mode == "cli" {
					cmd = exec.CommandContext(t.Context(), cli, "build", "--plan", plan, "--output", output, "--report", report)
				}
				raw, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %s %v", mode, raw, err)
				}
				// Compare full CLI report to the complete typed non-CLI result, never the capped stdout.
				if mode == "cli" {
					raw, err = os.ReadFile(report)
					if err != nil {
						t.Fatal(err)
					}
				}
				var r core.Result
				if err = json.Unmarshal(raw, &r); err != nil {
					t.Fatal(err)
				}
				schemaValid(t, r)
				if r.Status != "ok" || r.Pages == nil || r.Pages.Total == nil || *r.Pages.Total != int64(scenario.pages) {
					t.Fatal(r)
				}
				corpusArtifact(t, r)
				t.Run(mode+"-oracle", func(t *testing.T) {
					tools := oracle.RequireTools(t)
					document, err := oracle.Load(tools, output)
					if err != nil {
						t.Fatal(err)
					}
					if document.PageCount() != scenario.pages || document.PageCount() != int(*r.Pages.Total) {
						t.Fatalf("physical count %d want %d", document.PageCount(), scenario.pages)
					}
					if err = tools.Check(output); err != nil {
						t.Fatal(err)
					}
					if scenario.name != "forms" {
						targets, err := document.OutlineTargets()
						if err != nil || len(targets) < 2 {
							t.Fatalf("targets %v %v", targets, err)
						}
						if _, err = document.OutlineTree(); err != nil {
							t.Fatal(err)
						}
					}
					if scenario.name == "rich" {
						text, err := document.RawTexts()
						if err != nil {
							t.Fatal(err)
						}
						joined := strings.Join(text, " ")
						if !strings.Contains(joined, "Custom Ā") || !strings.Contains(joined, "Full Ελληνικά") {
							t.Fatal(joined)
						}
						fonts, err := exec.CommandContext(t.Context(), tools.PDFFonts, output).Output()
						if err != nil || !bytes.Contains(fonts, []byte("NotoSans-Regular")) {
							t.Fatalf("font resources %s %v", fonts, err)
						}
					}
					if scenario.name == "fitted" {
						info, err := exec.CommandContext(t.Context(), tools.PDFInfo, "-f", "1", "-l", "3", output).Output()
						if err != nil || !bytes.Contains(info, []byte("595.276 x 841.89")) {
							t.Fatalf("fitted physical geometry %s %v", info, err)
						}
					}

				})
				if scenario.name == "forms" {
					renames := 0
					for _, part := range r.Parts {
						renames += len(part.FieldsRenamed)
					}
					if renames == 0 {
						t.Fatal("form rename facts missing")
					}
				}
				// Output hashes/paths identify different artifacts; the rest of the complete facts must agree.
				r.Output = nil
				r.Report = nil
				if mode == "file" {
					reference = r
				} else if !reflect.DeepEqual(reference, r) {
					t.Fatalf("%s full domain facts differ", mode)
				}
			}
		})
	}
}
func TestUncappedDiagnosticsRemainComplete(t *testing.T) {
	dir := t.TempDir()
	var items []core.Item
	for n := range 30 {
		items = append(items, core.Source(fmt.Sprintf("missing-%d.pdf", n), fmt.Sprintf("row:%d", n)))
	}
	r := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromItems(items...), core.Options{WorkingDir: dir, Report: filepath.Join(t.TempDir(), "full.json")}))
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
	if _, ds := core.DecodePlan([]byte(`{"version":3,"items":[{"source":"missing.pdf"}]}`), "static", dir); len(ds) > 0 {
		t.Fatal("static opened PDF", ds)
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
		{`[{"page":{}}]`, "report_identity_unverified"},
	} {
		data := []byte(`{"version":3,"page":{"size":"A4","text":{"font":{"file":"missing.ttf"},"value":""}},"items":` + tc.items + `}`)
		report := filepath.Join(dir, "report.json")
		r := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromPlan(core.BytesPlan(data, "editor", dir)), core.Options{WorkingDir: dir, Report: report, Overwrite: true}))
		if tc.want == "" {
			if r.Status != "ok" || !r.Report.Written {
				t.Fatal(r)
			}
		} else if !code(r, tc.want) || !code(r, "font_unreadable") || r.Report.Written {
			t.Fatal(r)
		}
	}
	items := make([]core.Item, 100001)
	for n := range items {
		items[n] = core.Blank("")
	}
	r := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromItems(items...), core.Options{WorkingDir: dir}))
	if !code(r, "plan_limit") {
		t.Fatal(r.Diagnostics)
	}
	sheet, err := core.ParseSheet("Letter")
	if err != nil {
		t.Fatal(err)
	}
	options := core.Options{WorkingDir: dir, FitTo: &sheet}
	request := core.NewRequest(core.Check, core.FromItems(core.Source("source.pdf", "row")), options)
	sheet, err = core.ParseSheet("A4")
	if err != nil {
		t.Fatal(err)
	}
	r = core.Execute(t.Context(), request)
	if r.Status != "ok" || r.FitTo.Width != 612 || r.FitTo.Height != 792 {
		t.Fatal(r)
	}
}

func TestReportCannotCreateAMissingInputDirectoryPrefix(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(fmt.Sprintf("separate-report-directory=%t", separate), func(t *testing.T) {
			parent := t.TempDir()
			prefix := filepath.Join(parent, "not-yet-dir")
			input := filepath.Join(prefix, "source.pdf")
			report := prefix
			if separate {
				report = filepath.Join(t.TempDir(), "report.json")
			}
			r := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromItems(core.Source(input, "row")), core.Options{WorkingDir: parent, Report: report}))
			if !code(r, "source_unreadable") || r.Report == nil || r.Report.Written != separate {
				t.Fatal(r)
			}
			if !separate && !code(r, "report_identity_unverified") {
				t.Fatal("missing-prefix obstruction was authorized", r)
			}
			if _, err := os.Stat(prefix); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("report obstructed the future input directory: %v", err)
			}
			if separate {
				data, err := os.ReadFile(report)
				if err != nil {
					t.Fatal(err)
				}
				var full core.Result
				if err = json.Unmarshal(data, &full); err != nil || !full.Report.Written || full.Status != "failed" {
					t.Fatalf("safe separate report: %s %v", data, err)
				}
				schemaValid(t, full)
			}
		})
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
