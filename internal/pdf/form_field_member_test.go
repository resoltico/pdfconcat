// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func fieldTreeFixture(tree, member string) *pdffixture.Doc {
	var doc *pdffixture.Doc
	switch tree {
	case "empty":
		doc = pdffixture.Plain("empty form")
		insertEntry(doc, 1, "/AcroForm << /Fields [] >>")
	case "root":
		doc = pdffixture.EmptySignatureField("unsigned signature")
	case "child":
		doc = pdffixture.EmptySignatureField("unsigned signature group")
		doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte("/Fields [6 0 R]"), []byte("/Fields [7 0 R]"), 1)
		doc.Objs[5] = bytes.Replace(doc.Objs[5], []byte("/FT /Sig /T (approval)"), []byte("/Parent 7 0 R"), 1)
		doc.Objs = append(doc.Objs, []byte("<< /FT /Sig /T (approval) /Kids [6 0 R] >>"))
	default:
		panic("unknown field fixture tree")
	}
	if member == "" {
		return doc
	}
	ref := member
	if member == "live-null" || member == "live-number" {
		value := "null"
		if member == "live-number" {
			value = "3"
		}
		doc.Objs = append(doc.Objs, []byte(value))
		ref = fmt.Sprintf("%d 0 R", len(doc.Objs))
	}
	switch tree {
	case "empty":
		doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte("/Fields []"), []byte("/Fields ["+ref+"]"), 1)
	case "root":
		doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte("/Fields [6 0 R]"), []byte("/Fields [6 0 R "+ref+"]"), 1)
	case "child":
		doc.Objs[6] = bytes.Replace(doc.Objs[6], []byte("/Kids [6 0 R]"), []byte("/Kids [6 0 R "+ref+"]"), 1)
	}
	return doc
}

func TestRequiredFormFieldMembersRefuseBeforePublication(t *testing.T) {
	for _, tree := range []struct {
		name, path string
		object     int
	}{{"empty", "Catalog.AcroForm.Fields[0]", 1}, {"root", "Catalog.AcroForm.Fields[1]", 1}, {"child", "Kids[1]", 7}} {
		for _, member := range []string{"live-null", "null", "live-number"} {
			t.Run(tree.name+"/"+member, func(t *testing.T) {
				d := fieldTreeFixture(tree.name, member)
				dir := t.TempDir()
				input, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.pdf")
				if err := d.WriteFile(input); err != nil {
					t.Fatal(err)
				}
				sentinel := []byte("existing output")
				if err := os.WriteFile(output, sentinel, 0600); err != nil {
					t.Fatal(err)
				}
				for _, op := range []app.Operation{app.Check, app.Build} {
					result := app.Execute(t.Context(), app.Request{Operation: op, WorkingDir: dir, InspectionWorkers: 1,
						Input: app.Items{app.SourceItem{Ref: "source", Path: input}}, Output: output, Overwrite: true})
					if result.Status != "invalid" || result.Output.Published || len(result.Diagnostics) != 1 {
						t.Fatal(result.Diagnostics)
					}
					finding := result.Diagnostics[0]
					if finding.Code != diag.SourceInvalid || finding.PDFLocation == nil ||
						finding.PDFLocation.Object != tree.object || finding.PDFLocation.Path != tree.path {
						t.Fatal(finding)
					}
					if !bytes.Equal(readPDF(t, output), sentinel) {
						t.Fatal("refusal replaced the existing output")
					}
				}
				err := pdf.Verify(t.Context(), d.Bytes(), 1)
				if err == nil {
					t.Fatal("reopened verifier accepted an invalid field node")
				}
				reopened := pdf.Diagnostic(err)
				if reopened.Code != diag.OutputInvalid || reopened.PDFLocation == nil ||
					reopened.PDFLocation.Path != tree.path || !strings.Contains(reopened.Detail, "must resolve to a dictionary") {
					t.Fatal(reopened)
				}
			})
		}
	}
}

func TestFormFieldTreeAcceptsEmptyAndUnsignedControls(t *testing.T) {
	for _, test := range []struct{ tree, value string }{
		{"empty", ""}, {"root", ""}, {"child", ""}, {"root", "null"}, {"child", "null"},
	} {
		t.Run(test.tree+"/"+test.value, func(t *testing.T) {
			doc := fieldTreeFixture(test.tree, "")
			if test.value != "" {
				field := 6
				if test.tree == "child" {
					field = 7
				}
				insertEntry(doc, field, "/V "+test.value)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, out := selectiveRequest(t, doc.Bytes(), "", op)
				if r.Status != "ok" || r.Output.Published != (op == app.Build) {
					t.Fatal(r.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, out)
				}
			}
		})
	}
}
