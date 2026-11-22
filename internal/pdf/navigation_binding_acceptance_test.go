// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func navigationBindingFixture(edge string) *pdffixture.Doc {
	const array = "[3 0 R /XYZ 100 300 0]"
	const gotoAction = "<< /S /GoTo /D " + array + " >>"
	const leaf = "<< /Limits [(target) (target)] /Names [(target) " + array + "] >>"
	doc := destinationOperandFixture("link", "/XYZ 100 300 0", array)
	link := func(entries string) {
		doc.Objs[5] = []byte("<< /Type /Annot /Subtype /Link /Rect [10 20 30 40] " + entries + " >>")
	}
	names := func(root string) {
		insertEntry(doc, 1, "/Names << /Dests "+root+" >>")
		link("/Dest (target)")
	}
	legacy := func(value string) {
		insertEntry(doc, 1, "/Dests "+value)
		link("/Dest /target")
	}
	switch edge {
	case "link-array":
		link("/Dest 7 1 R")
	case "link-symbolic":
		names(leaf)
		link("/Dest 7 1 R")
		doc.Objs[6] = []byte("(target)")
	case "goto-array":
		link("/A << /S /GoTo /D 7 1 R >>")
	case "goto-symbolic":
		names(leaf)
		link("/A << /S /GoTo /D 7 1 R >>")
		doc.Objs[6] = []byte("(target)")
	case "action-owner":
		link("/A 7 1 R")
		doc.Objs[6] = []byte(gotoAction)
	case "action-kind":
		link("/A << /S 7 1 R /D " + array + " >>")
		doc.Objs[6] = []byte("/GoTo")
	case "next-owner", "next-array":
		next := "7 1 R"
		if edge == "next-array" {
			next = "[7 1 R]"
		}
		link("/A << /S /URI /URI (https://example.invalid/) /Next " + next + " >>")
		doc.Objs[6] = []byte(gotoAction)
	case "additional-actions":
		insertEntry(doc, 3, "/AA 7 1 R")
		link("")
		doc.Objs[6] = []byte("<< /O " + gotoAction + " >>")
	case "additional-action-event":
		insertEntry(doc, 3, "/AA << /O 7 1 R >>")
		link("")
		doc.Objs[6] = []byte(gotoAction)
	case "outline-array":
		doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("/Annots [6 0 R]"), nil)
		insertEntry(doc, 1, "/Outlines 10 0 R")
		doc.Objs = append(doc.Objs, []byte("<< /Type /Outlines /First 11 0 R /Last 11 0 R /Count 1 >>"),
			[]byte("<< /Parent 10 0 R /Title (Target) /Dest 7 1 R >>"))
	case "catalog-names":
		insertEntry(doc, 1, "/Names 7 1 R")
		link("/Dest (target)")
		doc.Objs[6] = []byte("<< /Dests " + leaf + " >>")
	case "name-tree-root":
		names("7 1 R")
		doc.Objs[6] = []byte(leaf)
	case "name-tree-kids":
		names("<< /Kids 7 1 R >>")
		doc.Objs[6] = []byte("[10 0 R]")
		doc.Objs = append(doc.Objs, []byte(leaf))
	case "name-tree-child":
		names("<< /Kids [7 1 R] >>")
		doc.Objs[6] = []byte(leaf)
	case "name-tree-entries":
		names("<< /Names 7 1 R >>")
		doc.Objs[6] = []byte("[(target) " + array + "]")
	case "name-tree-key":
		names("<< /Names [7 1 R " + array + "] >>")
		doc.Objs[6] = []byte("(target)")
	case "name-tree-value":
		names("<< /Names [(target) 7 1 R] >>")
	case "name-tree-wrapper":
		names("<< /Names [(target) << /D 7 1 R >>] >>")
	case "legacy-root":
		legacy("7 1 R")
		doc.Objs[6] = []byte("<< /target " + array + " >>")
	case "legacy-value":
		legacy("<< /target 7 1 R >>")
	case "legacy-wrapper":
		legacy("<< /target << /D 7 1 R >> >>")
	}
	return doc
}

func TestNavigationBindingsRequireFullReferenceIdentity(t *testing.T) {
	for _, edge := range []string{
		"link-array", "link-symbolic", "goto-array", "goto-symbolic", "action-owner", "action-kind",
		"next-owner", "next-array", "additional-actions", "additional-action-event", "outline-array",
		"catalog-names", "name-tree-root", "name-tree-kids", "name-tree-child", "name-tree-entries",
		"name-tree-key", "name-tree-value", "name-tree-wrapper", "legacy-root", "legacy-value", "legacy-wrapper",
	} {
		t.Run(edge, func(t *testing.T) {
			doc := navigationBindingFixture(edge)
			source := filepath.Join(t.TempDir(), "undefined-generation.pdf")
			if err := doc.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(run(t, qpdfTool, "--show-object=7,1", source))); got != "null" {
				t.Fatalf("qpdf found %q for an undefined generation", got)
			}
			assertNavigationRefusal(t, doc.Bytes(), "undefined PDF object 7 1")
			if err := pdf.Verify(t.Context(), doc.Bytes(), 1); err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid ||
				!strings.Contains(pdf.Diagnostic(err).Detail, "dangling output object 7 1 R") {
				t.Fatalf("reopened verification did not reject the undefined binding: %v", err)
			}
			valid := destinationOperandGenerationOne(doc.Bytes())
			for _, operation := range []app.Operation{app.Check, app.Build} {
				result, _, path := selectiveRequest(t, valid, "300x600pt", operation)
				if result.Status != "ok" || result.Output.Published != (operation == app.Build) {
					t.Fatal(result.Diagnostics)
				}
				if operation == app.Build {
					assertExplicitTail(t, path, []any{"/XYZ", 99.0, 400.0, 0.0})
				}
			}
		})
	}
}

