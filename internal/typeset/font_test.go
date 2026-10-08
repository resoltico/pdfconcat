// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

const (
	defaultFontDigest = "f3961a9cde016d41a4879aecda1474d3a36d6bf54fa0e4643de029cc2248b0e8"
	defaultFontSize   = 431364
	fallbackFontName  = "EmbeddedFont"
)

func TestDefaultFontIdentity(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	identity := font.Identity()
	if got := hex.EncodeToString(identity[:]); got != defaultFontDigest {
		t.Errorf("identity %s, want pinned digest %s", got, defaultFontDigest)
	}

	if len(font.Bytes()) != defaultFontSize {
		t.Errorf("font size %d", len(font.Bytes()))
	}
}

func TestDefaultFontMetrics(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	if font.PostScriptName() != "NotoSans-Regular" || font.UnitsPerEm() != 1000 {
		t.Errorf("name %q upem %d", font.PostScriptName(), font.UnitsPerEm())
	}

	if font.Ascent() != 1069 || font.Descent() != -293 || font.CapHeight() != 714 {
		t.Errorf("ascent %v descent %v cap %v", font.Ascent(), font.Descent(), font.CapHeight())
	}

	if box := font.BBox(); box[0] >= 0 || box[2] <= 0 {
		t.Errorf("bbox %v", box)
	}
}

func TestUnknownGlyphsHaveNoAdvanceOrText(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	if font.GlyphAdvance(0xFFFF) != 0 {
		t.Error("unknown glyph must have no advance")
	}

	if font.GlyphText(0xFFFF) != "" || font.GlyphText(0) != "" {
		t.Error("glyphs without a cmap entry have no text")
	}
}

