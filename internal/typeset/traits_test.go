// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestOriginalFontTraitsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		name               string
		angle              uint32
		pitch              uint32
		version, selection uint16
		italic, fixed      bool
		wantAngle          float64
	}{
		{"regular proportional", 0, 0, 4, 0, false, false, 0},
		{"italic zero angle", 0, 0, 4, 1, true, false, 0},
		{"angle does not imply style", 0xfff38000, 0, 4, 0, false, false, -12.5},
		{"mono non-one pitch", 0, 7, 4, 0, false, true, 0},
		{"old oblique bit ignored", 0, 0, 3, 512, false, false, 0},
		{"oblique boundary", 0xfff38000, 1, 4, 512, true, true, -12.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mutatedFont(t, func(m map[string][]byte) {
				binary.BigEndian.PutUint32(m["post"][4:], tc.angle)
				binary.BigEndian.PutUint32(m["post"][12:], tc.pitch)
				binary.BigEndian.PutUint16(m["OS/2"], tc.version)
				binary.BigEndian.PutUint16(m["OS/2"][62:], tc.selection)
			})
			font, err := typeset.LoadFont(data)
			if err != nil {
				t.Fatal(err)
			}
			traits := font.Traits()
			if traits.ItalicAngle != tc.wantAngle || traits.ItalicOrOblique != tc.italic || traits.FixedPitch != tc.fixed {
				t.Fatal(traits)
			}
			traits.FixedPitch = !traits.FixedPitch
			if font.Traits().FixedPitch != tc.fixed {
				t.Fatal("mutable traits")
			}
		})
	}
}
func TestConsumedFontMetadataRefusesMalformedAndUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name        string
		edit        func(map[string][]byte)
		unsupported bool
	}{
		{"missing post", func(m map[string][]byte) { delete(m, "post") }, false},
		{"short post", func(m map[string][]byte) { m["post"] = m["post"][:31] }, false},
		{"short OS2", func(m map[string][]byte) { m["OS/2"] = m["OS/2"][:63] }, false},
		{"unknown post", func(m map[string][]byte) { binary.BigEndian.PutUint32(m["post"], 0x40000) }, true},
		{"unknown OS2", func(m map[string][]byte) { binary.BigEndian.PutUint16(m["OS/2"], 6) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := typeset.LoadFont(mutatedFont(t, tc.edit))
			want := typeset.ErrMalformedFont
			if tc.unsupported {
				want = typeset.ErrUnsupportedFont
			}
			if !errors.Is(err, want) {
				t.Fatalf("%v want %v", err, want)
			}
		})
	}
}
