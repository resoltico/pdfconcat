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
	"regexp"
	"strconv"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func assembleReferenceWitness(t *testing.T, doc *pdffixture.Doc, metadata *packet.Metadata) (pdf.Assembled, pdf.Assembly, []byte) {
	t.Helper()
	data := doc.Bytes()
	input := filepath.Join(t.TempDir(), "source.pdf")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	facts, views, ds := pdf.Inspect(t.Context(), data, "", []pdf.SelectionRequest{{Item: "source"}})
	if diag.Outcome(ds) != "ok" || len(views) != 1 || views[0] == nil {
		t.Fatal(ds)
	}
	part := pdf.Part{
		Part:       packet.Part{Ref: "source", Source: input, Kind: packet.PartSource, Pages: 1, FirstPage: 1},
		Occurrence: &pdf.Occurrence{File: &facts, Facts: views[0]},
		Input:      pdffixture.SourceInput(data),
	}
	job := pdf.Assembly{Parts: []pdf.Part{part}, Metadata: metadata}
	var output bytes.Buffer
	built, err := pdf.Assemble(t.Context(), job, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := built.VerifyReader(t.Context(), bytes.NewReader(output.Bytes()), 1, job); err != nil {
		t.Fatal(err)
	}
	return built, job, output.Bytes()
}

func assertOutputReferenceRefusal(t *testing.T, err error, path, target string) {
	t.Helper()
	if err == nil {
		t.Fatal("strict reopen accepted the invalid reference")
	}
	finding := pdf.Diagnostic(err)
	if finding.Code != diag.OutputInvalid || finding.PDFLocation == nil ||
		finding.PDFLocation.Path != path || !strings.Contains(finding.Detail, "dangling output object "+target) {
		t.Fatal(finding)
	}
}

func TestReopenRejectsOpaqueWrongGenerationRegardlessOfInfo(t *testing.T) {
	for _, info := range []string{"absent", "direct", "indirect"} {
		t.Run(info, func(t *testing.T) {
			doc := opaqueReferenceFixture("AuditReference", "5 0 R", "(real source object)")
			var m *packet.Metadata
			switch info {
			case "direct":
				doc.Version = "2.0"
			case "indirect":
				title := "Staged reference witness"
				m = &packet.Metadata{Title: &title}
			}
			built, job, data := assembleReferenceWitness(t, doc, m)
			if info == "direct" {
				data = bytes.Replace(data, []byte("\ntrailer\n<<"), []byte("\ntrailer\n<</Info"+directCreationDate+" "), 1)
			}
			if err := pdf.Verify(t.Context(), data, 1); err != nil {
				t.Fatalf("clean output: %v", err)
			}
			ref := regexp.MustCompile(`/AuditReference ([0-9]+) 0 R\b`).FindSubmatch(data)
			if len(ref) != 2 || bytes.Count(data, ref[0]) != 1 {
				t.Fatal("missing unique staged reference")
			}
			number := string(ref[1])
			altered := bytes.Replace(data, ref[0], []byte("/AuditReference "+number+" 1 R"), 1)
			if len(altered) != len(data) {
				t.Fatal("mutation moved xref offsets")
			}
			path := filepath.Join(t.TempDir(), "wrong-generation.pdf")
			if err := os.WriteFile(path, altered, 0600); err != nil {
				t.Fatal(err)
			}
			qpdfCheck(t, path)
			for generation, expected := range []string{"(real source object)", "null"} {
				got := string(run(t, qpdfTool, fmt.Sprintf("--show-object=%s,%d", number, generation), path))
				if strings.TrimSpace(got) != expected {
					t.Fatalf("qpdf object %s %d: %q, want %q", number, generation, got, expected)
				}
			}
			target := number + " 1 R"
			assertOutputReferenceRefusal(t, pdf.Verify(t.Context(), altered, 1), "AuditReference", target)
			assertOutputReferenceRefusal(t, built.VerifyReader(t.Context(), bytes.NewReader(altered), 1, job), "AuditReference", target)
		})
	}
}

func TestReopenChecksEffectiveTrailerReferences(t *testing.T) {
	for _, test := range []struct{ name, entry, path string }{
		{"indirect-info", "/Info 5 1 R", "Trailer.Info"},
		{"indirect-info-child", "/Info 5 0 R", "Private"},
		{"direct-info-child", "/Info << /Private 7 1 R >>", "Trailer.Info.Private"},
		{"id", "/ID [7 1 R (id)]", "Trailer.ID[0]"},
		{"extension", "/AdditionalStreams [7 1 R]", "Trailer.AdditionalStreams[0]"},
		{"private-trailer-entry", "/Private 7 1 R", "Trailer.Private"},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := opaqueReferenceFixture("Mark", "(page)", "<< /Private 7 1 R >>", "null", "(reference control)")
			data := bytes.Replace(doc.Bytes(), []byte("/Root 1 0 R >>\nstartxref"),
				[]byte("/Root 1 0 R "+test.entry+" >>\nstartxref"), 1)
			target := "7 1 R"
			switch test.name {
			case "indirect-info":
				target = "5 1 R"
			case "direct-info-child":
				// Direct Info is retained by the reader for a PDF 2.0 input.
				data = bytes.Replace(data, []byte("%PDF-1.7"), []byte("%PDF-2.0"), 1)
			}
			assertOutputReferenceRefusal(t, pdf.Verify(t.Context(), data, 1), test.path, target)
		})
	}
}

