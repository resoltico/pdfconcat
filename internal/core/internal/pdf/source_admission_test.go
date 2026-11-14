// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestSourceAdmissionRefusesMissingAndWrongGenerationContent(t *testing.T) {
	for _, tc := range []struct {
		name, original, replacement string
		object                      int
	}{
		{"missing stream", "/Contents 4 0 R", "/Contents 99 0 R", 3},
		{"wrong stream generation", "/Contents 4 0 R", "/Contents 4 1 R", 3},
		{"missing array member", "/Contents 4 0 R", "/Contents [4 0 R 99 0 R]", 3},
		{"null array member", "/Contents 4 0 R", "/Contents [4 0 R null]", 3},
		{"wrong page generation", "/Kids [3 0 R]", "/Kids [3 1 R]", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := pdffixture.Plain("must remain visible")
			if !bytes.Contains(doc.Objs[tc.object-1], []byte(tc.original)) {
				t.Fatal("fixture no longer contains the intended reference")
			}
			doc.Objs[tc.object-1] = bytes.Replace(doc.Objs[tc.object-1], []byte(tc.original), []byte(tc.replacement), 1)
			_, _, ds := pdf.Inspect(t.Context(), doc.Bytes(), nil)
			if !codePresent(ds, diag.SourceDamaged) {
				t.Fatalf("content/page damage was not refused: %+v", ds)
			}
		})
	}
}

func TestSourceAdmissionRetainsActiveContentAndReportsItsNotice(t *testing.T) {
	doc := insertEntry(pdffixture.Plain("active"), 3, "/AA << /O << /S /JavaScript /JS (void 0;) >> >>")
	facts, _, ds := pdf.Inspect(t.Context(), doc.Bytes(), nil)
	if status, _ := diag.Outcome(ds); status != "ok" || facts.Pages != 1 || !codePresent(ds, diag.SourceValidationNotice) || codePresent(ds, diag.SourceDamaged) {
		t.Fatalf("retained active content misclassified: %+v", ds)
	}
	path := assembleSourceBytes(t, doc.Bytes())
	out, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	if out.FirstLine(1) != "active" {
		t.Fatal("page content changed")
	}
	raw := run(t, qpdfTool, "--json", path)
	if !bytes.Contains(raw, []byte(`"/JavaScript"`)) || !bytes.Contains(raw, []byte("void 0;")) {
		t.Fatal("retained page script was lost")
	}
}

func TestWholeSourceRefusalsExplainSeparateOriginalDelivery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		doc    *pdffixture.Doc
		code   diag.Code
		reason string
	}{
		{"signature", pdffixture.SignatureValue("signed"), diag.SourceSigned, "signature"},
		{"permissions", insertEntry(pdffixture.Plain("permissions"), 1, "/Perms << >>"), diag.SourceSigned, "permission structures"},
		{"layers", insertEntry(pdffixture.Plain("layers"), 1, "/OCProperties << >>"), diag.SourceOptionalContent, "visibility layers"},
		{"dynamic", insertEntry(pdffixture.Plain("dynamic"), 1, "/NeedsRendering true"), diag.SourceDynamicForm, "dynamic rendering"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ds := pdf.Inspect(t.Context(), tc.doc.Bytes(), nil)
			if len(ds) != 1 || ds[0].Code != tc.code || !strings.Contains(ds[0].Message, tc.reason) || !strings.Contains(ds[0].Message, "composition") || !strings.Contains(ds[0].Message, "original separately") {
				t.Fatalf("refusal lost reason or safe next step: %+v", ds)
			}
		})
	}
}
