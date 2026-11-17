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
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// build owns the complete staging lifecycle, including abort on every failure.
type buildOutcome struct {
	output      outcome.Output
	renames     [][]packet.FieldRename
	diagnostics []diag.Diagnostic
}

func build(c context.Context, j packet.Plan, composition *composition, overwrite bool) buildOutcome {
	out := buildOutcome{output: outcome.Output{Path: j.Output()}}
	layout, assembly := composition.layout, composition.assembly
	var placements []*typeset.Placed
	for _, part := range assembly.Parts {
		for _, page := range part.Generated {
			for _, block := range page.Blocks {
				placements = append(placements, block.Text)
			}
		}
	}

	if assembly.NumberLayout != nil {
		union, e := assembly.NumberLayout.Union()
		if e != nil {
			out.diagnostics = append(out.diagnostics, compose.Diagnostic(e, "/page_numbers"))
			return out
		}
		placements = append(placements, union)
	}

	embedded, err := typeset.PrepareFonts(placements)
	if err != nil {
		out.diagnostics = append(out.diagnostics, diag.New(diag.FontInvalid, err.Error()))
		return out
	}
	if err = c.Err(); err != nil {
		out.diagnostics = append(out.diagnostics, diag.New(diag.Interrupted, err.Error()))
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
	// Acquire each original resource again at assembly. Repeated occurrences share
	// immutable input bytes; their PDF graphs and digest/admission gates remain independent.
	acquired := map[string][]byte{}
	for index := range assembly.Parts {
		part := &assembly.Parts[index]
		if part.Source == "" {
			continue
		}
		data, known := acquired[part.Source]
		if !known {
			var e error
			data, e = files.Read(c, part.Source, 0)
			if e != nil {
				d := ioDiagnostic(diag.SourceChanged, e, part.Source)
				d.Items = []string{part.Ref}
				out.diagnostics = append(out.diagnostics, d)
				return out
			}
			acquired[part.Source] = data
		}
		part.Data = data
	}
	assembled, err := pdf.Assemble(c, assembly, io.MultiWriter(counter, hash))
	out.diagnostics = append(out.diagnostics, assembled.Diagnostics...)
	out.renames = assembled.Renames
	if err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	data, readErr := files.Read(c, staged.Path(), 0)
	if readErr != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputInvalid, readErr, j.Output()))
		return out
	}
	if err = pdf.VerifyAssembly(c, data, layout.TotalPages, assembly); err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	if err = c.Err(); err != nil {
		out.diagnostics = append(out.diagnostics, diag.New(diag.Interrupted, err.Error()))
		return out
	}
	published, err := staged.Commit(overwrite)
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