func assertNavigationRefusal(t *testing.T, data []byte, message string, pages ...packet.PageRange) {
	t.Helper()
	dir := t.TempDir()
	source, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("existing output")
	if err := os.WriteFile(output, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := app.Execute(t.Context(), app.Request{Operation: op, WorkingDir: dir, InspectionWorkers: 1,
			Input: app.Declared{Declaration: packet.Declaration{Items: []packet.Item{&packet.SourceItem{
				Ref: "source", Path: source, Selection: pages,
			}}}}, Output: output, Overwrite: true})
		if result.Status != "invalid" || result.Output.Published || len(result.Diagnostics) != 1 ||
			result.Diagnostics[0].Code != diag.SourceUnsupported || !strings.Contains(result.Diagnostics[0].Message, message) ||
			result.Diagnostics[0].PDFLocation == nil {
			t.Fatal(result.Diagnostics)
		}
		if !bytes.Equal(readPDF(t, output), sentinel) {
			t.Fatal("refusal replaced the existing output")
		}
	}
}

func TestLocalGoToRequiresDestination(t *testing.T) {
	for _, entry := range []string{"", "/D null", "/D 7 0 R", "/D []"} {
		for _, compound := range []bool{false, true} {
			doc := destinationOperandFixture("link", "/Fit", "null")
			action := "<< /S /GoTo " + entry + " >>"
			if compound {
				action = "<< /S /URI /URI (https://example.invalid/) /Next [" + action + " << /S /GoTo /D [3 0 R /Fit] >>] >>"
			}
			doc.Objs[5] = []byte("<< /Type /Annot /Subtype /Link /Rect [10 20 30 40] /A " + action + " >>")
			err := pdf.Verify(t.Context(), doc.Bytes(), 1)
			if err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid ||
				!strings.Contains(pdf.Diagnostic(err).Detail, "requires a destination") {
				t.Fatalf("reopened malformed GoTo passed: %v", err)
			}
			assertNavigationRefusal(t, doc.Bytes(), "requires a destination")
		}
	}
}

func TestOptionalNullDestinationsAndActionsRemainAbsent(t *testing.T) {
	for _, value := range []string{"null", "7 0 R"} {
		for _, test := range []struct {
			entries string
			targets []int
		}{
			{"/Dest " + value, nil},
			{"/Dest " + value + " /A << /S /URI /URI (https://example.invalid/) /Next [" + value + " << /S /GoTo /D [3 0 R /Fit] >>] >>", []int{1}},
			{"/A " + value + " /Dest [3 0 R /Fit]", []int{1}},
			{"/A << /S /GoTo /D [3 0 R /Fit] /Next " + value + " >>", []int{1}},
		} {
			doc := destinationOperandFixture("link", "/Fit", "null")
			doc.Objs[5] = []byte("<< /Type /Annot /Subtype /Link /Rect [10 20 30 40] " + test.entries + " >>")
			for _, operation := range []app.Operation{app.Check, app.Build} {
				result, _, path := selectiveRequest(t, doc.Bytes(), "", operation)
				if result.Status != "ok" || result.Output.Published != (operation == app.Build) {
					t.Fatal(test.entries, result.Diagnostics)
				}
				if operation == app.Build {
					out, err := oracle.Load(oracle.RequireTools(t), path)
					if err != nil {
						t.Fatal(err)
					}
					targets, err := out.ActionTargets(1)
					if err != nil || len(targets) != len(test.targets) || len(targets) > 0 && !reflect.DeepEqual(targets, test.targets) {
						t.Fatal(test.entries, targets, err)
					}
				}
			}
		}
	}
}

func TestSelectedCompoundActionsRequireGoToDestination(t *testing.T) {
	doc := pdffixture.Pages("selected navigation", 2)
	insertEntry(doc, 4, "/AA << /O << /S /URI /URI (https://example.invalid/) /Next [<< /S /GoTo /D [4 0 R /Fit] >> << /S /GoTo /D null >>] >> >>")
	assertNavigationRefusal(t, doc.Bytes(), "requires a destination", packet.PageRange{First: 1, Last: 1})
}

