// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

type headerCase struct {
	edit     func(tables map[string][]byte)
	unwanted string
	wanted   string
}

const (
	sfntTrueTypeSignature    = 0x00010000
	sfntTrueTypeTagSignature = 0x74727565
	directoryHeaderSize      = 12
	directoryRecordSize      = 16
	recordOffsetField        = 8
	maxTableCount            = 128
	glyphCountOffset         = 4
	headBBoxOffset           = 36
	unitsPerEmOffset         = 18
	hheaMetricCountOffset    = 34
	hmtxMetricSize           = 4
	locaEntrySize            = 4
	locaFormatOffset         = 50

	errTableTooShort  = "table too short"
	errNoGlyphs       = "no glyphs"
	errBadDirectory   = "bad table directory"
	errLocaOffsets    = "loca offsets"
	errUnitsPerEm     = "unitsPerEm"
	errTruncatedHead  = "truncated header"
	errHmtxShorter    = "hmtx shorter"
	errLocaShorter    = "loca table shorter"
	maxPostScriptSize = 63
)

func requiredTableTags() []string {
	return []string{headTableTag, "hhea", "hmtx", "maxp", "cmap", "glyf", "loca", os2TableTag}
}

// emptyRequiredTables has every required table, each of length zero.
func emptyRequiredTables() map[string][]byte {
	tables := map[string][]byte{}
	for _, tag := range requiredTableTags() {
		tables[tag] = []byte{}
	}

	return tables
}

func loadError(tb testing.TB, data []byte) error {
	tb.Helper()

	_, err := typeset.LoadFont(data)

	return err
}

func requireErrorMentioning(tb testing.TB, err error, want string) {
	tb.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		tb.Fatalf("error %v does not mention %q", err, want)
	}
}

func requireNoErrorMentioning(tb testing.TB, err error, unwanted string) {
	tb.Helper()

	if err != nil && strings.Contains(err.Error(), unwanted) {
		tb.Fatalf("error %q mentions %q", err, unwanted)
	}
}

func TestTableDirectoryBoundaries(t *testing.T) {
	t.Parallel()

	// A directory whose records all describe empty tables at the very end of the file.
	exact := buildSFNT(t, sfntTrueTypeTagSignature, emptyRequiredTables())

	zeroOffsets := buildSFNT(t, sfntTrueTypeSignature, emptyRequiredTables())
	for index := range len(requiredTableTags()) {
		binary.BigEndian.PutUint32(zeroOffsets[directoryHeaderSize+directoryRecordSize*index+recordOffsetField:], 0)
	}

	withExtraTables := func(extra int) []byte {
		tables := emptyRequiredTables()
		for index := range extra {
			tables[fmt.Sprintf("x%03d", index)] = []byte{}
		}

		return buildSFNT(t, sfntTrueTypeSignature, tables)
	}

	cases := map[string]struct {
		want string
		data []byte
	}{
		"empty tables at the end of the file": {errTableTooShort, exact},
		"directory one byte short":            {errBadDirectory, exact[:len(exact)-1]},
		"empty table at offset zero":          {errTableTooShort, zeroOffsets},
		"header without records":              {errBadDirectory, append([]byte{0, 1, 0, 0, 0, 0}, make([]byte, 6)...)},
		"header one byte short":               {errTruncatedHead, append([]byte{0, 1, 0, 0, 0, 0}, make([]byte, 5)...)},
		"maximum table count":                 {errTableTooShort, withExtraTables(maxTableCount - len(requiredTableTags()))},
		"one table above the maximum":         {errBadDirectory, withExtraTables(maxTableCount - len(requiredTableTags()) + 1)},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			requireErrorMentioning(t, loadError(t, tc.data), tc.want)
		})
	}
}

func TestBoundingBoxIsSigned(t *testing.T) {
	t.Parallel()

	want := [4]int{-32768, -1, 1, 32767}

	data := mutatedFont(t, func(tables map[string][]byte) {
		for index, value := range want {
			binary.BigEndian.PutUint16(tables[headTableTag][headBBoxOffset+2*index:], uint16Of(t, value&0xFFFF))
		}
	})

	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}

	if got := font.BBox(); got != want {
		t.Errorf("bbox %v, want %v", got, want)
	}
}

