// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"context"
	"errors"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

// Diagnostic maps text and placement failures to product diagnostics.
func Diagnostic(err error, item string) diag.Diagnostic {
	code := diag.FontInvalid
	var placement placementError
	if errors.As(err, &placement) {
		return diag.New(diag.TextOverflow, err.Error(), item)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return diag.New(diag.Interrupted, err.Error(), item)
	}
	var text *typeset.TextError
	var overflow *typeset.OverflowError
	switch {
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
	return diag.New(code, err.Error(), item)
}
