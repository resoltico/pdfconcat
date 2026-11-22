// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

func assertLocatedReadRefusal(t *testing.T, data []byte, code diag.Code, location string) {
	t.Helper()
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.pdf")
	sentinel := []byte("existing output")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
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
		if finding.Code != code || finding.PDFLocation == nil || finding.PDFLocation.Path != location {
			t.Fatal(finding)
		}
		if !bytes.Equal(readPDF(t, output), sentinel) {
			t.Fatal("read refusal replaced the existing output")
		}
	}
	err := pdf.Verify(t.Context(), data, 1)
	if err == nil {
		t.Fatal("reopened output accepted an invalid read boundary")
	}
	finding := pdf.Diagnostic(err)
	if finding.Code != diag.OutputInvalid || finding.PDFLocation == nil || finding.PDFLocation.Path != location {
		t.Fatal(finding)
	}
}

func TestEarlyReaderRejectsOccupiedObjectZero(t *testing.T) {
	for _, test := range []struct {
		name, value string
		freeMember  bool
	}{
		{"without-undefined-reference", "(valid)", false},
		{"with-undefined-reference", "20 0 R", false},
		{"with-another-free-object", "(valid)", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := opaqueReferenceFixture("Private", test.value)
			if test.freeMember {
				doc.Objs = append(doc.Objs, []byte("(unused)"))
			}
			base := doc.Bytes()
			if test.freeMember {
				offset := bytes.Index(base, []byte("5 0 obj\n"))
				base = bytes.Replace(base, fmt.Appendf(nil, "%010d 00000 n ", offset), []byte("0000000000 00001 f "), 1)
			}
			xref := bytes.Index(base, []byte("\nxref\n")) + 1
			body := []byte("0 0 obj\n(occupied zero)\nendobj\n")
			data := make([]byte, 0, len(base)+len(body))
			data = append(data, base[:xref]...)
			data = append(data, body...)
			data = append(data, base[xref:]...)
			data = bytes.Replace(data, []byte("0000000000 65535 f "), fmt.Appendf(nil, "%010d 00000 n ", xref), 1)
			data = bytes.Replace(data, fmt.Appendf(nil, "startxref\n%d\n", xref), fmt.Appendf(nil, "startxref\n%d\n", xref+len(body)), 1)
			assertLocatedReadRefusal(t, data, diag.SourceDamaged, "XRef[0]")
		})
	}
}

func TestEarlyReaderUsesEffectiveFreeListHead(t *testing.T) {
	base := opaqueReferenceFixture("Private", "(valid)").Bytes()
	for _, format := range []string{"classic", "incremental", "stream", "hybrid"} {
		t.Run(format, func(t *testing.T) {
			data := freeHeadSourceRepresentation(base, format)
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, out := selectiveRequest(t, data, "", op)
				if result.Status != "ok" || result.Output.Published != (op == app.Build) {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, out)
				}
			}
		})
	}
}