func TestSelectedActionArrayPreservesSiblingsAndPrivateAlias(t *testing.T) {
	doc := pdffixture.Pages("selected navigation", 2)
	doc.Objs = append(doc.Objs, []byte("[<< /S /GoTo /D [6 0 R /Fit] >> << /S /GoTo /D [4 0 R /Fit] >>]"))
	first := "<< /S /URI /URI (https://example.invalid/open) /Next 8 0 R >>"
	last := "<< /S /URI /URI (https://example.invalid/close) /Next 8 0 R >>"
	insertEntry(doc, 4, "/AA << /O "+first+" /C "+last+" >> /Private 8 0 R")
	dir := t.TempDir()
	source, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.pdf")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	result := app.Execute(t.Context(), app.Request{Operation: app.Build, WorkingDir: dir, InspectionWorkers: 1,
		Input: app.Declared{Declaration: packet.Declaration{Items: []packet.Item{&packet.SourceItem{
			Ref: "selected", Path: source, Selection: []packet.PageRange{{First: 1, Last: 1}},
		}}}}, Output: output})
	if result.Status != "ok" || !result.Output.Published {
		t.Fatal(result.Diagnostics)
	}
	reopened, err := oracle.Load(oracle.RequireTools(t), output)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := reopened.ActionTargets(1)
	if err != nil || !reflect.DeepEqual(targets, []int{1, 1}) {
		t.Fatal(targets, err)
	}
	private, ok := qpdfPages(t, output)[0]["/Private"].([]any)
	if !ok || len(private) != 2 {
		t.Fatal("pruning changed the privately aliased action array", private)
	}
}

func TestNavigationNameCleanupLeavesPrivateCatalogNamesAlias(t *testing.T) {
	doc := navigationBindingFixture("catalog-names")
	doc.Objs[0] = bytes.ReplaceAll(doc.Objs[0], []byte("/Names 7 1 R"), []byte("/Names 7 0 R"))
	insertEntry(doc, 3, "/PrivateNames 7 0 R")
	result, _, path := selectiveRequest(t, doc.Bytes(), "", app.Build)
	if result.Status != "ok" {
		t.Fatal(result.Diagnostics)
	}
	assertLocalTargets(t, path, 1)
	private, ok := qpdfPages(t, path)[0]["/PrivateNames"].(map[string]any)
	if !ok || private["/Dests"] == nil {
		t.Fatal("catalog cleanup changed the original private Names dictionary", private)
	}
}

func TestNavigationBindingsRetainLiveGenerationThroughMerge(t *testing.T) {
	for _, edge := range []string{"action-owner", "next-array", "name-tree-child"} {
		t.Run(edge, func(t *testing.T) {
			dir := t.TempDir()
			prefix, second, output := filepath.Join(dir, "prefix.pdf"), filepath.Join(dir, "second.pdf"), filepath.Join(dir, "packet.pdf")
			if err := pdffixture.Plain("Prefix").WriteFile(prefix); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(second, destinationOperandGenerationOne(navigationBindingFixture(edge).Bytes()), 0600); err != nil {
				t.Fatal(err)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				result := app.Execute(t.Context(), app.Request{Operation: op, WorkingDir: dir, InspectionWorkers: 1,
					Input: app.Items{app.SourceItem{Ref: "prefix", Path: prefix}, app.SourceItem{Ref: "second", Path: second}}, Output: output})
				if result.Status != "ok" || result.Output.Published != (op == app.Build) {
					t.Fatal(result.Diagnostics)
				}
			}
			assertLocalTargets(t, output, 2)
		})
	}
}

func TestOptionalNullNavigationContainersRemainAbsent(t *testing.T) {
	for _, test := range []struct {
		name   string
		edit   func(*pdffixture.Doc)
		target int
	}{
		{"annotation-list", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Annots [6 0 R]"), []byte("/Annots 7 0 R"), 1)
		}, 0},
		{"form", func(d *pdffixture.Doc) { insertEntry(d, 1, "/AcroForm 7 0 R") }, 1},
		{"in-reply-to", func(d *pdffixture.Doc) { insertEntry(d, 6, "/IRT 7 0 R") }, 1},
		{"popup", func(d *pdffixture.Doc) { insertEntry(d, 6, "/Popup 7 0 R") }, 1},
		{"popup-parent", func(d *pdffixture.Doc) {
			d.Objs[5] = []byte("<< /Type /Annot /Subtype /Popup /Rect [10 20 30 40] /Parent 7 0 R >>")
		}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := destinationOperandFixture("link", "/Fit", "null")
			test.edit(doc)
			for _, operation := range []app.Operation{app.Check, app.Build} {
				result, _, path := selectiveRequest(t, doc.Bytes(), "", operation)
				if result.Status != "ok" || result.Output.Published != (operation == app.Build) {
					t.Fatal(result.Diagnostics)
				}
				if operation == app.Build {
					out, err := oracle.Load(oracle.RequireTools(t), path)
					if err != nil {
						t.Fatal(err)
					}
					targets, err := out.ActionTargets(1)
					if err != nil || len(targets) != test.target || len(targets) == 1 && targets[0] != 1 {
						t.Fatal(targets, err)
					}
				}
			}
		})
	}
}