func TestLocaTableLengthAndLastEntry(t *testing.T) {
	t.Parallel()

	glyphs := func(tables map[string][]byte) int {
		return int(binary.BigEndian.Uint16(tables["maxp"][glyphCountOffset:]))
	}

	cases := map[string]struct {
		edit func(tables map[string][]byte)
		want string
	}{
		"one entry exactly per glyph and one more": {
			func(tables map[string][]byte) { tables["loca"] = tables["loca"][:(glyphs(tables)+1)*locaEntrySize] }, "",
		},
		"one byte short": {
			func(tables map[string][]byte) { tables["loca"] = tables["loca"][:(glyphs(tables)+1)*locaEntrySize-1] }, errLocaShorter,
		},
		"last entry beyond glyf": {
			func(tables map[string][]byte) {
				binary.BigEndian.PutUint32(tables["loca"][glyphs(tables)*locaEntrySize:], uint32Of(t, len(tables["glyf"])+1))
			}, errLocaOffsets,
		},
		"last entry before the previous one": {
			func(tables map[string][]byte) {
				binary.BigEndian.PutUint32(tables["loca"][glyphs(tables)*locaEntrySize:], 0)
			},
			errLocaOffsets,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := loadError(t, mutatedFont(t, tc.edit))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}

				return
			}

			requireErrorMentioning(t, err, tc.want)
		})
	}
}

func headerCases(tb testing.TB) map[string]headerCase {
	tb.Helper()

	put16 := func(table []byte, at, value int) { binary.BigEndian.PutUint16(table[at:], uint16Of(tb, value)) }

	return map[string]headerCase{
		"maxp of the minimum length": {
			edit:     func(tables map[string][]byte) { tables["maxp"] = tables["maxp"][:6] },
			unwanted: errTableTooShort,
		},
		"OS/2 of the minimum length": {
			edit:     func(tables map[string][]byte) { tables[os2TableTag] = tables[os2TableTag][:10] },
			unwanted: errTableTooShort,
		},
		"one glyph": {
			edit:     func(tables map[string][]byte) { put16(tables["maxp"], glyphCountOffset, 1) },
			unwanted: errNoGlyphs,
		},
		"smallest units per em": {
			edit:     func(tables map[string][]byte) { put16(tables[headTableTag], unitsPerEmOffset, 16) },
			unwanted: errUnitsPerEm,
		},
		"largest units per em": {
			edit:     func(tables map[string][]byte) { put16(tables[headTableTag], unitsPerEmOffset, 16384) },
			unwanted: errUnitsPerEm,
		},
		"units per em one below the smallest": {
			edit:   func(tables map[string][]byte) { put16(tables[headTableTag], unitsPerEmOffset, 15) },
			wanted: errUnitsPerEm,
		},
		"units per em one above the largest": {
			edit:   func(tables map[string][]byte) { put16(tables[headTableTag], unitsPerEmOffset, 16385) },
			wanted: errUnitsPerEm,
		},
		"hmtx exactly the horizontal metrics": {
			edit: func(tables map[string][]byte) {
				metrics := int(binary.BigEndian.Uint16(tables["hhea"][hheaMetricCountOffset:]))
				tables["hmtx"] = tables["hmtx"][:metrics*hmtxMetricSize]
			},
			unwanted: errHmtxShorter,
		},
		"hmtx one byte short": {
			edit: func(tables map[string][]byte) {
				metrics := int(binary.BigEndian.Uint16(tables["hhea"][hheaMetricCountOffset:]))
				tables["hmtx"] = tables["hmtx"][:metrics*hmtxMetricSize-1]
			},
			wanted: errHmtxShorter,
		},
	}
}

func TestHeaderFieldBoundaries(t *testing.T) {
	t.Parallel()

	for name, tc := range headerCases(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := loadError(t, mutatedFont(t, tc.edit))
			if tc.wanted != "" {
				requireErrorMentioning(t, err, tc.wanted)
			}

			if tc.unwanted != "" {
				requireNoErrorMentioning(t, err, tc.unwanted)
			}
		})
	}
}

