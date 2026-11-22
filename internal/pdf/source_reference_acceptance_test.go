// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func opaqueReferenceFixture(key, ref string, objects ...string) *pdffixture.Doc {
	doc := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		fmt.Appendf(nil, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << >> /Contents 4 0 R /%s %s >>", key, ref),
		layerStream("", "30 30 10 10 re f"),
	}}
	for _, object := range objects {
		doc.Objs = append(doc.Objs, []byte(object))
	}
	return doc
}

func sourceReferenceRequest(t *testing.T, dir string, op app.Operation, docs ...*packet.SourceItem) app.Result {
	t.Helper()
	items := make([]packet.Item, 0, len(docs))
	for _, doc := range docs {
		items = append(items, doc)
	}
	return app.Execute(t.Context(), app.Request{Operation: op, WorkingDir: dir, InspectionWorkers: 1,
		Input: app.Declared{Declaration: packet.Declaration{Items: items}}, Output: filepath.Join(dir, "output.pdf"), Overwrite: true})
}

func assertUndefinedSourceRefusal(t *testing.T, dir, badItem, ref string, sources ...*packet.SourceItem) {
	t.Helper()
	output := filepath.Join(dir, "output.pdf")
	sentinel := []byte("protected output")
	if err := os.WriteFile(output, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := sourceReferenceRequest(t, dir, op, sources...)
		if result.Status != "invalid" || result.Output.Published {
			t.Fatal(result.Diagnostics)
		}
		found := false
		for _, finding := range result.Diagnostics {
			if finding.Code == diag.SourceInvalid && strings.Contains(finding.Message, "undefined PDF object "+ref+".") &&
				finding.PDFLocation != nil && finding.PDFLocation.Object == 3 && finding.PDFLocation.Page == 1 &&
				finding.PDFLocation.Path == "AuditReference" && len(finding.Items) == 1 && finding.Items[0] == badItem {
				found = true
			}
		}
		if !found || !bytes.Equal(readPDF(t, output), sentinel) {
			t.Fatal(result.Diagnostics)
		}
	}
}

func TestUndefinedSourceReferencesCannotBindToAllocatedObjects(t *testing.T) {
	for _, n := range []int{5, 6, 7, 8, 9, 10, 20, 999999} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "undefined.pdf")
			ref := fmt.Sprintf("%d 0", n)
			if err := opaqueReferenceFixture("AuditReference", ref+" R").WriteFile(path); err != nil {
				t.Fatal(err)
			}
			actual := run(t, qpdfTool, fmt.Sprintf("--show-object=%d,0", n), path)
			if strings.TrimSpace(string(actual)) != "null" {
				t.Fatal(string(actual))
			}
			assertUndefinedSourceRefusal(t, dir, "undefined", ref, &packet.SourceItem{Path: path, Ref: "undefined"})
		})
	}
}

func TestSourceReferenceIdentitySurvivesBothMergeOrders(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.pdf")
	if err := opaqueReferenceFixture("FixtureMarker", "6 0 R", "null", "(first source marker)").WriteFile(target); err != nil {
		t.Fatal(err)
	}
	undefined := filepath.Join(dir, "undefined.pdf")
	if err := opaqueReferenceFixture("AuditReference", "6 0 R").WriteFile(undefined); err != nil {
		t.Fatal(err)
	}
	one := &packet.SourceItem{Path: target, Ref: "target"}
	two := &packet.SourceItem{Path: undefined, Ref: "undefined"}
	assertUndefinedSourceRefusal(t, dir, "undefined", "6 0", one, two)
	assertUndefinedSourceRefusal(t, dir, "undefined", "6 0", two, one)

	if err := opaqueReferenceFixture("AuditReference", "6 0 R", "null", "null").WriteFile(undefined); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.pdf")
	for _, order := range [][]*packet.SourceItem{{one, two}, {two, one}} {
		for _, op := range []app.Operation{app.Check, app.Build} {
			result := sourceReferenceRequest(t, dir, op, order...)
			if result.Status != "ok" || result.Output.Published != (op == app.Build) {
				t.Fatal(result.Diagnostics)
			}
		}
		assertLiveNullObject(t, output, "AuditReference")
		pages := qpdfPages(t, output)
		for i, source := range order {
			if source == one && pages[i]["/FixtureMarker"] != "u:first source marker" {
				t.Fatal(pages)
			}
			if source == two && pages[i]["/AuditReference"] != nil {
				t.Fatal(pages)
			}
		}
	}
}

