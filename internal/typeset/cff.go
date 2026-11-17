// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"bytes"
	"fmt"

	"seehuhn.de/go/membudget"
	"seehuhn.de/go/postscript/cid"
	"seehuhn.de/go/sfnt/cff"
)

// OutlineKind identifies the PDF font-program contract.
type OutlineKind uint8

const (
	// OutlineTrueType uses glyf outlines and a FontFile2 program.
	OutlineTrueType OutlineKind = iota
	// OutlineCFF uses static CFF1 and an identity CIDFontType0C program.
	OutlineCFF
)

// Outline returns the validated font program kind.
func (f *Font) Outline() OutlineKind { return f.outline }

// EmbeddingProgram returns the complete program, without changing glyph IDs.
func (f *Font) EmbeddingProgram() []byte {
	if f.outline == OutlineCFF {
		return f.program
	}
	return f.data
}

// cidProgram uses a maintained CFF reader/writer to normalize both name-keyed
// and CID-keyed CFF to Adobe-Identity CIDs equal to the shaping glyph IDs.
// Full embedding preserves all glyphs and avoids inventing a CFF subsetter.
func cidProgram(data []byte, glyphs int, name string) ([]byte, error) {
	f, err := cff.Read(bytes.NewReader(data), membudget.New(128<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: CFF1: %w", ErrMalformedFont, err)
	}
	if f.FontName != name {
		return nil, fmt.Errorf("%w: CFF and OpenType PostScript names disagree", ErrMalformedFont)
	}
	if len(f.Glyphs) != glyphs {
		return nil, fmt.Errorf("%w: CFF/maxp glyph counts disagree", ErrMalformedFont)
	}
	ids := make([]cid.CID, glyphs)
	for i := range ids {
		ids[i] = cid.CID(i)
	}
	f.MakeCIDKeyed(&cid.SystemInfo{Registry: "Adobe", Ordering: "Identity", Supplement: 0}, ids)
	var out bytes.Buffer
	if err = f.Write(&out); err != nil {
		return nil, fmt.Errorf("%w: CFF1: %w", ErrMalformedFont, err)
	}
	return out.Bytes(), nil
}
