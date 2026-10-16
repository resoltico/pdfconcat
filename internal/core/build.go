// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/publication"
	"github.com/resoltico/pdfconcat/internal/result"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// build owns the complete staging lifecycle, including abort on every failure.
type buildOutcome struct {
	output      result.Output
	renames     map[int][]job.FieldRename
	diagnostics []diag.Diagnostic
}

func build(c context.Context, j job.Job, prepared preparationOutcome, composition compositionOutcome, overwrite bool) buildOutcome {
	out := buildOutcome{output: result.Output{Path: j.Output}}
	layout, pages, facts, occurrences, fonts := composition.layout, composition.pages, prepared.facts, prepared.occurrences, prepared.fonts
	placements := placedTexts(pages)
	if j.PageNumbers != nil {
		digits, e := typeset.NewDigits(fonts["default"], j.PageNumbers.Size)
		if e != nil {
			out.diagnostics = append(out.diagnostics, textDiagnostic(e, "/page_numbers"))
			return out
		}
		union, e := digits.Place("0123456789", 10*digits.CellWidth, 0)
		if e != nil {
			out.diagnostics = append(out.diagnostics, textDiagnostic(e, "/page_numbers"))
			return out
		}
		placements = append(placements, union)
	}
	embedded, err := typeset.PrepareFonts(placements)
	if err != nil {
		out.diagnostics = append(out.diagnostics, diag.New("font_invalid", err.Error()))
		return out
	}
	if err = c.Err(); err != nil {
		out.diagnostics = append(out.diagnostics, diag.New("interrupted", err.Error()))
		return out
	}
	staged, err := publication.Create(j.Output)
	if err != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic("output_write_failed", err, j.Output))
		return out
	}
	defer staged.Abort()
	hash := sha256.New()
	counter := &countWriter{Writer: staged}
	assembled, err := pdf.Assemble(c, layout, facts, occurrences, pages, j.FitTo, embedded, io.MultiWriter(counter, hash))
	out.diagnostics = append(out.diagnostics, assembled.Diagnostics...)
	out.renames = assembled.FieldsRenamed
	if err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	if err = pdf.Verify(c, staged.Path(), layout.TotalPages); err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	if err = c.Err(); err != nil {
		out.diagnostics = append(out.diagnostics, diag.New("interrupted", err.Error()))
		return out
	}
	published, err := staged.Commit(overwrite)
	out.output.Published = published
	if published {
		out.output.SHA256 = hex.EncodeToString(hash.Sum(nil))
		out.output.Bytes = knownCount(counter.n)
	}
	if err != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic("output_publish_failed", err, j.Output))
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
