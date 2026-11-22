// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func structureDestinationFixture(carrier, version, value string) *pdffixture.Doc {
	doc := localLink("/Fit", true)
	doc.Version = version
	doc.Objs[2] = []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R /Annots [6 0 R] >>")
	doc.Objs[3] = layerStream("", "10 10 20 20 re f")
	doc.Objs[6] = []byte("null")
	link := "<< /Type /Annot /Subtype /Link /Rect [10 20 30 40]"
	definition := "<< /D [3 0 R /Fit] /SD " + value + " >>"
	switch carrier {
	case "action":
		link += " /A << /S /GoTo /D [3 0 R /Fit] /SD " + value + " >>"
	case "name-tree":
		insertEntry(doc, 1, "/Names << /Dests << /Names [(target) "+definition+"] >> >>")
		link += " /Dest (target)"
	case "legacy":
		insertEntry(doc, 1, "/Dests << /target "+definition+" >>")
		link += " /Dest /target"
	}
	doc.Objs[5] = []byte(link + " >>")
	return doc
}

func assertLocalTargets(t *testing.T, path string, pages ...int) {
	t.Helper()
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		targets, err := output.ActionTargets(page)
		if err != nil || !reflect.DeepEqual(targets, []int{page}) {
			t.Fatalf("source link on composed page %d targets %v: %v", page, targets, err)
		}
	}
}

func TestNullStructureDestinationKeepsLocalTarget(t *testing.T) {
	for _, carrier := range []string{"action", "name-tree", "legacy"} {
		for _, value := range []string{"null", "7 0 R"} {
			t.Run(carrier+"/"+value, func(t *testing.T) {
				doc := structureDestinationFixture(carrier, "2.0", value)
				if err := pdf.Verify(t.Context(), doc.Bytes(), 1); err != nil {
					t.Fatalf("serialized null carrier failed verification: %+v", pdf.Diagnostic(err))
				}
				for _, operation := range []app.Operation{app.Check, app.Build} {
					result, _, output := selectiveRequest(t, doc.Bytes(), "", operation)
					if result.Status != "ok" || result.Output.Published != (operation == app.Build) ||
						codePresent(result.Diagnostics, diag.SourceStructureDestination) {
						t.Fatal(result.Diagnostics)
					}
					if operation == app.Build {
						assertLocalTargets(t, output, 1)
					}
				}
			})
		}
	}
}

func TestStructureDestinationNullDoesNotMaskBrokenReferences(t *testing.T) {
	for _, carrier := range []string{"action", "name-tree", "legacy"} {
		for _, value := range []string{"7 1 R", "20 0 R"} {
			t.Run(carrier+"/"+value, func(t *testing.T) {
				doc := structureDestinationFixture(carrier, "2.0", value)
				_, _, ds := pdf.Inspect(t.Context(), doc.Bytes(), "", nil)
				if !codePresent(ds, diag.SourceStructureDestination) {
					t.Fatal(ds)
				}
				err := pdf.Verify(t.Context(), doc.Bytes(), 1)
				if err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid ||
					!strings.Contains(pdf.Diagnostic(err).Detail, "dangling output object "+value) {
					t.Fatalf("serialized malformed structure destination: %v", err)
				}
			})
		}
	}
}

func TestNullStructureDestinationInMixedPDFVersions(t *testing.T) {
	dir := t.TempDir()
	var inputs app.Items
	for i, doc := range []*pdffixture.Doc{
		structureDestinationFixture("action", "1.7", "7 0 R"),
		structureDestinationFixture("name-tree", "2.0", "7 0 R"),
	} {
		path := filepath.Join(dir, []string{"pdf17.pdf", "pdf20.pdf"}[i])
		if err := doc.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, app.SourceItem{Ref: filepath.Base(path), Path: path})
	}
	output := filepath.Join(dir, "mixed.pdf")
	for _, operation := range []app.Operation{app.Check, app.Build} {
		result := app.Execute(t.Context(), app.Request{
			Operation: operation, InspectionWorkers: 1, WorkingDir: dir, Input: inputs, Output: output,
		})
		if result.Status != "ok" || result.Output.Published != (operation == app.Build) {
			t.Fatal(result.Diagnostics)
		}
	}
	assertLocalTargets(t, output, 1, 2)
}
