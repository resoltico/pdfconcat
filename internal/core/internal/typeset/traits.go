// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import "fmt"

// FontTraits records independent original program metadata used by PDF descriptors.
type FontTraits struct {
	ItalicAngle                 float64
	ItalicOrOblique, FixedPitch bool
}

// Traits returns a value; pruning never replaces these original validated traits.
func (f *Font) Traits() FontTraits { return f.traits }
func readFontTraits(post, os2 []byte) (FontTraits, error) {
	if len(post) < 32 || len(os2) < 64 {
		return FontTraits{}, fmt.Errorf("%w: incomplete post or OS/2 metadata", ErrMalformedFont)
	}
	switch u32(post, 0) {
	case 0x10000, 0x20000, 0x25000, 0x30000:
	default:
		return FontTraits{}, fmt.Errorf("%w: unknown post format", ErrUnsupportedFont)
	}
	version := u16(os2, 0)
	if version > 5 {
		return FontTraits{}, fmt.Errorf("%w: unknown OS/2 version", ErrUnsupportedFont)
	}
	angle := u32(post, 4)
	// Convert signed 16.16 without narrowing an untrusted unsigned integer.
	signed := int64(angle)
	if angle&0x80000000 != 0 {
		signed -= 1 << 32
	}
	selection := u16(os2, 62)
	return FontTraits{ItalicAngle: float64(signed) / 65536, FixedPitch: u32(post, 12) != 0, ItalicOrOblique: selection&1 != 0 || version >= 4 && selection&(1<<9) != 0}, nil
}
