// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

// build owns the complete staging lifecycle, including abort on every failure.
type buildOutcome struct {
	output      result.Output
	renames     [][]packet.FieldRename
	diagnostics []diag.Diagnostic
}

func build(c context.Context, j packet.Plan, composition *composition, overwrite bool) buildOutcome {
	out := buildOutcome{output: result.Output{Path: j.Output}}
	layout, assembly := composition.layout, composition.assembly
	var placements []*typeset.Placed
	for _, part := range assembly.Parts {
		for _, page := range part.Generated {
			for _, block := range page.Blocks {
				placements = append(placements, block.Text)
			}
		}
	}

	if assembly.Footer != nil {
		union, e := assembly.Footer.Union()
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
	staged, err := files.Create(j.Output)
	if err != nil {
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputWriteFailed, err, j.Output))
		return out
	}
	defer staged.Abort()
	hash := sha256.New()
	counter := &countWriter{Writer: staged}
	assembly.Fonts = embedded
	assembled, err := pdf.Assemble(c, assembly, io.MultiWriter(counter, hash))
	out.diagnostics = append(out.diagnostics, assembled.Diagnostics...)
	out.renames = assembled.Renames
	if err != nil {
		out.diagnostics = append(out.diagnostics, pdf.Diagnostic(err))
		return out
	}
	if err = pdf.Verify(c, staged.Path(), layout.TotalPages); err != nil {
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
		out.diagnostics = append(out.diagnostics, ioDiagnostic(diag.OutputPublishFailed, err, j.Output))
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
