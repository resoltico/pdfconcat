// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestStrictZeroOffsetAdmission(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted bool
	}{{"clean", true}, {"unreferenced-zero", true}, {"incremental-shadow-zero", true}, {"referenced-zero", false}, {"unreachable-ref-zero", false}} {
		t.Run(tc.name, func(t *testing.T) {
			data := readPDF(t, "testdata/xref/"+tc.name+".pdf")
			conf, err := configuration()
			if err != nil {
				t.Fatal(err)
			}
			conf.ValidationMode = model.ValidationStrict
			raw, strictErr := api.ReadContext(t.Context(), bytes.NewReader(data), conf)
			if tc.name != "clean" && (strictErr == nil || !strings.Contains(strictErr.Error(), "in-use xref entry has offset 0")) {
				t.Fatalf("strict negative control: %v %+v", strictErr, raw)
			}
			source, _, ds, err := readSource(t.Context(), data, "")
			if !tc.accepted {
				if err == nil || !strings.Contains(err.Error(), "original reference to zero-offset object 6") {
					t.Fatalf("original-reference refusal: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "clean" {
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				return
			}
			count := 0
			for _, d := range ds {
				if d.Code == diag.SourceXRefEntryIgnored {
					count++
					if d.PDFLocation == nil || d.PDFLocation.Object != 6 {
						t.Fatal(d)
					}
				}
			}
			if count != 1 {
				t.Fatalf("accepted evidence: %+v", ds)
			}
			if entry := source.Table[6]; entry != nil && !entry.Free {
				t.Fatalf("older definition resurrected: %+v", entry)
			}
		})
	}
}

// Independently write classic xref syntax so reference checks cannot pass by
// sharing the production parser's object or offset representation.
func zeroReferenceFixture(body, streamDict, trailer string, catalogEntries ...string) []byte {
	content := "BT /F1 12 Tf 50 700 Td (Synthetic xref evidence) Tj ET\n% 6 0 R\n"
	length := fmt.Sprintf("/Length %d", len(content))
	if strings.Contains(streamDict, "/Length") {
		length = ""
	}
	objects := []string{"<< /Type /Catalog /Pages 2 0 R " + strings.Join(catalogEntries, " ") + " >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", fmt.Sprintf("<< %s %s >>\nstream\n%sendstream", length, streamDict, content), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", "", body}
	if strings.Contains(body, "/Type /Page ") {
		objects[1] = "<< /Type /Pages /Kids [3 0 R 7 0 R] /Count 2 >>"
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		if object == "" {
			continue
		}
		offsets[index+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 8\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 8 /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", trailer, xref)
	return b.Bytes()
}

func TestOriginalZeroOffsetReferenceSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, body, stream, trailer string
		reject                      bool
	}{
		{"nested", "<< /Nested [<< /Bad 6 0 R >>] >>", "", "", true},
		{"different generation", "[6 77 R]", "", "", true},
		{"stream dictionary", "(unreferenced)", "/Probe 6 0 R", "", true},
		{"indirect length", "(unreferenced)", "/Length 6 0 R", "", true},
		{"trailer", "(unreferenced)", "", "/Probe [6 0 R]", true},
		{"string", "(6 0 R)", "", "", false},
		{"hex string", "<3620302052>", "", "", false},
		{"comment", "<< %6 0 R\n /Value 12 >>", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := zeroReferenceFixture(tc.body, tc.stream, tc.trailer)
			_, _, ds, err := readSource(t.Context(), data, "")
			if tc.reject {
				if err == nil {
					t.Fatalf("forbidden reference admitted: %+v", ds)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestZeroOffsetFixtureFilesUnchanged(t *testing.T) {
	// Assembly reads original bytes; no repair writes back into source files.
	data := readPDF(t, "testdata/xref/unreferenced-zero.pdf")
	path := t.TempDir() + "/original.pdf"
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := readSource(t.Context(), data, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, readPDF(t, path)) {
		t.Fatal("original changed")
	}
}

func appendClassicRevision(data []byte, entryKind string) []byte {
	matches := regexp.MustCompile(`startxref\s+([0-9]+)\s+%%EOF`).FindAllSubmatch(data, -1)
	previous := string(matches[len(matches)-1][1])
	result := append([]byte(nil), data...)
	entry := "0000000000 00000 n \n"
	switch entryKind {
	case "valid":
		offset := len(result)
		result = append(result, []byte("6 0 obj\n(NEW EFFECTIVE DEFINITION)\nendobj\n")...)
		entry = fmt.Sprintf("%010d 00000 n \n", offset)
	case "free":
		entry = "0000000000 00001 f \n"
	}
	offset := len(result)
	result = fmt.Appendf(result, "xref\n6 1\n%strailer\n<< /Size 7 /Root 1 0 R /Prev %s >>\nstartxref\n%d\n%%%%EOF\n", entry, previous, offset)
	return result
}
func TestZeroOffsetEffectiveRevisionPrecedence(t *testing.T) {
	base := readPDF(t, "testdata/xref/unreferenced-zero.pdf")
	for _, kind := range []string{"valid", "free", "zero"} {
		t.Run(kind, func(t *testing.T) {
			s, _, ds, err := readSource(t.Context(), appendClassicRevision(base, kind), "")
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, d := range ds {
				if d.Code == diag.SourceXRefEntryIgnored {
					count++
				}
			}
			want := 0
			if kind == "zero" {
				want = 1
			}
			if count != want {
				t.Fatalf("shadowed candidate evidence: %+v", ds)
			}
			if kind == "valid" && s.Table[6].Free {
				t.Fatal("new valid definition was lost")
			}
			if kind != "valid" && !s.Table[6].Free {
				t.Fatal("older definition returned")
			}
		})
	}
}
func TestExceptionalStreamRevisionRefused(t *testing.T) {
	tools := oracle.RequireTools(t)
	output := t.TempDir() + "/streams.pdf"
	if raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--object-streams=generate", "testdata/xref/clean.pdf", output).CombinedOutput(); err != nil {
		t.Fatalf("valid compressed control: %v %s", err, raw)
	}
	data := readPDF(t, output)
	if _, _, _, err := readSource(t.Context(), data, ""); err != nil {
		t.Fatalf("valid stream source regressed: %v", err)
	}
	matches := regexp.MustCompile(`startxref\s+([0-9]+)\s+%%EOF`).FindAllSubmatch(data, -1)
	previous := string(matches[len(matches)-1][1])
	offset := len(data)
	exceptional := fmt.Appendf(append([]byte(nil), data...), "xref\n100 1\n0000000000 00000 n \ntrailer\n<< /Size 101 /Root 1 0 R /Prev %s >>\nstartxref\n%d\n%%%%EOF\n", previous, offset)
	if _, _, _, err := readSource(t.Context(), exceptional, ""); err == nil || !strings.Contains(err.Error(), "exclusively classic") {
		t.Fatalf("exceptional combined-table proof was assumed: %v", err)
	}
}
func TestReferenceOnOmittedPageStillRefused(t *testing.T) {
	data := zeroReferenceFixture("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Probe 6 0 R >>", "", "")
	_, _, ds := Inspect(t.Context(), data, "", []SelectionRequest{{Item: "kept", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
	if diag.Outcome(ds) != "invalid" || len(ds) == 0 || ds[len(ds)-1].Code != diag.SourceInvalid || ds[len(ds)-1].PDFLocation == nil || ds[len(ds)-1].PDFLocation.Object != 7 {
		t.Fatalf("omitted original page reference lost: %+v", ds)
	}
}

func TestEstablishedXRefFindingSurvivesLaterSignatureRefusal(t *testing.T) {
	data := zeroReferenceFixture("(unused)", "", "", "/Perms << >>")
	_, _, ds, err := readSource(t.Context(), data, "")
	if err == nil || diagnostic(err, diag.SourceInvalid).Code != diag.SourceSigned || len(ds) != 1 || ds[0].Code != diag.SourceXRefEntryIgnored {
		t.Fatalf("later refusal erased established entry evidence: %+v %v", ds, err)
	}
}
