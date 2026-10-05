// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

type harness struct {
	t      *testing.T
	engine *pdfengine.PDFCPU
	dir    string
	stdin  string
	stdout bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	engine, err := pdfengine.NewPDFCPU()
	if err != nil {
		t.Fatalf("NewPDFCPU() error = %v", err)
	}

	return &harness{t: t, dir: t.TempDir(), engine: engine}
}

func (h *harness) path(name string) string { return filepath.Join(h.dir, name) }

func (h *harness) pdf(name string, pages int) string {
	h.t.Helper()
	path := h.path(name)
	pdffixture.Write(h.t, path, pages, pdffixture.A4())

	return path
}

func (h *harness) run(req cli.Request) error {
	h.t.Helper()
	h.stdout.Reset()
	runner := app.New(h.engine, app.Streams{Stdin: strings.NewReader(h.stdin), Stdout: &h.stdout})

	return runner.Run(context.Background(), req)
}

func (h *harness) pages(path string) int {
	h.t.Helper()

	info, err := h.engine.Inspect(context.Background(), path)
	if err != nil {
		h.t.Fatalf("Inspect(%q) error = %v", path, err)
	}

	return info.Pages
}

func (h *harness) writePlan(name, content string) string {
	h.t.Helper()

	path := h.path(name)

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		h.t.Fatal(err)
	}

	return path
}

func direct(output string, items ...assembly.Item) cli.Request {
	return cli.Request{Output: output, Sequence: assembly.Sequence{Items: items}}
}

func blank() assembly.Item { return assembly.BlankItem(assembly.BlankStyle{}, 1) }

func TestRunAssemblesDirectSequence(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a, b := h.pdf("a.pdf", 2), h.pdf("b.pdf", 3)
	output := h.path("out.pdf")

	err := h.run(direct(output, blank(), assembly.PDFItem(a), blank(), blank(), assembly.PDFItem(b), blank()))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := h.pages(output); got != 9 {
		t.Fatalf("output pages = %d, want 9", got)
	}

	if !strings.Contains(h.stdout.String(), "9 pages") {
		t.Errorf("stdout = %q", h.stdout.String())
	}

	if entries, _ := os.ReadDir(h.dir); len(entries) != 3 {
		t.Errorf("directory has %d entries, want sources plus output only (no leftover workspace)", len(entries))
	}
}

func TestRunReadsPlanFileWithOutputAndRelativePaths(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.pdf("a.pdf", 1)
	h.pdf("b.pdf", 2)
	plan := h.writePlan("book.json", `{
		"version": 1, "output": "book.pdf",
		"blank": {"text": {"value": "Left blank"}},
		"items": ["a.pdf", {"blank": {}, "count": 2}, {"dir": ".", "items": ["b.pdf"]}]
	}`)

	err := h.run(cli.Request{PlanPath: plan})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := h.pages(h.path("book.pdf")); got != 5 {
		t.Fatalf("output pages = %d, want 5", got)
	}
}

func TestRunReadsPlanFromStandardInput(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 1)
	h.stdin = fmt.Sprintf(`{"version":1,"items":[%q,{"blank":{}}]}`, a)
	output := h.path("out.pdf")

	err := h.run(cli.Request{PlanPath: cli.StdinPlan, Output: output})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := h.pages(output); got != 2 {
		t.Fatalf("output pages = %d, want 2", got)
	}
}

func TestRunCommandLineBlankDefaultsOverridePlanDefaultsAndItemsOverrideBoth(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 1)
	plan := h.writePlan("p.json", fmt.Sprintf(`{"version":1,"output":"o.pdf",
		"blank":{"text":{"value":"from plan","size":30}},
		"items":[%q,{"blank":{}},{"blank":{"text":{"value":"from item"}}}]}`, a))

	req := cli.Request{PlanPath: plan, DryRun: true, JSON: true}

	req.Blank.Text.Value = assembly.Some("from cli")

	err := h.run(req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var summary struct {
		Parts []struct {
			Blank *struct {
				Text string `json:"text"`
			} `json:"blank"`
		} `json:"parts"`
	}

	err = json.Unmarshal(h.stdout.Bytes(), &summary)
	if err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, h.stdout.String())
	}

	if got := summary.Parts[1].Blank.Text; got != "from cli" {
		t.Errorf("blank without item style prints %q, want the CLI default", got)
	}

	if got := summary.Parts[2].Blank.Text; got != "from item" {
		t.Errorf("blank with item text prints %q, want the item text", got)
	}
}

func TestRunDryRunReportsWithoutCreatingOutput(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 2)
	output := h.path("out.pdf")
	req := direct(output, assembly.PDFItem(a), assembly.BlankItem(assembly.BlankStyle{}, 1))
	req.DryRun = true

	err := h.run(req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	text := h.stdout.String()
	for _, want := range []string{"Output: " + output, a + " (2 pages)", "blank 595x842pt", "Total: 3 pages", "Dry run"} {
		if !strings.Contains(text, want) {
			t.Errorf("dry-run text is missing %q:\n%s", want, text)
		}
	}

	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("dry run created the output: %v", statErr)
	}
}

