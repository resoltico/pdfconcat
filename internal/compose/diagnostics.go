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
func Diagnostic(err error, item string) diag.Diagnostic { return diagnosticWidth(err, item, "", 0) }

// PageNumberDiagnostic distinguishes physical overlay placement from body layout.
func PageNumberDiagnostic(err error, page int64) diag.Diagnostic {
	return diagnosticWidth(err, "", "", page)
}

func diagnosticWidth(err error, item, widthOrigin string, page int64) diag.Diagnostic {
	var placement placementError
	if errors.As(err, &placement) {
		if item == "" {
			return diag.Report(diag.TextOverflow, diag.PageNumberPlacementFailure(page, err.Error()))
		}
		return diag.Report(diag.TextOverflow, diag.PageNumberPlacementFailure(page, err.Error()), item)
	}
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
		if widthOrigin != "" && implicatedWidth(overflow) {
			d := diag.Report(code, diag.GeneratedTextFailure(err.Error(), widthOrigin), item)
			d.AuthoredRef = widthOrigin
			return d
		}
	}
	return diag.Report(code, diag.GeneratedTextFailure(err.Error(), ""), item)
}

func implicatedWidth(overflow *typeset.OverflowError) bool {
	if overflow.FixedWidthExceedsPage() {
		return true
	}
	for _, finding := range overflow.Findings {
		if finding.Kind == typeset.FindingWordTooWide {
			return true
		}
	}
	return false
}