func TestRetainedSourceReferencesRespectFreeEntriesAndGenerations(t *testing.T) {
	for _, invalid := range []string{"wrong-generation", "free"} {
		t.Run(invalid, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "source.pdf")
			doc := opaqueReferenceFixture("AuditReference", "7 1 R", "null", "null", "(retained marker)")
			data := doc.Bytes()
			if invalid == "free" {
				lines := bytes.Split(data, []byte{'\n'})
				for i, line := range lines {
					if string(line) == "xref" {
						lines[i+2] = []byte("0000000007 65535 f ")
						lines[i+2+7] = []byte("0000000000 00001 f ")
						break
					}
				}
				data = bytes.Join(lines, []byte{'\n'})
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			assertUndefinedSourceRefusal(t, dir, "source", "7 1", &packet.SourceItem{Path: path, Ref: "source"})
			valid := destinationOperandGenerationOne(doc.Bytes())
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, output := selectiveRequest(t, valid, "", op)
				if r.Status != "ok" || r.Output.Published != (op == app.Build) {
					t.Fatal(r.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, output)
					if qpdfPages(t, output)[0]["/AuditReference"] != "u:retained marker" {
						t.Fatal("a valid nonzero generation lost its value")
					}
				}
			}
		})
	}
}

func TestUndefinedReferencesInDiscardedSourceDataAreAllowed(t *testing.T) {
	plain := pdffixture.Pages("selection", 2)
	insertEntry(plain, 6, "/DiscardedPageReference 20 0 R")
	insertEntry(plain, 1, "/DiscardedCatalogReference 21 0 R")
	plain.Objs = append(plain.Objs, []byte("<< /Unreachable 22 0 R >>"))

	navigation := pdffixture.NavigationPages("omitted action target", false, false)
	navigation.Objs[len(navigation.Objs)-1] = []byte("<< /Type /Annot /Subtype /Link /Rect [50 100 180 120] /P 4 0 R /A << /S /GoTo /D [6 0 R /Fit] /DiscardedActionReference 20 0 R >> >>")

	tagged := pdffixture.Tagged("structure contribution")
	// The source structure root's private entries do not contribute to the
	// packet root. Its retained children refer back to it through /P.
	insertEntry(tagged, 6, "/DiscardedRootReference 20 0 R")

	for _, fixture := range []struct {
		name string
		doc  *pdffixture.Doc
	}{
		{"page-selection-and-catalog-cleanup", plain},
		{"pruned-action-chain", navigation},
		{"replaced-structure-root", tagged},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "source.pdf")
			if err := fixture.doc.WriteFile(path); err != nil {
				t.Fatal(err)
			}
			source := &packet.SourceItem{Path: path, Ref: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}
			for _, op := range []app.Operation{app.Check, app.Build} {
				r := sourceReferenceRequest(t, dir, op, source)
				if r.Status != "ok" || r.Output.Published != (op == app.Build) {
					t.Fatal(r.Diagnostics)
				}
			}
			out := filepath.Join(dir, "output.pdf")
			qpdfCheck(t, out)
			raw := run(t, qpdfTool, "--json", out)
			if bytes.Contains(raw, []byte("/Discarded")) || bytes.Contains(raw, []byte("/Unreachable")) {
				t.Fatal("discarded data survived")
			}
		})
	}
}