func TestRunOutputPolicy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 1)
	output := h.pdf("existing.pdf", 5)

	req := direct(output, assembly.PDFItem(a))

	err := h.run(req)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite without --overwrite: error = %v", err)
	}

	if h.pages(output) != 5 {
		t.Fatal("existing output was modified")
	}

	req.Overwrite = true

	err = h.run(req)
	if err != nil {
		t.Fatalf("Run() with overwrite error = %v", err)
	}

	if h.pages(output) != 1 {
		t.Fatal("output was not replaced")
	}

	err = h.run(direct(a, assembly.PDFItem(a)))
	if err == nil || !strings.Contains(err.Error(), "also an input") {
		t.Fatalf("output-as-input error = %v", err)
	}

	err = h.run(direct(filepath.Join(h.dir, "no-such-dir", "o.pdf"), assembly.PDFItem(a)))
	if err == nil {
		t.Fatal("missing output directory accepted")
	}
}

func TestRunRejectsOutputThatIsThePlanFile(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 1)
	plan := h.writePlan("p.json", fmt.Sprintf(`{"version":1,"items":[%q]}`, a))

	err := h.run(cli.Request{PlanPath: plan, Output: plan, Overwrite: true})
	if err == nil || !strings.Contains(err.Error(), "plan file") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunReportsEveryFailingInputAtOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	good := h.pdf("good.pdf", 1)

	items := make([]assembly.Item, 0, 26)
	items = append(items, assembly.PDFItem(good))

	for index := range 25 {
		bad := h.path(fmt.Sprintf("bad%02d.pdf", index))

		err := os.WriteFile(bad, []byte("not a pdf"), 0o600)
		if err != nil {
			t.Fatal(err)
		}

		items = append(items, assembly.PDFItem(bad))
	}

	err := h.run(direct(h.path("out.pdf"), items...))
	if err == nil {
		t.Fatal("Run() accepted invalid PDFs")
	}

	message := err.Error()
	if !strings.Contains(message, "25 of 26 input PDFs failed") || !strings.Contains(message, "bad00.pdf") ||
		!strings.Contains(message, "bad19.pdf") || strings.Contains(message, "bad20.pdf") || !strings.Contains(message, "first 20") {
		t.Errorf("error = %v", message)
	}
}

func TestRunExplainsUnexpandedWildcards(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	err := h.run(direct(h.path("out.pdf"), assembly.PDFItem(h.path("*.pdf"))))
	if err == nil || !strings.Contains(err.Error(), "does not expand wildcards") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunRejectsBlankOptionsWithoutBlanks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := direct(h.path("out.pdf"), assembly.PDFItem(h.pdf("a.pdf", 1)))
	req.Blank.Text.Value = assembly.Some("unused")

	err := h.run(req)
	if _, isUsage := errors.AsType[*cli.UsageError](err); !isUsage {
		t.Fatalf("Run() error = %v, want usage error", err)
	}
}

func TestRunRequiresAnOutput(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 1)
	plan := h.writePlan("p.json", fmt.Sprintf(`{"version":1,"items":[%q]}`, a))

	err := h.run(cli.Request{PlanPath: plan})
	if _, isUsage := errors.AsType[*cli.UsageError](err); !isUsage {
		t.Fatalf("Run() error = %v, want usage error", err)
	}
}

func TestRunFailsOnUnrenderableBlankTextBeforeReadingPDFs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	missing := h.path("missing.pdf")
	style := assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Some("Ā")}}

	err := h.run(direct(h.path("out.pdf"), assembly.PDFItem(missing), assembly.BlankItem(style, 1)))
	if err == nil || !strings.Contains(err.Error(), "U+0100") || strings.Contains(err.Error(), "missing.pdf") {
		t.Fatalf("Run() error = %v, want the character error alone", err)
	}
}

func TestRunReportsPlanErrorsWithTheirLocation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	plan := h.writePlan("p.json", `{"version":1,"items":["a.pdf",{"blank":{"text":{"color":"red"}}}]}`)

	err := h.run(cli.Request{PlanPath: plan, Output: h.path("o.pdf")})
	if err == nil || !strings.Contains(err.Error(), "items[1].blank.text.color") {
		t.Fatalf("Run() error = %v", err)
	}

	err = h.run(cli.Request{PlanPath: h.path("missing.json"), Output: h.path("o.pdf")})
	if err == nil {
		t.Fatal("missing plan accepted")
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	a := h.pdf("a.pdf", 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := app.New(h.engine, app.Streams{Stdout: &h.stdout})

	err := runner.Run(ctx, direct(h.path("out.pdf"), assembly.PDFItem(a)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context cancellation", err)
	}

	if _, statErr := os.Stat(h.path("out.pdf")); statErr == nil {
		t.Fatal("cancelled run published an output")
	}
}

func TestRunShowsProgressOnlyWhenRequested(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	items := make([]assembly.Item, 0, 5)
	for index := range 5 {
		items = append(items, assembly.PDFItem(h.pdf(fmt.Sprintf("d%d.pdf", index), 1)))
	}

	var progress bytes.Buffer

	runner := app.New(h.engine, app.Streams{Stdout: &h.stdout, Progress: &progress})

	err := runner.Run(context.Background(), direct(h.path("out.pdf"), items...))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(progress.String(), "Validating PDFs 5/5") {
		t.Errorf("progress = %q", progress.String())
	}
}
