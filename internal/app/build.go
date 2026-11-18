// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func build(c context.Context, j packet.Plan, composition *composition, roles files.Roles) buildOutcome {
	out := buildOutcome{output: outcome.Output{Path: j.Output()}}
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
	defer staged.Abort()
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
	reader, readErr := files.OpenRegular(c, staged.Path(), 0)
	if readErr != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputInvalid, readErr, j.Output()))
		return out
	}
	err = pdf.VerifyAssemblyReader(c, reader, layout.TotalPages, assembly)
	closeErr := reader.Close()
	if err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	if closeErr != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputInvalid, closeErr, j.Output()))
		return out
	}
	if err = c.Err(); err != nil {
		out.diagnostics = append(out.diagnostics, diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error())))
		return out
	}
	if ds := roles.CheckOutput(); hasErrors(ds) {
		out.diagnostics = append(out.diagnostics, ds...)
		return out
	}
	published, err := staged.Commit(roles.Overwrite)
	out.output.Published = published
	if published {
		out.output.SHA256 = hex.EncodeToString(hash.Sum(nil))
		out.output.Bytes = knownCount(counter.n)
	}
	if err != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputPublishFailed, err, j.Output()))
	}
	return out
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