func TestLoadFontRejects(t *testing.T) {
	t.Parallel()

	good := defaultFontFile(t)
	tables := func(edit func(map[string][]byte)) []byte { return mutatedFont(t, edit) }
	patch := func(tag string, at int, value []byte) []byte {
		return tables(func(m map[string][]byte) { copy(m[tag][at:], value) })
	}
	be16 := func(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

	cases := map[string]struct {
		want string
		data []byte
	}{
		"empty":            {"truncated header", nil},
		"cff":              {"OpenType CFF", append([]byte("OTTO"), make([]byte, 64)...)},
		"collection":       {"collection", append([]byte("ttcf"), make([]byte, 64)...)},
		"woff":             {"unknown sfnt signature", append([]byte("wOFF"), make([]byte, 64)...)},
		"zero tables":      {"bad table directory", append([]byte{0, 1, 0, 0, 0, 0}, make([]byte, 64)...)},
		"directory past":   {"bad table directory", append([]byte{0, 1, 0, 0, 0, 100}, make([]byte, 64)...)},
		"table past end":   {"extends past end", good[:len(good)-10]},
		"missing glyf":     {`missing required table "glyf"`, tables(func(m map[string][]byte) { delete(m, "glyf") })},
		"missing cmap":     {`missing required table "cmap"`, tables(func(m map[string][]byte) { delete(m, "cmap") })},
		"variable":         {`"fvar" present`, tables(func(m map[string][]byte) { m["fvar"] = make([]byte, 16) })},
		"cff2":             {`"CFF2" present`, tables(func(m map[string][]byte) { m["CFF2"] = make([]byte, 16) })},
		"short head":       {"table too short", tables(func(m map[string][]byte) { m[headTableTag] = m[headTableTag][:20] })},
		"head magic":       {"head magic", patch(headTableTag, 12, []byte{0, 0, 0, 0})},
		"upem small":       {"unitsPerEm 8", patch(headTableTag, 18, be16(8))},
		"upem huge":        {"unitsPerEm 20000", patch(headTableTag, 18, be16(20000))},
		"no glyphs":        {"no glyphs", patch("maxp", 4, be16(0))},
		"loca format":      {"indexToLocFormat", patch(headTableTag, 50, be16(7))},
		"loca short":       {"loca table shorter", tables(func(m map[string][]byte) { m["loca"] = m["loca"][:10] })},
		"loca unordered":   {"loca offsets", patch("loca", 4, []byte{0xFF, 0xFF})},
		"loca past glyf":   {"loca offsets", tables(func(m map[string][]byte) { m["glyf"] = m["glyf"][:100] })},
		"hmtx short":       {"hmtx shorter", tables(func(m map[string][]byte) { m["hmtx"] = m["hmtx"][:8] })},
		"no h metrics":     {"hmtx shorter", patch("hhea", 34, be16(0))},
		"restricted":       {"does not permit embedding", patch(os2TableTag, 8, be16(0x0002))},
		"bitmap only":      {"does not permit embedding", patch(os2TableTag, 8, be16(0x0200))},
		"garbage cmap":     {"malformed font", tables(func(m map[string][]byte) { m["cmap"] = make([]byte, 40) })},
		"no vertical size": {"vertical metrics", tables(func(m map[string][]byte) { zeroVerticalMetrics(m) })},
		"cmap overflow":    {"cmap enumerates", tables(func(m map[string][]byte) { m["cmap"] = hugeFormat12Cmap() })},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f, err := typeset.LoadFont(tc.data)
			if err == nil || f != nil {
				t.Fatalf("accepted (err=%v)", err)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadFontDuplicateTable(t *testing.T) {
	t.Parallel()

	data := defaultFontFile(t)
	// Rename the second directory record to the first one's tag.
	copy(data[12+16:], data[12:16])

	if _, err := typeset.LoadFont(data); err == nil || !strings.Contains(err.Error(), "duplicate table") {
		t.Fatalf(gotValueFormat, err)
	}
}

func TestEmbeddingPermission(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		fsType uint16
		ok     bool
	}{{0, true}, {0x0004, true}, {0x0008, true}, {0x0100, true}, {0x0002, false}, {0x0202, false}, {0x0200, false}} {
		data := mutatedFont(t, func(m map[string][]byte) { binary.BigEndian.PutUint16(m[os2TableTag][8:], tc.fsType) })
		_, err := typeset.LoadFont(data)

		var embedding *typeset.EmbeddingError

		switch {
		case tc.ok && err != nil:
			t.Errorf("fsType 0x%04x rejected: %v", tc.fsType, err)
		case !tc.ok && !errors.As(err, &embedding):
			t.Errorf("fsType 0x%04x: want EmbeddingError, got %v", tc.fsType, err)
		case !tc.ok && embedding.FsType != tc.fsType:
			t.Errorf("fsType in error 0x%04x", embedding.FsType)
		default:
		}
	}
}

func TestPostScriptNameSanitizing(t *testing.T) {
	t.Parallel()

	nameTable := func(platform, id uint16, raw []byte, count uint16) []byte {
		table := binary.BigEndian.AppendUint16(nil, 0)
		table = binary.BigEndian.AppendUint16(table, count)
		table = binary.BigEndian.AppendUint16(table, 18)
		table = binary.BigEndian.AppendUint16(table, platform)
		table = binary.BigEndian.AppendUint16(table, 1)
		table = binary.BigEndian.AppendUint16(table, 0)
		table = binary.BigEndian.AppendUint16(table, id)
		table = binary.BigEndian.AppendUint16(table, uint16Of(t, len(raw)))
		table = binary.BigEndian.AppendUint16(table, 0)

		return append(table, raw...)
	}
	utf16be := func(s string) []byte {
		var out []byte
		for _, r := range s {
			out = binary.BigEndian.AppendUint16(out, uint16Of(t, int(r)))
		}

		return out
	}

	cases := map[string]struct {
		want  string
		table []byte
	}{
		"windows":           {"My-Font", nameTable(3, 6, utf16be("My-Font"), 1)},
		"mac":               {"Mac-Font", nameTable(1, 6, []byte("Mac-Font"), 1)},
		"delimiters":        {"ABcd", nameTable(3, 6, utf16be("A B(c)/d%"), 1)},
		"non-latin":         {"A", nameTable(3, 6, utf16be("Aā"), 1)},
		"wrong id":          {fallbackFontName, nameTable(3, 4, utf16be("Other"), 1)},
		"too long":          {fallbackFontName, nameTable(3, 6, utf16be(strings.Repeat("A", 64)), 1)},
		"empty after clean": {fallbackFontName, nameTable(3, 6, utf16be("( )"), 1)},
		"string past end":   {fallbackFontName, nameTable(3, 6, utf16be(testFontName)[:4], 1)[:20]},
		"short table":       {fallbackFontName, []byte{0, 0}},
		"count too large":   {testFontName, nameTable(3, 6, utf16be(testFontName), 40)},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data := mutatedFont(t, func(m map[string][]byte) { m["name"] = tc.table })

			f, err := typeset.LoadFont(data)
			if err != nil {
				t.Fatal(err)
			}

			if f.PostScriptName() != tc.want {
				t.Errorf("name %q, want %q", f.PostScriptName(), tc.want)
			}
		})
	}
}

func TestCapHeightFallsBackToAscent(t *testing.T) {
	t.Parallel()

	data := mutatedFont(t, func(m map[string][]byte) { binary.BigEndian.PutUint16(m[os2TableTag][88:], 0) })

	f, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}

	if f.CapHeight() != f.Ascent() {
		t.Errorf("cap height %v, ascent %v", f.CapHeight(), f.Ascent())
	}
}