func TestVerticalMetricsMayBeNegative(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		ascent, descent   int
		wantAscentDescent [2]float64
	}{
		{"descender below a small ascender", 100, -300, [2]float64{100, -300}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := mutatedFont(t, func(tables map[string][]byte) {
				zeroVerticalMetrics(tables)
				binary.BigEndian.PutUint16(tables["hhea"][4:], uint16Of(t, tc.ascent&0xFFFF))
				binary.BigEndian.PutUint16(tables["hhea"][6:], uint16Of(t, tc.descent&0xFFFF))
			})

			font, err := typeset.LoadFont(data)
			if err != nil {
				t.Fatal(err)
			}

			if got := [2]float64{font.Ascent(), font.Descent()}; got != tc.wantAscentDescent {
				t.Errorf("ascent and descent %v, want %v", got, tc.wantAscentDescent)
			}
		})
	}
}

func TestGlyphAdvanceBeyondLastGlyph(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)
	glyphs := int(binary.BigEndian.Uint16(sfntTables(t, font.Bytes())["maxp"][glyphCountOffset:]))

	if got := font.GlyphAdvance(uint16Of(t, glyphs)); got != 0 {
		t.Errorf("advance of glyph %d, one past the last, is %d", glyphs, got)
	}

	if got := font.GlyphAdvance(uint16Of(t, glyphs-1)); got < 0 {
		t.Errorf("advance of the last glyph is %d", got)
	}
}

// nameTableOf encodes 16-bit big-endian words.
func nameTableOf(words ...uint16) []byte {
	var table []byte
	for _, word := range words {
		table = binary.BigEndian.AppendUint16(table, word)
	}

	return table
}

func TestPostScriptNameRecordLayouts(t *testing.T) {
	t.Parallel()

	const (
		storageAfterHeader = 8
		recordWords        = 6
	)

	// The name "AB" is stored inside the first record's encoding and language fields: Unicode platform
	// strings are 16-bit characters whose high byte is zero.
	hidden := []uint16{0, 0x41, 0x42}

	recordEndsTable := nameTableOf(append([]uint16{0, 1, storageAfterHeader}, append(hidden, 6, 4, 0)...)...)

	// Two records are announced, but the second has only half of its twelve bytes in the table; the rest
	// of its bytes belong to the next table, "namf", which completes it into a PostScript-name record.
	partialRecord := nameTableOf(append([]uint16{0, 2, storageAfterHeader}, append(hidden, 4, 0, 0, 0, 0, 0)...)...)
	completion := nameTableOf(6, 4, 0, 0, 0, 0)

	// A record beyond the announced count would be the name.
	beyondCount := append(
		nameTableOf(0, 1, 6+2*recordWords*2, 3, 1, 0, 4, 0, 0, 1, 0, 0, 6, 4, 0),
		[]byte(testFontName)...,
	)

	longest := append(
		nameTableOf(0, 1, 6+recordWords*2, 1, 0, 0, 6, maxPostScriptSize, 0),
		[]byte(strings.Repeat("A", maxPostScriptSize))...)

	// The string lies past the table when the storage offset and the record offset are added, but not
	// when they are subtracted; the bytes past the table spell "Font".
	pastTable := nameTableOf(0, 1, 20, 3, 1, 0, 6, 8, 4, 0, 0, 0)
	spelling := []byte{0, 'F', 0, 'o', 0, 'n', 0, 't'}

	cases := map[string]struct {
		want   string
		table  []byte
		follow []byte
	}{
		"record ends at the table end":        {"AB", recordEndsTable, nil},
		"partial record after a whole one":    {fallbackFontName, partialRecord, completion},
		"record beyond the announced count":   {fallbackFontName, beyondCount, nil},
		"longest legal name":                  {strings.Repeat("A", maxPostScriptSize), longest, nil},
		"string past the table by its offset": {fallbackFontName, pastTable, spelling},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data := mutatedFont(t, func(tables map[string][]byte) {
				tables["name"] = tc.table
				if tc.follow != nil {
					tables["namf"] = tc.follow
				}
			})

			font, err := typeset.LoadFont(data)
			if err != nil {
				t.Fatal(err)
			}

			if font.PostScriptName() != tc.want {
				t.Errorf("name %q, want %q", font.PostScriptName(), tc.want)
			}
		})
	}
}

