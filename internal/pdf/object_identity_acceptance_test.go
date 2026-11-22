// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

func assertPhysicalIdentityFinding(t *testing.T, finding diag.Diagnostic, code diag.Code, path, detail string) {
	t.Helper()
	if finding.Code != code || finding.PDFLocation == nil || finding.PDFLocation.Path != path ||
		!strings.Contains(finding.Detail, detail) {
		t.Fatal(finding)
	}
}

func assertPhysicalIdentitySourceRefusal(t *testing.T, input, path, detail string) {
	t.Helper()
	dir := filepath.Dir(input)
	output := filepath.Join(dir, "output.pdf")
	sentinel := []byte("protected output")
	if err := os.WriteFile(output, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := sourceReferenceRequest(t, dir, op, &packet.SourceItem{Path: input, Ref: "source"})
		if result.Status != "invalid" || result.Output.Published || len(result.Diagnostics) != 1 {
			t.Fatal(result.Diagnostics)
		}
		assertPhysicalIdentityFinding(t, result.Diagnostics[0], diag.SourceInvalid, path, detail)
		if !bytes.Equal(readPDF(t, output), sentinel) {
			t.Fatal("refusal replaced the existing output")
		}
	}
}

func TestReopenRejectsPhysicalGenerationMismatch(t *testing.T) {
	doc := opaqueReferenceFixture("AuditReference", "5 0 R", "(real source object)")
	built, job, data := assembleReferenceWitness(t, doc, nil)
	ref := regexp.MustCompile(`/AuditReference ([0-9]+) 0 R\b`).FindSubmatch(data)
	if len(ref) != 2 {
		t.Fatal("missing staged reference")
	}
	number := string(ref[1])
	header := []byte("\n" + number + " 0 obj\n")
	if bytes.Count(data, header) != 1 {
		t.Fatal("missing unique object header")
	}
	changed := bytes.Replace(data, header, []byte("\n"+number+" 1 obj\n"), 1)
	path := filepath.Join(t.TempDir(), "header-mismatch.pdf")
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	tools := oracle.RequireTools(t)
	out, err := exec.CommandContext(t.Context(), tools.QPDF, "--check", path).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || !strings.Contains(string(out), "expected "+number+" 0 obj") {
		t.Fatalf("qpdf failed to identify the header mismatch: %v\n%s", err, out)
	}
	detail := fmt.Sprintf("xref identity %s 0 R points to physical object %s 1 R", number, number)
	for _, err := range []error{
		pdf.Verify(t.Context(), changed, 1),
		built.VerifyReader(t.Context(), bytes.NewReader(changed), 1, job),
	} {
		if err == nil {
			t.Fatal("strict reopen accepted a mismatched object header")
		}
		assertPhysicalIdentityFinding(t, pdf.Diagnostic(err), diag.OutputInvalid, "AuditReference", detail)
	}
	assertPhysicalIdentitySourceRefusal(t, path, "AuditReference", detail)
	if got := readPDF(t, path); !bytes.Equal(got, changed) {
		t.Fatal("refusal changed the input")
	}
}

func TestPhysicalObjectNumberAliasRequiresReachability(t *testing.T) {
	doc := opaqueReferenceFixture("AuditReference", "5 0 R", "(real source object)", "(unreachable)")
	data := doc.Bytes()
	offset5 := bytes.Index(data, []byte("\n5 0 obj\n")) + 1
	offset6 := bytes.Index(data, []byte("\n6 0 obj\n")) + 1
	alias := bytes.Replace(data, fmt.Appendf(nil, "%010d 00000 n ", offset6),
		fmt.Appendf(nil, "%010d 00000 n ", offset5), 1)
	if bytes.Equal(alias, data) {
		t.Fatal("missing xref row to alias")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "alias.pdf")
	if err := os.WriteFile(path, alias, 0600); err != nil {
		t.Fatal(err)
	}
	if err := pdf.Verify(t.Context(), alias, 1); err != nil {
		t.Fatal(err)
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := sourceReferenceRequest(t, dir, op, &packet.SourceItem{Path: path, Ref: "source"})
		if result.Status != "ok" || result.Output.Published != (op == app.Build) {
			t.Fatal(result.Diagnostics)
		}
	}
	if got := qpdfPages(t, filepath.Join(dir, "output.pdf"))[0]["/AuditReference"]; got != "u:real source object" {
		t.Fatal(got)
	}
	used := bytes.Replace(alias, []byte("/AuditReference 5 0 R"), []byte("/AuditReference 6 0 R"), 1)
	if err := os.WriteFile(path, used, 0600); err != nil {
		t.Fatal(err)
	}
	assertPhysicalIdentitySourceRefusal(t, path, "AuditReference", "xref identity 6 0 R points to physical object 5 0 R")
}