func zeroVerticalMetrics(m map[string][]byte) {
	hhea, os2 := m["hhea"], m[os2TableTag]
	for i := 4; i < 10; i++ {
		hhea[i] = 0
	}

	for _, at := range []int{68, 70, 72, 74, 76, 78} {
		os2[at], os2[at+1] = 0, 0
	}

	binary.BigEndian.PutUint16(os2[62:], 0) // fsSelection: no USE_TYPO_METRICS
}

// hugeFormat12Cmap is a Windows full-repertoire cmap whose only group claims 2^32 code points.
func hugeFormat12Cmap() []byte {
	var t []byte

	t = binary.BigEndian.AppendUint16(t, 0)
	t = binary.BigEndian.AppendUint16(t, 1)
	t = binary.BigEndian.AppendUint16(t, 3)
	t = binary.BigEndian.AppendUint16(t, 10)
	t = binary.BigEndian.AppendUint32(t, 12)
	t = binary.BigEndian.AppendUint16(t, 12)
	t = binary.BigEndian.AppendUint16(t, 0)
	t = binary.BigEndian.AppendUint32(t, 16+12)
	t = binary.BigEndian.AppendUint32(t, 0)
	t = binary.BigEndian.AppendUint32(t, 1)
	t = binary.BigEndian.AppendUint32(t, 0)
	t = binary.BigEndian.AppendUint32(t, 0xFFFFFFFF)

	return binary.BigEndian.AppendUint32(t, 1)
}

func TestShortLocaFormat(t *testing.T) {
	t.Parallel()

	data := mutatedFont(t, func(m map[string][]byte) {
		// A short table addresses at most 131,070 bytes: clamp the offsets and truncate glyf to match.
		// Validation does not read glyph outlines.
		const limit = 2 * 0xFFFF

		long := m["loca"]
		short := make([]byte, 0, len(long)/2)
		m["glyf"] = m["glyf"][:limit]

		for i := 0; i+4 <= len(long); i += 4 {
			offset := min(binary.BigEndian.Uint32(long[i:]), limit)
			short = binary.BigEndian.AppendUint16(short, uint16Of(t, int(offset/2)))
		}

		m["loca"] = short
		binary.BigEndian.PutUint16(m[headTableTag][50:], 0)
	})

	if _, err := typeset.LoadFont(data); err != nil {
		t.Fatal(err)
	}
}

func TestGlyphLookups(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	var shaper typeset.Shaper

	placed, err := shaper.Place(font, baseParams("a"))
	if err != nil {
		t.Fatal(err)
	}

	glyph := placed.Lines[0].Clusters[0].Glyphs[0]
	if font.GlyphAdvance(glyph.ID) != glyph.Advance || font.GlyphAdvance(glyph.ID) == 0 {
		t.Errorf("advance %d vs %d", font.GlyphAdvance(glyph.ID), glyph.Advance)
	}

	if font.GlyphText(glyph.ID) != "a" {
		t.Errorf("glyph text %q", font.GlyphText(glyph.ID))
	}
}