// Each non-classic witness omits entry zero in the newest xref subsection.
func freeHeadSourceRepresentation(base []byte, format string) []byte {
	if format == "classic" {
		return base
	}
	oldXRef := bytes.Index(base, []byte("\nxref\n")) + 1
	if format == "incremental" {
		data := append(bytes.Clone(base), []byte("5 0 obj\n(added)\nendobj\n")...)
		return fmt.Appendf(data, "xref\n5 1\n%010d 00000 n \ntrailer\n<< /Size 6 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", len(base), oldXRef, len(data))
	}
	var b bytes.Buffer
	b.Write(base[:oldXRef])
	offsets := make([]int, 6)
	for n := 1; n <= 4; n++ {
		offsets[n] = bytes.Index(base, []byte(strconv.Itoa(n)+" 0 obj\n"))
	}
	offsets[5] = b.Len()
	var records [35]byte
	for n := 1; n <= 5; n++ {
		record := records[(n-1)*7 : n*7]
		record[0] = 1
		offset := offsets[n]
		if offset < 0 || offset > math.MaxUint32 {
			panic("fixture object offset does not fit the xref stream")
		}
		binary.BigEndian.PutUint32(record[1:5], uint32(offset))
	}
	fmt.Fprintf(&b, "5 0 obj\n<< /Type /XRef /Size 6 /Root 1 0 R /Index [1 5] /W [1 4 2] /Length %d >>\nstream\n", len(records))
	b.Write(records[:])
	b.WriteString("\nendstream\nendobj\n")
	xref := offsets[5]
	if format == "hybrid" {
		xref = b.Len()
		b.WriteString("xref\n1 5\n")
		for n := 1; n <= 5; n++ {
			fmt.Fprintf(&b, "%010d 00000 n \n", offsets[n])
		}
		fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R /XRefStm %d >>\n", offsets[5])
	}
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

func TestEarlyReaderRequiresQualifiedCatalogVersionReference(t *testing.T) {
	for _, ref := range []string{"7 1 R", "20 0 R"} {
		t.Run(ref, func(t *testing.T) {
			doc := opaqueReferenceFixture("Private", "(valid)", "null", "null", "/2.0")
			insertEntry(doc, 1, "/Version "+ref)
			input := filepath.Join(t.TempDir(), "input.pdf")
			if err := doc.WriteFile(input); err != nil {
				t.Fatal(err)
			}
			if raw := run(t, qpdfTool, "--show-object=1", input); bytes.Contains(raw, []byte("/Version")) {
				t.Fatalf("qpdf used an undefined version: %s", raw)
			}
			assertLocatedReadRefusal(t, doc.Bytes(), diag.SourceInvalid, "Catalog.Version")
		})
	}
}

func TestEarlyReaderPreservesEffectiveVersion(t *testing.T) {
	for _, test := range []struct {
		name, header, entry, object, want string
		generationOne                     bool
	}{
		{"absent", "1.7", "", "null", "1.7", false},
		{"direct", "1.7", "/2.0", "null", "2.0", false},
		{"live", "1.7", "7 0 R", "/2.0", "2.0", false},
		{"live-generation-one", "1.7", "7 1 R", "/2.0", "2.0", true},
		{"live-null", "1.7", "7 0 R", "null", "1.7", false},
		{"older-override", "2.0", "/1.7", "null", "2.0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := opaqueReferenceFixture("Private", "(valid)", "null", "null", test.object)
			doc.Version = test.header
			if test.entry != "" {
				insertEntry(doc, 1, "/Version "+test.entry)
			}
			data := doc.Bytes()
			if test.generationOne {
				data = destinationOperandGenerationOne(data)
			}
			if err := pdf.Verify(t.Context(), data, 1); err != nil {
				t.Fatal(err)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, out := selectiveRequest(t, data, "", op)
				if result.Status != "ok" || result.Output.Published != (op == app.Build) {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					if !bytes.HasPrefix(readPDF(t, out), []byte("%PDF-"+test.want+"\n")) {
						t.Fatal("Build used the wrong effective PDF version")
					}
					qpdfCheck(t, out)
				}
			}
		})
	}
}

func TestEarlyReaderVersionSelectsRealAnnotationRules(t *testing.T) {
	for _, test := range []struct {
		name, entry string
		code        diag.Code
	}{
		{"absent-version", "", diag.Code{}},
		{"wrong-generation", "/Version 6 1 R", diag.SourceInvalid},
		{"live-version", "/Version 6 0 R", diag.SourceOutputIncompatible},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := opaqueReferenceFixture("Private", "(valid)",
				"<< /Type /Annot /Subtype /Text /Rect [10 10 20 20] /Contents (note) >>", "/2.0")
			insertEntry(d, 3, "/Annots [5 0 R]")
			if test.entry != "" {
				insertEntry(d, 1, test.entry)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, out := selectiveRequest(t, d.Bytes(), "", op)
				if test.code == (diag.Code{}) {
					if result.Status != "ok" || result.Output.Published != (op == app.Build) {
						t.Fatal(result.Diagnostics)
					}
					if op == app.Build {
						qpdfCheck(t, out)
					}
					continue
				}
				if result.Status != "invalid" || result.Output.Published || !codePresent(result.Diagnostics, test.code) {
					t.Fatal(result.Diagnostics)
				}
				if test.code == diag.SourceInvalid && codePresent(result.Diagnostics, diag.SourceOutputIncompatible) {
					t.Fatal("undefined version selected PDF 2.0 appearance policy", result.Diagnostics)
				}
			}
		})
	}
}
