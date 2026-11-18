// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"context"
	"errors"

	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// Diagnostic maps text and placement failures to product diagnostics.
func Diagnostic(err error, item string) diag.Diagnostic { return diagnosticWidth(err, item, "") }

// PageNumberDiagnostic distinguishes physical overlay placement from body layout.
func PageNumberDiagnostic(err error, page int64) diag.Diagnostic {
	var placement placementError
	if errors.As(err, &placement) {
		return diag.Report(diag.TextOverflow, diag.PageNumberPlacementFailure(page, err.Error()))
	}
	return Diagnostic(err, "")
}

func diagnosticWidth(err error, item, widthOrigin string) diag.Diagnostic {
	code := diag.FontInvalid
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error()), item)
	}
	var embedding *typeset.EmbeddingError
	var text *typeset.TextError
	var overflow *typeset.OverflowError
	switch {
	case errors.As(err, &embedding):
		code = diag.FontNotEmbeddable
	case errors.As(err, &text):
		code = diag.TextUnsupportedCharacter
		if text.GlyphMissingOnly() {
			code = diag.TextGlyphMissing
		}
	case errors.Is(err, typeset.ErrShapingFailed):
		code = diag.AssemblyFailed
	case errors.As(err, &overflow):
		code = diag.TextOverflow
	}
	return diag.Report(code, diag.GeneratedTextFailure(err.Error(), widthOrigin), item)
}
