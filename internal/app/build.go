// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/resoltico/pdfconcat/internal/compose"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

// build owns the complete staging lifecycle, including abort on every failure.
type buildOutcome struct {
	output      outcome.Output
	diagnostics []diag.Diagnostic
}

func build(c context.Context, j packet.Plan, composition *composition, roles files.Roles) (out buildOutcome) {
	out = buildOutcome{output: outcome.Output{Path: j.Output()}}
	layout, assembly := composition.layout, composition.assembly
	var pages []compose.Page
	for _, part := range assembly.Parts {
		pages = append(pages, part.Generated...)
	}
	embedded, err := compose.PrepareFonts(pages, assembly.NumberLayout)
	if err != nil {
		out.diagnostics = append(out.diagnostics, diag.Report(diag.FontInvalid, diag.FontPreparationFailure(err.Error())))
		return out
	}
	if err = c.Err(); err != nil {
		out.diagnostics = append(out.diagnostics, diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error())))
		return out
	}
	staged, err := files.Create(j.Output())
	if err != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputWriteFailed, err, j.Output()))
		return out
	}
	defer abortStage(staged, packet.OutputDestination, j.Output(), &out.diagnostics)
	hash := sha256.New()
	counter := &countWriter{Writer: staged}
	assembly.Fonts = embedded
	sources := assemblySources(assembly.Parts)
	defer sources.close()
	assembled, err := pdf.Assemble(c, assembly, io.MultiWriter(counter, hash))
	out.diagnostics = append(out.diagnostics, assembled.Diagnostics...)
	out.diagnostics = append(out.diagnostics, bindAssemblyFacts(assembly.Parts, assembled, err == nil)...)
	if hasErrors(out.diagnostics) {
		return out
	}
	if err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	emitted := serializedPDFIdentity{size: counter.n, digest: [sha256.Size]byte(hash.Sum(nil))}
	published, findings := verifyAndPublishPDF(c, staged, roles, assembled, layout.TotalPages, assembly, emitted)
	out.diagnostics = append(out.diagnostics, findings...)
	out.output.Published = published
	if published {
		out.output.SHA256 = hex.EncodeToString(emitted.digest[:])
		out.output.Bytes = knownCount(emitted.size)
	}
	return out
}

func verifyAndPublishPDF(c context.Context, staged *files.Staged, roles files.Roles, assembled pdf.Assembled, pages int64, assembly pdf.Assembly, emitted serializedPDFIdentity) (bool, []diag.Diagnostic) {
	reader, err := staged.Reader(c)
	if err != nil {
		return false, []diag.Diagnostic{ioDiagnostic(diag.OutputInvalid, err, roles.Output)}
	}
	if err = assembled.VerifyReader(c, reader, pages, assembly); err != nil {
		return false, []diag.Diagnostic{pdf.Diagnostic(err)}
	}
	// Take a fresh size observation through the same handle before the byte
	// witness. The PDF verifier's reader was bounded to its earlier length.
	readback, err := staged.Reader(c)
	if err == nil {
		err = verifyStagedBytes(c, readback, emitted)
	}
	if err != nil {
		return false, []diag.Diagnostic{ioDiagnostic(diag.OutputInvalid, err, roles.Output)}
	}
	if err = c.Err(); err != nil {
		return false, []diag.Diagnostic{diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error()))}
	}
	published, findings, err := roles.Publish(staged, packet.OutputDestination)
	if err != nil {
		findings = append(findings, publicationDiagnostic(packet.OutputDestination, roles.Output, roles.InputsKnown, published, err))
	}
	return published, findings
}

type serializedPDFIdentity struct {
	size   int64
	digest [sha256.Size]byte
}

// verifyStagedBytes reads a section bounded by Staged.Reader through the owned
// handle. The expected identity comes from the assembler's writer.
func verifyStagedBytes(c context.Context, readback io.ReadSeeker, emitted serializedPDFIdentity) error {
	if err := c.Err(); err != nil {
		return err
	}
	size, err := readback.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("measure staged PDF: %w", err)
	}
	if size != emitted.size {
		return fmt.Errorf("staged PDF has %d bytes, assembler wrote %d", size, emitted.size)
	}
	if _, err := readback.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind staged PDF: %w", err)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, stagedByteReader{context: c, reader: readback})
	if err != nil {
		return fmt.Errorf("read staged PDF: %w", err)
	}
	if n != emitted.size {
		return fmt.Errorf("read %d staged PDF bytes, assembler wrote %d", n, emitted.size)
	}
	if [sha256.Size]byte(hash.Sum(nil)) != emitted.digest {
		return errors.New("staged PDF SHA-256 differs from the assembler's output")
	}
	return c.Err()
}

type stagedByteReader struct {
	context context.Context
	reader  io.Reader
}

func (r stagedByteReader) Read(p []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type countWriter struct {
	io.Writer
	n int64
}

func (w *countWriter) Write(data []byte) (int, error) {
	n, err := w.Writer.Write(data)
	w.n += int64(n)
	return n, err
}

func bindAssemblyFacts(parts []pdf.Part, assembled pdf.Assembled, complete bool) []diag.Diagnostic {
	expected := make(map[packet.PartIdentity]bool, len(parts))
	for _, part := range parts {
		expected[part.Identity()] = true
	}
	failure := func() []diag.Diagnostic {
		return []diag.Diagnostic{diag.Report(diag.AssemblyFailed, diag.AssemblyFactsDoNotMatchTheirOwningParts())}
	}
	for key := range assembled.Parts {
		if !expected[key] {
			return failure()
		}
	}
	for index, part := range parts {
		fact, exists := assembled.Parts[part.Identity()]
		if complete && (!exists || !fact.Known) {
			return failure()
		}
		if exists && fact.Known && part.Kind == packet.PartSource {
			parts[index].FieldsRenamed = append([]packet.FieldRename{}, fact.Renames...)
		}
	}
	return nil
}
