// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
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
	var emitted bytes.Buffer
	assembled, err := pdf.Assemble(t.Context(), assembly, io.MultiWriter(stage, &emitted))
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
	published, ds := verifyAndPublishPDF(t.Context(), stage, roles, assembled, 1, assembly, testPDFIdentity(emitted.Bytes()))
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
	var emitted bytes.Buffer
	built, err := pdf.Assemble(t.Context(), assembly, io.MultiWriter(stage, &emitted))
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
	published, ds := verifyAndPublishPDF(t.Context(), stage, roles, built, 3, assembly, testPDFIdentity(emitted.Bytes()))
	if published || stage.Published() || len(ds) != 1 || ds[0].Code != diag.OutputInvalid ||
		ds[0].PDFLocation == nil || ds[0].PDFLocation.Page != 3 || !strings.Contains(ds[0].Detail, "drawing stream") {
		t.Fatalf("publication = %v, findings = %v", published, ds)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "incumbent" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}

func testPDFIdentity(data []byte) serializedPDFIdentity {
	return serializedPDFIdentity{size: int64(len(data)), digest: sha256.Sum256(data)}
}

func TestStagedByteWitnessRefusesSameSizeDifferentValidPDF(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "packet.pdf")
	roles := files.Roles{Output: target, Overwrite: true, InputsKnown: true}
	size := packet.Size{Width: 300, Height: 400}
	part := func(n int64, ref packet.Ref, color packet.Color) pdf.Part {
		return pdf.Part{
			Part: packet.Part{
				Leaf:  packet.Leaf{Kind: packet.PartPage, Ref: ref, Count: 1},
				Pages: 1, FirstPage: n, LastPage: n, Size: size, OutputSize: size,
			},
			Repeat:    1,
			Generated: []compose.Page{{Width: size.Width, Height: size.Height, Background: &color}},
		}
	}
	red, blue := packet.Color{R: 1}, packet.Color{B: 1}
	assembly := pdf.Assembly{Structure: packet.StructureDiscard, Parts: []pdf.Part{part(1, "first", red), part(2, "second", blue)}}
	alternative := assembly
	alternative.Parts = []pdf.Part{part(1, "first", blue), part(2, "second", red)}
	var changed bytes.Buffer
	if _, err := pdf.Assemble(t.Context(), alternative, &changed); err != nil {
		t.Fatal(err)
	}
	for _, swap := range []bool{true, false} {
		if err := os.WriteFile(target, []byte("incumbent"), 0600); err != nil {
			t.Fatal(err)
		}
		stage, err := files.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = stage.Abort() })
		var original bytes.Buffer
		assembled, err := pdf.Assemble(t.Context(), assembly, io.MultiWriter(stage, &original))
		if err != nil {
			t.Fatal(err)
		}
		emitted := testPDFIdentity(original.Bytes())
		if swap {
			if changed.Len() != original.Len() || bytes.Equal(original.Bytes(), changed.Bytes()) {
				t.Fatalf("fixture must change bytes at equal size, got %d and %d", original.Len(), changed.Len())
			}
			if err := assembled.VerifyReader(t.Context(), bytes.NewReader(changed.Bytes()), 2, assembly); err != nil {
				t.Fatalf("expected the structurally valid control to pass: %v", err)
			}
			if _, err := stage.WriteAt(changed.Bytes(), 0); err != nil {
				t.Fatal(err)
			}
		}
		published, findings := verifyAndPublishPDF(t.Context(), stage, roles, assembled, 2, assembly, emitted)
		saved, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if swap {
			if published || stage.Published() || len(findings) != 1 || findings[0].Code != diag.OutputInvalid ||
				!strings.Contains(findings[0].Message, "SHA-256") || string(saved) != "incumbent" {
				t.Fatalf("same-size substitution escaped the byte witness: %v %v", published, findings)
			}
		} else if !published || len(findings) != 0 || testPDFIdentity(saved) != emitted {
			t.Fatalf("unchanged output identity: %v %v", published, findings)
		}
	}
}

func TestStagedByteWitnessReportsLengthIOAndCancellation(t *testing.T) {
	const data = "stable staged bytes"
	emitted := testPDFIdentity([]byte(data))
	if err := verifyStagedBytes(t.Context(), bytes.NewReader([]byte(data+" suffix")), emitted); err == nil ||
		!strings.Contains(err.Error(), "assembler wrote") {
		t.Fatalf("length mismatch was not identified: %v", err)
	}

	fault := errors.New("synthetic backing file failure")
	for _, tc := range []struct {
		name, part string
		reader     *witnessFaultReader
	}{
		{"length seek", "measure", &witnessFaultReader{Reader: bytes.NewReader([]byte(data)), seek: io.SeekEnd, fault: fault}},
		{"rewind", "rewind", &witnessFaultReader{Reader: bytes.NewReader([]byte(data)), seek: io.SeekStart, fault: fault}},
		{"read", "read", &witnessFaultReader{Reader: bytes.NewReader([]byte(data)), seek: -1, fault: fault}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyStagedBytes(t.Context(), tc.reader, emitted)
			if !errors.Is(err, fault) || !strings.Contains(err.Error(), tc.part) {
				t.Fatalf("I/O cause lost: %v", err)
			}
		})
	}

	c, cancel := context.WithCancel(t.Context())
	defer cancel()
	readback := &witnessFaultReader{Reader: bytes.NewReader([]byte(data)), seek: -1, cancel: cancel}
	if err := verifyStagedBytes(c, readback, emitted); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during the sequential read: %v", err)
	}
}

type witnessFaultReader struct {
	*bytes.Reader
	seek   int
	fault  error
	cancel context.CancelFunc
}

func (r *witnessFaultReader) Seek(offset int64, whence int) (int64, error) {
	if whence == r.seek {
		return 0, r.fault
	}
	return r.Reader.Seek(offset, whence)
}

func (r *witnessFaultReader) Read(p []byte) (int, error) {
	if r.fault != nil {
		return 0, r.fault
	}
	if r.cancel != nil {
		r.cancel()
	}
	return r.Reader.Read(p)
}

func TestStageCleanupPreservesPrimaryFindingsAndReportsOperation(t *testing.T) {
	for _, tc := range []struct {
		role packet.DestinationRole
		code diag.Code
		word string
	}{
		{packet.OutputDestination, diag.OutputPublishFailed, "output PDF"},
		{packet.ReportDestination, diag.ReportWriteFailed, "report"},
	} {
		t.Run(tc.word, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "destination")
			stage, err := files.Create(target)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = stage.Abort() })
			// Closing the underlying file independently makes the eventual
			// stage cleanup's close fail while removal still succeeds.
			handle := stage.File
			if err := handle.Close(); err != nil {
				t.Fatal(err)
			}
			findings := []diag.Diagnostic{diag.New(diag.OutputInvalid, "synthetic pre-publication refusal")}
			abortStage(stage, tc.role, target, &findings)
			if len(findings) != 2 || findings[0].Code != diag.OutputInvalid || findings[1].Code != tc.code ||
				findings[1].File != target || !strings.Contains(findings[1].Message, tc.word) ||
				!strings.Contains(findings[1].Detail, "close staged file") {
				t.Fatal(findings)
			}
			if _, err := os.Stat(stage.Path()); !os.IsNotExist(err) {
				t.Fatalf("close error prevented cleanup: %v", err)
			}
		})
	}
}