func TestActiveXRefStreamMatchesEffectiveSelfEntry(t *testing.T) {
	doc := opaqueReferenceFixture("Mark", "(page)")
	built, job, _ := assembleReferenceWitness(t, doc, nil)
	stream := freeHeadSourceRepresentation(doc.Bytes(), "stream")
	hybrid := freeHeadSourceRepresentation(doc.Bytes(), "hybrid")
	header := []byte("\n5 0 obj\n")
	wrongGeneration := []byte("\n5 1 obj\n")
	wrongNumber := []byte("\n9 0 obj\n")
	badGeneration := bytes.Replace(stream, header, wrongGeneration, 1)
	badNumber := bytes.Replace(stream, header, wrongNumber, 1)
	previous := bytes.Index(stream, header) + 1
	// The final byte of the fifth seven-byte row stores its generation.
	live := bytes.Clone(badGeneration)
	start := bytes.Index(live[previous:], []byte("\nstream\n")) + previous + len("\nstream\n")
	live[start+5*7-1] = 1
	// Retain the four document rows and omit the stream's own row.
	withoutSelf := append(bytes.Clone(stream[:start+4*7]), stream[start+5*7:]...)
	withoutSelf = bytes.Replace(withoutSelf, []byte("/Index [1 5] /W [1 4 2] /Length 35"),
		[]byte("/Index [1 4] /W [1 4 2] /Length 28"), 1)
	for _, tc := range []struct {
		name, actual string
		data         []byte
	}{
		{"startxref-generation", "5 1 R", badGeneration},
		{"startxref-number", "9 0 R", badNumber},
		{"hybrid-generation", "5 1 R", bytes.Replace(hybrid, header, wrongGeneration, 1)},
		{"hybrid-number", "9 0 R", bytes.Replace(hybrid, header, wrongNumber, 1)},
		{"prev-generation", "5 1 R", appendXRefIdentityRevision(badGeneration, previous, 6, 0)},
		{"prev-number", "9 0 R", appendXRefIdentityRevision(badNumber, previous, 6, 0)},
		{"nonzero-generation", "", live},
		{"superseded", "", appendXRefIdentityRevision(stream, previous, 5, 1)},
		{"omitted-self-entry", "", withoutSelf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "xref.pdf")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(tc.name, "startxref-") {
				tools := oracle.RequireTools(t)
				out, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-object=5,0", path).CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 3 || !bytes.Contains(out, []byte("expected 5 0 obj")) {
					t.Fatalf("qpdf failed to identify the xref stream header mismatch: %v\n%s", err, out)
				}
			}
			standalone := pdf.Verify(t.Context(), tc.data, 1)
			if tc.actual != "" {
				detail := "xref identity 5 0 R points to physical object " + tc.actual
				for _, err := range []error{standalone, built.VerifyReader(t.Context(), bytes.NewReader(tc.data), 1, job)} {
					if err == nil {
						t.Fatal("strict reopen accepted a mismatched xref stream header")
					}
					assertPhysicalIdentityFinding(t, pdf.Diagnostic(err), diag.OutputInvalid, "XRef[5]", detail)
				}
				assertPhysicalIdentitySourceRefusal(t, path, "XRef[5]", detail)
				return
			}
			if standalone != nil {
				t.Fatal(standalone)
			}
			if tc.name != "omitted-self-entry" {
				object := run(t, qpdfTool, "--show-object=5,1", path)
				if tc.name == "superseded" && strings.TrimSpace(string(object)) != "(new revision)" ||
					tc.name == "nonzero-generation" && !bytes.Contains(object, []byte("/Type /XRef")) {
					t.Fatalf("qpdf resolved the wrong object: %s", object)
				}
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				result := sourceReferenceRequest(t, dir, op, &packet.SourceItem{Path: path, Ref: "source"})
				if result.Status != "ok" || result.Output.Published != (op == app.Build) {
					t.Fatal(result.Diagnostics)
				}
			}
			qpdfCheck(t, filepath.Join(dir, "output.pdf"))
		})
	}
}

