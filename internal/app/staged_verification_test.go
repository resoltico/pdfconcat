// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestFailedStagedVerificationPreservesIncumbent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	if err := os.WriteFile(target, []byte("incumbent"), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := files.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Abort() })
	size := packet.Size{Width: 300, Height: 400}
	assembly := pdf.Assembly{Parts: []pdf.Part{{
		Part: packet.Part{
			Leaf:  packet.Leaf{Kind: packet.PartPage, Ref: "generated", Count: 1},
			Pages: 1, FirstPage: 1, LastPage: 1, Size: size, OutputSize: size,
		},
		Repeat:    1,
		Generated: []compose.Page{{Width: size.Width, Height: size.Height}},
	}}}
	assembled, err := pdf.Assemble(t.Context(), assembly, stage)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := stage.Reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := assembled.VerifyReader(t.Context(), reader, 1, assembly); err != nil {
		t.Fatal(err)
	}
	if err := stage.Truncate(8); err != nil {
		t.Fatal(err)
	}
	roles := files.Roles{Output: target, Overwrite: true, InputsKnown: true}
	published, ds := verifyAndPublishPDF(t.Context(), stage, roles, assembled, 1, assembly)
	if published || stage.Published() || len(ds) != 1 || ds[0].Code != diag.OutputInvalid {
		t.Fatalf("publication = %v, findings = %v", published, ds)
	}
	if err := stage.Abort(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "incumbent" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	if _, err := os.Stat(stage.Path()); !os.IsNotExist(err) {
		t.Fatalf("abort left the stage: %v", err)
	}
}

func TestOptionalContentFaultOnRepeatedPagePreventsPublication(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(target, []byte("incumbent"), 0600); err != nil {
		t.Fatal(err)
	}
	const art = "0 0 100 100 re f"
	const wrapped = "/OC /Layer BDC " + art + " EMC"
	source := pdffixture.EncodedPages("layer", []pdffixture.EncodedStream{{Data: []byte(wrapped)}})
	source.Objs[3] = bytes.Replace(source.Objs[3], []byte("/Font"), []byte("/Properties << /Layer << /Type /OCG /Name (Layer) >> >> /Font"), 1)
	raw := source.Bytes()
	fitting := packet.DefaultFitPolicy(packet.Size{Width: 500, Height: 650})
	policy := packet.OptionalContentFlattenVisibleContent
	facts, occurrences, ds := pdf.InspectReader(t.Context(), bytes.NewReader(raw), sha256.Sum256(raw), int64(len(raw)),
		pdf.SourcePolicies{OptionalContent: policy}, []pdf.SelectionRequest{{Item: "first", Fitting: fitting}, {Item: "repeat", Fitting: fitting}})
	if hasErrors(ds) || len(occurrences) != 2 || occurrences[0] == nil || occurrences[1] == nil {
		t.Fatal(ds)
	}
	sourced := func(number int64, ref packet.Ref, occurrence *pdf.OccurrenceFacts) pdf.Part {
		return pdf.Part{
			Part: packet.Part{
				Leaf:  packet.Leaf{Kind: packet.PartSource, Ref: ref, Source: filepath.Join(dir, "source.pdf")},
				Pages: 1, FirstPage: number, LastPage: number,
				Mapping: []packet.SourceMapping{{SourceFirst: 1, SourceLast: 1, OutputFirst: number}},
			},
			Occurrence: &pdf.Occurrence{File: &facts, Facts: occurrence},
			Input:      pdffixture.SourceInput(raw),
		}
	}
	assembly := pdf.Assembly{OptionalContent: policy, Fitting: fitting, Parts: []pdf.Part{
		sourced(1, "first", occurrences[0]),
		{
			Part: packet.Part{
				Leaf:  packet.Leaf{Kind: packet.PartPage, Ref: "blank", Count: 1},
				Pages: 1, FirstPage: 2, LastPage: 2, OutputSize: packet.Size{Width: 500, Height: 650},
			},
			Repeat: 1, Generated: []compose.Page{{Width: 500, Height: 650}},
		},
		sourced(3, "repeat", occurrences[1]),
	}}

	stage, err := files.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Abort() })
	built, err := pdf.Assemble(t.Context(), assembly, stage)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := stage.Reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = built.VerifyReader(t.Context(), reader, 3, assembly); err != nil {
		t.Fatal(err)
	}
	// Supply a graph-clean stage with an undeclared lexical wrapper in a
	// fitting-style Form on the repeated physical page only.
	counterexample := pdffixture.EncodedPages("stage",
		[]pdffixture.EncodedStream{{Data: []byte(art)}},
		[]pdffixture.EncodedStream{{Data: []byte("")}},
		[]pdffixture.EncodedStream{{Data: []byte("q /Fit Do Q")}},
	)
	form := len(counterexample.Objs) + 1
	counterexample.Objs[7] = bytes.Replace(counterexample.Objs[7], []byte("/Font"),
		fmt.Appendf(nil, "/XObject << /Fit %d 0 R >> /Font", form), 1)
	counterexample.Objs = append(counterexample.Objs, fmt.Appendf(nil,
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(wrapped), wrapped))
	altered := counterexample.Bytes()
	if err := pdf.Verify(t.Context(), altered, 3); err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid ||
		!strings.Contains(pdf.Diagnostic(err).Detail, "contains an optional-content marked operator") {
		t.Fatalf("the independent packet-wide detection did not refuse the operator: %v", err)
	}
	if err := stage.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Write(altered); err != nil {
		t.Fatal(err)
	}
	roles := files.Roles{Output: target, Overwrite: true, InputsKnown: true}
	published, ds := verifyAndPublishPDF(t.Context(), stage, roles, built, 3, assembly)
	if published || stage.Published() || len(ds) != 1 || ds[0].Code != diag.OutputInvalid ||
		ds[0].PDFLocation == nil || ds[0].PDFLocation.Page != 3 || !strings.Contains(ds[0].Detail, "drawing stream") {
		t.Fatalf("publication = %v, findings = %v", published, ds)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "incumbent" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}
