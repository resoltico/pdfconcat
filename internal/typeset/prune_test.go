// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

func glyphBytes(t *testing.T, tables map[string][]byte, g uint16) []byte {
	t.Helper()
	loca := tables["loca"]
	var a, b uint32
	if binary.BigEndian.Uint16(tables["head"][50:]) == 0 {
		a = 2 * uint32(binary.BigEndian.Uint16(loca[int(g)*2:]))
		b = 2 * uint32(binary.BigEndian.Uint16(loca[(int(g)+1)*2:]))
	} else {
		a = binary.BigEndian.Uint32(loca[int(g)*4:])
		b = binary.BigEndian.Uint32(loca[(int(g)+1)*4:])
	}
	return tables["glyf"][a:b]
}

func TestPrunedGlyphsMetricsAndChecksums(t *testing.T) {
	font := defaultFont(t)
	var shaper typeset.Shaper
	placed, err := shaper.Place(font, baseParams("Ā ļ ķ ā ļ Ελληνικά Кириллица Áé"))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := typeset.PrepareFonts([]*typeset.Placed{placed})
	if err != nil {
		t.Fatal(err)
	}
	embedded := prepared[font.Identity()]
	data := embedded.Program
	original, reduced := sfntTables(t, font.Bytes()), sfntTables(t, data)
	if !bytes.Equal(original["maxp"], reduced["maxp"]) || !bytes.Equal(original["hmtx"], reduced["hmtx"]) {
		t.Fatal("glyph count/metrics changed")
	}
	for _, tag := range []string{"cmap", "GPOS", "GSUB", "GDEF", "DSIG"} {
		if _, ok := reduced[tag]; ok {
			t.Fatalf("retained unnecessary table %s", tag)
		}
	}
	if binary.BigEndian.Uint32(reduced["post"]) != 0x00030000 || len(reduced["post"]) != 32 {
		t.Fatal("post is not format 3")
	}
	if len(data) >= len(font.Bytes())/3 {
		t.Fatalf("short text program is unexpectedly large: %d", len(data))
	}
	kept := map[uint16]bool{}
	// Independent fixture closure: inspect the original composite component records.
	var assertGlyph func(uint16)
	composites := 0
	assertGlyph = func(g uint16) {
		if kept[g] {
			return
		}
		kept[g] = true
		before, after := glyphBytes(t, original, g), glyphBytes(t, reduced, g)
		if !bytes.HasPrefix(after, before) || bytes.Count(after[len(before):], []byte{0}) != len(after)-len(before) {
			t.Fatalf("glyph %d outline changed", g)
		}
		if len(before) < 10 || binary.BigEndian.Uint16(before) < 0x8000 {
			return
		}
		composites++
		pos := 10
		for {
			flags := binary.BigEndian.Uint16(before[pos:])
			child := binary.BigEndian.Uint16(before[pos+2:])
			assertGlyph(child)
			pos += 4
			if flags&1 == 0 {
				pos += 2
			} else {
				pos += 4
			}
			if flags&8 != 0 {
				pos += 2
			}
			if flags&64 != 0 {
				pos += 4
			}
			if flags&128 != 0 {
				pos += 8
			}
			if flags&32 == 0 {
				break
			}
		}
	}
	assertGlyph(0)
	for _, g := range embedded.Glyphs {
		assertGlyph(g)
	}
	if composites == 0 {
		t.Fatal("fixture did not exercise composite closure")
	}
	count := binary.BigEndian.Uint16(original["maxp"][4:])
	removed := 0
	for g := range count {
		if !kept[g] {
			if len(glyphBytes(t, reduced, g)) != 0 {
				t.Fatalf("unused glyph %d retained", g)
			}
			removed++
		}
	}
	if removed < 1000 {
		t.Fatal("fixture did not exercise removal")
	}
	var whole uint32
	for i := 0; i < len(data); i += 4 {
		whole += binary.BigEndian.Uint32(data[i:])
	}
	if whole != 0xb1b0afba {
		t.Fatalf("whole font checksum %08x", whole)
	}
	for i := range int(binary.BigEndian.Uint16(data[4:])) {
		record := data[12+16*i:]
		table := slices.Clone(reduced[string(record[:4])])
		if string(record[:4]) == "head" {
			clear(table[8:12])
		}
		for len(table)%4 != 0 {
			table = append(table, 0)
		}
		var sum uint32
		for j := 0; j < len(table); j += 4 {
			sum += binary.BigEndian.Uint32(table[j:])
		}
		if sum != binary.BigEndian.Uint32(record[4:]) {
			t.Fatalf("table %s checksum", record[:4])
		}
	}
	if _, err = font.Prune([]uint16{65535}); !errors.Is(err, typeset.ErrMalformedFont) {
		t.Fatalf("bad GID accepted: %v", err)
	}
}

func TestNoSubsettingPermissionKeepsOriginal(t *testing.T) {
	data := mutatedFont(t, func(tables map[string][]byte) { binary.BigEndian.PutUint16(tables["OS/2"][8:], 0x0100) })
	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}
	program, err := font.Prune([]uint16{0, 1})
	if err != nil || !bytes.Equal(program, data) {
		t.Fatalf("no-subsetting bytes changed: %v", err)
	}
}

func TestJobWideFontUnionIsDeterministic(t *testing.T) {
	font := defaultFont(t)
	var s typeset.Shaper
	var placed []*typeset.Placed
	for _, text := range []string{"ABC", "Ελληνικά", "Кириллица"} {
		p, err := s.Place(font, baseParams(text))
		if err != nil {
			t.Fatal(err)
		}
		placed = append(placed, p)
	}
	a, err := typeset.PrepareFonts(placed)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(placed)
	b, err := typeset.PrepareFonts(append(placed, placed...))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || len(b) != 1 {
		t.Fatal("font identity not shared")
	}
	aa, bb := a[font.Identity()], b[font.Identity()]
	if !bytes.Equal(aa.Program, bb.Program) || !slices.Equal(aa.Glyphs, bb.Glyphs) || !slices.IsSorted(aa.Glyphs) {
		t.Fatal("union depends on page order or repetitions")
	}
	for _, p := range placed {
		for _, l := range p.Lines {
			for _, c := range l.Clusters {
				for _, g := range c.Glyphs {
					if !slices.Contains(aa.Glyphs, g.ID) {
						t.Fatalf("union lost %d", g.ID)
					}
				}
			}
		}
	}
}