func appendXRefIdentityRevision(data []byte, previous, number, generation int) []byte {
	revision := fmt.Appendf(bytes.Clone(data), "%d %d obj\n(new revision)\nendobj\n", number, generation)
	return fmt.Appendf(revision, "xref\n%d 1\n%010d %05d n \ntrailer\n<< /Size %d /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		number, len(data), generation, number+1, previous, len(revision))
}

func TestCompressedObjectIdentityMatchesItsDirectory(t *testing.T) {
	objects := layerStream("/Type /ObjStm /N 2 /First 9", "7 0 8 17 (identity seven) (identity eight)")
	doc := opaqueReferenceFixture("AuditA", "7 0 R /AuditB 8 0 R", string(objects))
	control := objectStreamXRefFixture(t, doc, 0, 1)
	swapped := objectStreamXRefFixture(t, doc, 1, 0)
	container := bytes.Replace(control, []byte("\n5 0 obj\n"), []byte("\n5 1 obj\n"), 1)
	// Supersede both compressed entries with matching uncompressed entries.
	newest := append(bytes.Clone(swapped), []byte("7 0 obj\n(identity seven)\nendobj\n")...)
	offset8 := len(newest)
	newest = append(newest, []byte("8 0 obj\n(identity eight)\nendobj\n")...)
	previous := bytes.Index(swapped, []byte("\n6 0 obj\n")) + 1
	newest = fmt.Appendf(newest, "xref\n7 2\n%010d 00000 n \n%010d 00000 n \ntrailer\n<< /Size 9 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		len(swapped), offset8, previous, len(newest))
	stageDoc := opaqueReferenceFixture("AuditA", "(existing stage)")
	built, job, _ := assembleReferenceWitness(t, stageDoc, nil)
	for _, test := range []struct {
		name, path, detail string
		data               []byte
	}{
		{name: "control", data: control},
		{name: "swapped", path: "XRef[7]", detail: "object stream 5 index 1: xref identity 7 0 R points to physical object 8 0 R", data: swapped},
		{name: "container", path: "XRef[5]", detail: "xref identity 5 0 R points to physical object 5 1 R", data: container},
		{name: "superseded", data: newest},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "source.pdf")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			if test.name == "control" || test.name == "swapped" {
				assertIndependentObjectStreamIdentity(t, path, test.name == "swapped")
			}
			err := pdf.Verify(t.Context(), test.data, 1)
			if test.detail != "" {
				for _, err := range []error{err, built.VerifyReader(t.Context(), bytes.NewReader(test.data), 1, job)} {
					if err == nil {
						t.Fatal("strict reopen accepted the wrong compressed identity")
					}
					assertPhysicalIdentityFinding(t, pdf.Diagnostic(err), diag.OutputInvalid, test.path, test.detail)
				}
				assertPhysicalIdentitySourceRefusal(t, path, test.path, test.detail)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				result := sourceReferenceRequest(t, dir, op, &packet.SourceItem{Path: path, Ref: "source"})
				if result.Status != "ok" || result.Output.Published != (op == app.Build) {
					t.Fatal(result.Diagnostics)
				}
			}
			output := filepath.Join(dir, "output.pdf")
			qpdfCheck(t, output)
			page := qpdfPages(t, output)[0]
			if page["/AuditA"] != "u:identity seven" || page["/AuditB"] != "u:identity eight" {
				t.Fatal(page)
			}
		})
	}
}

func assertIndependentObjectStreamIdentity(t *testing.T, path string, swapped bool) {
	t.Helper()
	qpdfCheck(t, path)
	stream := strings.Fields(string(run(t, qpdfTool, "--show-object=5", "--filtered-stream-data", path)))
	xref := string(run(t, qpdfTool, "--show-xref", path))
	first, second := 0, 1
	if swapped {
		first, second = second, first
	}
	if len(stream) < 4 || stream[0] != "7" || stream[2] != "8" ||
		!strings.Contains(xref, fmt.Sprintf("7/0: compressed; stream = 5, index = %d", first)) ||
		!strings.Contains(xref, fmt.Sprintf("8/0: compressed; stream = 5, index = %d", second)) {
		t.Fatalf("unexpected object-stream directory or xref rows: %v\n%s", stream, xref)
	}
}
