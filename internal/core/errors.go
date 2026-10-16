// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"errors"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func textDiagnostic(err error, item string) diag.Diagnostic {
	code := diag.Code("font_invalid")
	var text *typeset.TextError
	var overflow *typeset.OverflowError
	switch {
	case errors.As(err, &text):
		code = "text_unsupported_character"
		if text.GlyphMissingOnly() {
			code = "text_glyph_missing"
		}
	case errors.Is(err, typeset.ErrShapingFailed):
		code = "assembly_failed"
	case errors.As(err, &overflow):
		code = "text_overflow"
	}
	return diag.New(code, err.Error(), item)
}