func TestPostScriptNameDropsDeleteCharacter(t *testing.T) {
	t.Parallel()

	table := append(nameTableOf(0, 1, 18, 1, 0, 0, 6, 4, 0), []byte("A~\x7fB")...)

	font, err := typeset.LoadFont(mutatedFont(t, func(tables map[string][]byte) { tables["name"] = table }))
	if err != nil {
		t.Fatal(err)
	}

	if font.PostScriptName() != "A~B" {
		t.Errorf("name %q", font.PostScriptName())
	}
}

// cmapOf encodes a Windows full-repertoire cmap of format 12 with the given groups, each a start
// character, an end character and the glyph of the start character.
func cmapOf(tb testing.TB, groups ...[3]uint32) []byte {
	tb.Helper()

	var table []byte

	table = binary.BigEndian.AppendUint16(table, 0)
	table = binary.BigEndian.AppendUint16(table, 1)
	table = binary.BigEndian.AppendUint16(table, 3)
	table = binary.BigEndian.AppendUint16(table, 10)
	table = binary.BigEndian.AppendUint32(table, directoryHeaderSize)
	table = binary.BigEndian.AppendUint16(table, 12)
	table = binary.BigEndian.AppendUint16(table, 0)
	table = binary.BigEndian.AppendUint32(table, uint32Of(tb, 16+12*len(groups)))
	table = binary.BigEndian.AppendUint32(table, 0)
	table = binary.BigEndian.AppendUint32(table, uint32Of(tb, len(groups)))

	for _, group := range groups {
		for _, value := range group {
			table = binary.BigEndian.AppendUint32(table, value)
		}
	}

	return table
}

func fontWithCmap(tb testing.TB, cmap []byte) (*typeset.Font, error) {
	tb.Helper()

	return typeset.LoadFont(mutatedFont(tb, func(tables map[string][]byte) { tables["cmap"] = cmap }))
}

func TestGlyphTextIsTheSmallestMappedCodePoint(t *testing.T) {
	t.Parallel()

	glyphs := uint32(binary.BigEndian.Uint16(sfntTables(t, defaultFontFile(t))["maxp"][glyphCountOffset:]))

	const (
		glyphForNul      = 4
		glyphForTwo      = 5
		glyphForMaximum  = 1
		glyphForBeyond   = 2
		glyphForNegative = 3
		maximumRune      = 0x10FFFF
		firstBeyondRune  = 0x110000
		negativeRune     = 0x80000000
	)

	font, err := fontWithCmap(t, cmapOf(t,
		[3]uint32{0, 0, glyphForNul},
		[3]uint32{'A', 'A', glyphs},
		[3]uint32{'B', 'B', glyphForTwo},
		[3]uint32{'C', 'C', glyphForTwo},
		[3]uint32{maximumRune, maximumRune, glyphForMaximum},
		[3]uint32{firstBeyondRune, firstBeyondRune, glyphForBeyond},
		[3]uint32{negativeRune, negativeRune, glyphForNegative},
	))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		want  string
		glyph uint32
	}{
		"code point zero":                     {"\x00", glyphForNul},
		"glyph one past the last":             {"", glyphs},
		"smaller of two code points":          {"B", glyphForTwo},
		"largest code point":                  {"\U0010FFFF", glyphForMaximum},
		"code point above the largest":        {"", glyphForBeyond},
		"code point that is negative as rune": {"", glyphForNegative},
	}

	for name, tc := range cases {
		if got := font.GlyphText(uint16Of(t, int(tc.glyph))); got != tc.want {
			t.Errorf("%s: glyph %d text %q, want %q", name, tc.glyph, got, tc.want)
		}
	}
}

func TestCmapOfEveryCodePointIsAccepted(t *testing.T) {
	t.Parallel()

	const maximumRune = 0x10FFFF

	font, err := fontWithCmap(t, cmapOf(t, [3]uint32{0, maximumRune, 0}))
	if err != nil {
		t.Fatal(err)
	}

	if font.GlyphText(1) != "\x01" {
		t.Errorf("glyph 1 text %q", font.GlyphText(1))
	}
}
