// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"errors"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func textDiagnostic(err error, item string) diag.Diagnostic {
	code := diag.FontInvalid
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
