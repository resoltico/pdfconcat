// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package layout

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestTextDeclarationUsesFontOnlyForInvalidFontFault(t *testing.T) {
	valueOrigin, fontOrigin := assembly.Origin{Ref: 7}, assembly.Origin{Ref: 8}
	for _, hasFont := range []bool{false, true} {
		for _, code := range []assembly.Code{CodeFontInvalid, CodeTextUnsupported} {
			text := assembly.TextStyle{Value: assembly.Set("text", valueOrigin)}
			if hasFont {
				text.Font = assembly.Set(assembly.Font{File: "font.ttf"}, fontOrigin)
			}
			origin, member := textDeclaration(&text, assembly.Origin{Ref: 9}, code)
			wantOrigin, wantMember := valueOrigin, textValuePointer
			if hasFont && code == CodeFontInvalid {
				wantOrigin, wantMember = fontOrigin, fontPointer
			}
			if origin != wantOrigin || member != wantMember {
				t.Errorf("font=%t code=%s got %v %s want %v %s", hasFont, code, origin, member, wantOrigin, wantMember)
			}
		}
	}
}