func TestReopenChecksStreamAndHybridTrailerReferences(t *testing.T) {
	base := opaqueReferenceFixture("Private", "(valid)").Bytes()
	for _, format := range []string{"stream", "hybrid"} {
		t.Run(format, func(t *testing.T) {
			data := freeHeadSourceRepresentation(base, format)
			// The stream starts at the same offset. A hybrid's classic
			// xref starts after this edit and needs its startxref adjusted.
			token := []byte("/Type /XRef /Size")
			replacement := []byte("/Type /XRef /PrivateWitness 20 0 R /Size")
			altered := bytes.Replace(data, token, replacement, 1)
			if format == "hybrid" {
				start := bytes.LastIndex(data, []byte("\nxref\n")) + 1
				altered = bytes.Replace(altered, fmt.Appendf(nil, "startxref\n%d\n", start),
					fmt.Appendf(nil, "startxref\n%d\n", start+len(replacement)-len(token)), 1)
			}
			assertOutputReferenceRefusal(t, pdf.Verify(t.Context(), altered, 1), "Trailer.PrivateWitness", "20 0 R")
		})
	}
}

func TestReopenAcceptsLiveNullAndDefinedNonzeroGeneration(t *testing.T) {
	for _, test := range []struct {
		name, ref, value, want string
		generationOne          bool
	}{
		{"live-null", "7 0 R", "null", "null", false},
		{"nonzero-generation", "7 1 R", "(original object)", "(original object)", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := opaqueReferenceFixture("AuditReference", test.ref,
				"<< /Unreachable 20 0 R >>", "null", test.value)
			data := doc.Bytes()
			generation := 0
			if test.generationOne {
				data = destinationOperandGenerationOne(data)
				generation = 1
			}
			path := filepath.Join(t.TempDir(), "valid.pdf")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			got := run(t, qpdfTool, "--show-object=7,"+strconv.Itoa(generation), path)
			if strings.TrimSpace(string(got)) != test.want {
				t.Fatal(string(got))
			}
			if err := pdf.Verify(t.Context(), data, 1); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReopenRejectsAReferenceToAFreeObject(t *testing.T) {
	doc := opaqueReferenceFixture("AuditReference", "5 1 R", "null")
	data := doc.Bytes()
	offset := bytes.Index(data, []byte("5 0 obj\n"))
	data = bytes.Replace(data, fmt.Appendf(nil, "%010d 00000 n ", offset), []byte("0000000000 00001 f "), 1)
	data = bytes.Replace(data, []byte("0000000000 65535 f "), []byte("0000000005 65535 f "), 1)
	assertOutputReferenceRefusal(t, pdf.Verify(t.Context(), data, 1), "AuditReference", "5 1 R")
}

func TestReopenIgnoresSupersededTrailerReferences(t *testing.T) {
	doc := opaqueReferenceFixture("Mark", "(page)")
	base := bytes.Replace(doc.Bytes(), []byte("/Root 1 0 R >>\nstartxref"),
		[]byte("/Root 1 0 R /PrivateWitness 20 0 R >>\nstartxref"), 1)
	newest := freeHeadSourceRepresentation(base, "incremental")
	at := bytes.LastIndex(newest, []byte("/Prev "))
	data := append(bytes.Clone(newest[:at]), []byte("/PrivateWitness (latest) ")...)
	data = append(data, newest[at:]...)
	if err := pdf.Verify(t.Context(), data, 1); err != nil {
		t.Fatal(err)
	}
}

func TestAssemblyReopenAcceptsAValidNonzeroGeneration(t *testing.T) {
	doc := opaqueReferenceFixture("AuditReference", "5 0 R", "(real source object)")
	built, job, data := assembleReferenceWitness(t, doc, nil)
	ref := regexp.MustCompile(`/AuditReference ([0-9]+) 0 R\b`).FindSubmatch(data)
	if len(ref) != 2 {
		t.Fatal("missing staged reference")
	}
	number := string(ref[1])
	header := []byte("\n" + number + " 0 obj\n")
	offset := bytes.Index(data, header) + 1
	if offset < 1 {
		t.Fatal("missing live object header")
	}
	changed := bytes.Replace(data, header, []byte("\n"+number+" 1 obj\n"), 1)
	changed = bytes.Replace(changed, ref[0], []byte("/AuditReference "+number+" 1 R"), 1)
	changed = bytes.Replace(changed, fmt.Appendf(nil, "%010d 00000 n \n", offset),
		fmt.Appendf(nil, "%010d 00001 n \n", offset), 1)
	path := filepath.Join(t.TempDir(), "live-generation.pdf")
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	qpdfCheck(t, path)
	if got := strings.TrimSpace(string(run(t, qpdfTool, "--show-object="+number+",1", path))); got != "(real source object)" {
		t.Fatalf("qpdf did not resolve the live generation: %q", got)
	}
	if err := pdf.Verify(t.Context(), changed, 1); err != nil {
		t.Fatal(err)
	}
	if err := built.VerifyReader(t.Context(), bytes.NewReader(changed), 1, job); err != nil {
		t.Fatal(err)
	}
}

func TestReopenChecksAReachableOlderXRefStreamDictionary(t *testing.T) {
	// The newest trailer overrides PrivateWitness. A page edge still
	// reaches the old xref stream dictionary and its original reference.
	doc := opaqueReferenceFixture("PrivateStream", "5 0 R")
	older := freeHeadSourceRepresentation(doc.Bytes(), "stream")
	xref := bytes.Index(older, []byte("5 0 obj\n"))
	older = bytes.Replace(older, []byte("/Type /XRef /Size"),
		[]byte("/Type /XRef /PrivateWitness 20 0 R /Size"), 1)
	data := append(bytes.Clone(older), []byte("6 0 obj\n(latest revision)\nendobj\n")...)
	data = fmt.Appendf(data, "xref\n6 1\n%010d 00000 n \ntrailer\n<< /Size 7 /Root 1 0 R /Prev %d /PrivateWitness (latest) >>\nstartxref\n%d\n%%%%EOF\n", len(older), xref, len(data))
	assertOutputReferenceRefusal(t, pdf.Verify(t.Context(), data, 1), "PrivateWitness", "20 0 R")
}

func TestReopenChecksAReachableObjectStreamDictionary(t *testing.T) {
	objects := layerStream("/Type /ObjStm /N 1 /First 4 /PrivateWitness 20 0 R", "7 0 (compressed object)")
	doc := opaqueReferenceFixture("PrivateStream", "5 0 R", string(objects))
	data := objectStreamXRefFixture(t, doc, 0)
	assertOutputReferenceRefusal(t, pdf.Verify(t.Context(), data, 1), "PrivateWitness", "20 0 R")
}

// Store object stream 5 and an xref stream 6 with compressed entries starting at 7.
func objectStreamXRefFixture(t *testing.T, doc *pdffixture.Doc, ordinals ...uint16) []byte {
	t.Helper()
	base := doc.Bytes()
	xref := bytes.Index(base, []byte("\nxref\n")) + 1
	records := make([]byte, 7*(6+len(ordinals)))
	for number := 1; number <= 6; number++ {
		offset := xref
		if number < 6 {
			offset = bytes.Index(base, fmt.Appendf(nil, "%d 0 obj\n", number))
		}
		if offset < 0 || offset > math.MaxUint32 {
			t.Fatal("fixture xref offset is invalid")
			return nil
		}
		record := records[(number-1)*7 : number*7]
		record[0] = 1
		binary.BigEndian.PutUint32(record[1:5], uint32(offset))
	}
	for i, ordinal := range ordinals {
		record := records[(6+i)*7 : (7+i)*7]
		record[0] = 2
		binary.BigEndian.PutUint32(record[1:5], 5)
		binary.BigEndian.PutUint16(record[5:], ordinal)
	}
	data := fmt.Appendf(bytes.Clone(base[:xref]),
		"6 0 obj\n<< /Type /XRef /Size %d /Root 1 0 R /Index [1 %d] /W [1 4 2] /Length %d >>\nstream\n",
		7+len(ordinals), 6+len(ordinals), len(records))
	data = append(data, records...)
	return fmt.Appendf(data, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", xref)
}
