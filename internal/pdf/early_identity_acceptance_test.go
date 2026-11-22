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

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func withDirectInfo(doc *pdffixture.Doc, info string) []byte {
	doc.Version = "2.0"
	return bytes.Replace(doc.Bytes(), []byte("/Root 1 0 R >>\nstartxref"),
		[]byte("/Root 1 0 R /Info "+info+" >>\nstartxref"), 1)
}

const directCreationDate = "<< /CreationDate (D:20201121000000Z) >>"

func TestDirectInfoCannotGiveAnUndefinedReferenceAnIdentity(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.pdf")
	if err := opaqueReferenceFixture("FixtureMarker", "5 0 R", "(neighbor marker)").WriteFile(target); err != nil {
		t.Fatal(err)
	}
	for _, number := range []int{5, 20} {
		t.Run(fmt.Sprintf("object-%d", number), func(t *testing.T) {
			ref := fmt.Sprintf("%d 0", number)
			data := withDirectInfo(opaqueReferenceFixture("AuditReference", ref+" R"), directCreationDate)
			source := filepath.Join(dir, fmt.Sprintf("undefined-%d.pdf", number))
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(run(t, qpdfTool, fmt.Sprintf("--show-object=%d,0", number), source))); got != "null" {
				t.Fatalf("qpdf found an unexpected original target: %q", got)
			}
			bad := &packet.SourceItem{Path: source, Ref: "undefined"}
			good := &packet.SourceItem{Path: target, Ref: "target"}
			assertUndefinedSourceRefusal(t, dir, "undefined", ref, bad)
			assertUndefinedSourceRefusal(t, dir, "undefined", ref, good, bad)
			assertUndefinedSourceRefusal(t, dir, "undefined", ref, bad, good)
			err := pdf.Verify(t.Context(), data, 1)
			if err == nil {
				t.Fatal("strict reopen accepted an undefined reference alongside direct Info")
			}
			finding := pdf.Diagnostic(err)
			if finding.Code != diag.OutputInvalid || finding.PDFLocation == nil || finding.PDFLocation.Path != "AuditReference" ||
				!strings.Contains(finding.Detail, "dangling output object "+ref+" R") {
				t.Fatal(finding)
			}
		})
	}
}

func TestDirectInfoRetentionKeepsDefaultBackendMaterialization(t *testing.T) {
	data := withDirectInfo(opaqueReferenceFixture("Private", "(drawing)"), directCreationDate)
	for _, keep := range []bool{false, true} {
		config := model.NewStatelessConfiguration()
		config.ValidationMode = model.ValidationStrict
		config.KeepDirectInfo = keep
		doc, err := api.ReadContext(t.Context(), bytes.NewReader(data), config)
		if err != nil {
			t.Fatal(err)
		}
		if keep {
			if doc.Info != nil || doc.DirectInfoDict == nil || doc.Exists(5) {
				t.Fatal("retaining direct Info allocated a source object")
			}
		} else if doc.Info == nil || doc.DirectInfoDict != nil || doc.Info.ObjectNumber.Value() != 5 {
			t.Fatal("default reader did not materialize Info")
		}
	}
}

func TestDirectInfoPreservesValidSourceReferencesAndVerification(t *testing.T) {
	for _, test := range []struct {
		name       string
		doc        *pdffixture.Doc
		generation bool
		wantText   bool
		wantNull   bool
	}{
		{"direct-info-only", opaqueReferenceFixture("Private", "(drawing)"), false, false, false},
		{"live-object-five", opaqueReferenceFixture("AuditReference", "5 0 R", "(source marker)"), false, true, false},
		{"live-null", opaqueReferenceFixture("AuditReference", "5 0 R", "null"), false, false, true},
		{"nonzero-generation", opaqueReferenceFixture("AuditReference", "7 1 R", "null", "null", "(source marker)"), true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := withDirectInfo(test.doc, directCreationDate)
			if test.generation {
				data = destinationOperandGenerationOne(data)
			}
			if err := pdf.Verify(t.Context(), data, 1); err != nil {
				t.Fatal(err)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, output := selectiveRequest(t, data, "", op)
				if r.Status != "ok" || r.Output.Published != (op == app.Build) {
					t.Fatal(r.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, output)
					if test.wantText && qpdfPages(t, output)[0]["/AuditReference"] != "u:source marker" {
						t.Fatal("source reference did not retain its value")
					}
					if test.wantNull {
						assertLiveNullObject(t, output, "AuditReference")
					}
					if bytes.Contains(readPDF(t, output), []byte("/Info")) {
						t.Fatal("source trailer Info survived composition")
					}
				}
			}
		})
	}
}

func TestDirectInfoKeepsSourceSignatureInspection(t *testing.T) {
	data := withDirectInfo(opaqueReferenceFixture("Private", "(drawing)"), "<< /Type /Sig >>")
	assertLocatedReadRefusal(t, data, diag.SourceSigned, "Trailer.Info")
}

func TestEarlyReaderUsesOriginalZeroOffsetHeadClassification(t *testing.T) {
	base := opaqueReferenceFixture("Private", "(valid)").Bytes()
	bad := bytes.Replace(base, []byte("0000000000 65535 f "), []byte("0000000000 00000 n "), 1)
	assertLocatedReadRefusal(t, bad, diag.SourceDamaged, "XRef[0]")

	config := model.NewStatelessConfiguration()
	config.ValidationMode = model.ValidationRelaxed
	s, err := api.ReadContext(t.Context(), bytes.NewReader(bad), config)
	if err != nil || s.Table[0] == nil || !s.Table[0].Free {
		t.Fatalf("relaxed read lost its legacy repair: %v", err)
	}

	old := bytes.Index(bad, []byte("\nxref\n")) + 1
	// A newer free row supersedes the older in-use row.
	repaired := fmt.Appendf(bytes.Clone(bad), "xref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 5 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", old, len(bad))
	for _, op := range []app.Operation{app.Check, app.Build} {
		r, _, out := selectiveRequest(t, repaired, "", op)
		if r.Status != "ok" || r.Output.Published != (op == app.Build) {
			t.Fatal(r.Diagnostics)
		}
		if op == app.Build {
			qpdfCheck(t, out)
		}
	}
}
