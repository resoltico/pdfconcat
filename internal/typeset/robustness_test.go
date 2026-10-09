// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

// byteStream is a deterministic pseudo-random sequence, a SHA-256 hash chain, so corrupted fonts are the
// same on every run and platform.
type byteStream struct {
	block [sha256.Size]byte
	used  int
}

const (
	panicProbeText = "Rīgas ļoti q̄ Ελληνικά ffi"
)

func newByteStream(seed byte) *byteStream {
	return &byteStream{block: sha256.Sum256([]byte{seed}), used: 0}
}

// next returns the next pseudo-random byte.
func (stream *byteStream) next() byte {
	if stream.used == len(stream.block) {
		stream.block = sha256.Sum256(stream.block[:])
		stream.used = 0
	}

	value := stream.block[stream.used]
	stream.used++

	return value
}

// below returns the next value in [0, limit).
func (stream *byteStream) below(limit uint32) int {
	var value uint32
	for range 4 {
		value = value<<8 | uint32(stream.next())
	}

	return int(value % limit)
}

// corruptedFont returns the default font with a few random bytes changed, biased towards the table
// directory and the layout tables. The sequence for a seed is stable.
func corruptedFont(tb testing.TB, stream *byteStream, data []byte) []byte {
	tb.Helper()

	corrupted := slices.Clone(data)

	for range 1 + stream.below(6) {
		position := stream.below(uint32Of(tb, len(corrupted)))
		if stream.below(2) == 0 {
			position = stream.below(1500)
		}

		if stream.below(3) == 0 {
			position = 0x1000 + stream.below(0x40000)
		}

		corrupted[position] = stream.next()
	}

	return corrupted
}

// panicFont applies captured corruptions to the pinned default font. The offsets come from actual
// parser failures; replaying them tests recovery without searching for a failing input on every run.
func panicFont(t *testing.T, changes map[int]byte) []byte {
	t.Helper()

	data := defaultFontFile(t)

	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != defaultFontDigest {
		t.Fatal("panic fixture requires the pinned default font")
	}

	for position, value := range changes {
		data[position] = value
	}

	return data
}

func TestParserPanicsBecomeErrors(t *testing.T) {
	t.Parallel()

	data := panicFont(t, map[int]byte{
		17203: 0x16, 128933: 0xc6, 152757: 0xa0,
		165203: 0xc3, 236299: 0x4a, 238790: 0xc7,
	})
	_, err := typeset.LoadFont(data)
	requireErrorMentioning(t, err, "parser panic")

	data = panicFont(t, map[int]byte{
		22: 0xe1, 1201: 0x7f, 19792: 0x0d, 132106: 0x84, 245452: 0x6b,
	})

	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}

	var shaper typeset.Shaper

	_, err = shaper.Place(font, baseParams(panicProbeText))
	requireErrorMentioning(t, err, "parser panic")

	// The same shaper keeps working after a recovered panic.
	_, err = shaper.Place(defaultFont(t), baseParams(panicProbeText))
	if err != nil {
		t.Fatalf("shaper unusable after panic: %v", err)
	}
}

func TestTruncatedAndCorruptedFontsAreErrors(t *testing.T) {
	t.Parallel()

	data := defaultFontFile(t)

	for size := 0; size < 4096; size += 61 {
		_, err := typeset.LoadFont(data[:size])
		if err == nil {
			t.Fatalf("truncation to %d bytes accepted", size)
		}
	}

	for _, size := range []int{len(data) / 2, len(data) - 1} {
		_, err := typeset.LoadFont(data[:size])
		if err == nil {
			t.Fatalf("truncation to %d bytes accepted", size)
		}
	}
}

func FuzzLoadFont(f *testing.F) {
	data := defaultFontFile(f)
	stream := newByteStream(1)

	f.Add(data[:12])
	f.Add([]byte("OTTO\x00\x00\x00\x00\x00\x00\x00\x00"))

	for range 3 {
		f.Add(corruptedFont(f, stream, data))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		var shaper typeset.Shaper

		start := time.Now()
		defer func() {
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Errorf("loading and placing took %v", elapsed)
			}
		}()

		font, err := typeset.LoadFont(input)
		if err != nil {
			if font != nil {
				t.Fatal("font returned together with an error")
			}

			return
		}

		// A font that loads must be usable: placement either succeeds or reports an error.
		placed, err := shaper.Place(font, baseParams(panicProbeText))
		if err == nil && placed == nil {
			t.Fatal("no result and no error")
		}

		if font.Identity() == ([32]byte{}) || font.UnitsPerEm() < 16 {
			t.Fatal("loaded font lacks identity or metrics")
		}
	})
}

// deletingGSUB is a GSUB table whose only feature, applied to all text, deletes glyph: a font may do this
// (a multiple substitution into zero glyphs), and the character then has no cluster left to carry it.
func deletingGSUB(glyph uint16) []byte {
	u16 := func(values ...uint16) []byte {
		out := make([]byte, 0, 2*len(values))
		for _, value := range values {
			out = binary.BigEndian.AppendUint16(out, value)
		}

		return out
	}

	const (
		scriptList  = 10
		featureList = scriptList + 2 + 6 + 4 + 8 // count, record, Script table, LangSys table
		lookupList  = featureList + 2 + 6 + 6    // count, record, Feature table
	)

	table := make([]byte, 0, 128)

	table = append(table, u16(1, 0, scriptList, featureList, lookupList)...)
	// ScriptList: one script, DFLT, whose default language system uses feature 0.
	table = append(table, u16(1)...)
	table = append(table, "DFLT"...)
	table = append(table, u16(8)...)
	table = append(table, u16(4, 0)...)
	table = append(table, u16(0, 0xFFFF, 1, 0)...)
	// FeatureList: one feature, ccmp, which runs lookup 0.
	table = append(table, u16(1)...)
	table = append(table, "ccmp"...)
	table = append(table, u16(8)...)
	table = append(table, u16(0, 1, 0)...)
	// LookupList: one multiple substitution whose only sequence is empty.
	table = append(table, u16(1, 4)...)
	table = append(table, u16(2, 0, 1, 8)...)
	table = append(table, u16(1, 8, 1, 14)...)
	table = append(table, u16(1, 1, glyph)...)
	table = append(table, u16(0)...)

	return table
}

func TestShapingThatDropsACharacterIsAnError(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	placed, err := shaper.Place(defaultFont(t), baseParams("a"))
	if err != nil {
		t.Fatal(err)
	}

	glyph := placed.Lines[0].Clusters[0].Glyphs[0].ID

	data := mutatedFont(t, func(tables map[string][]byte) { tables["GSUB"] = deletingGSUB(glyph) })

	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}

	// A paragraph whose every glyph the font deletes has no cluster left to carry its text.
	for _, text := range []string{"a", "aaa"} {
		_, err = shaper.Place(font, baseParams(text))
		if !errors.Is(err, typeset.ErrShapingFailed) || !strings.Contains(err.Error(), "dropped or reordered text") {
			t.Fatalf("shaping %q, all of which is deleted: %v", text, err)
		}
	}

	// Next to a glyph that stays, the deleted one leaves its character inside the neighbouring cluster, so
	// no text is lost and shaping succeeds.
	for _, text := range []string{"ab", "ba", "bab", "bc"} {
		_, err = shaper.Place(font, baseParams(text))
		if err != nil {
			t.Errorf("shaping %q: %v", text, err)
		}
	}
}
