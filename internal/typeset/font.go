// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"

	gtfont "github.com/go-text/typesetting/font"
)

type (
	// Digest is a font's identity: the SHA-256 of its file bytes.
	Digest [sha256.Size]byte

	// Font is a validated, immutable static TrueType font.
	Font struct {
		textOf   map[gtfont.GID]rune
		face     *gtfont.Face
		name     string
		data     []byte
		advances []int
		ink      []Rect
		digest   Digest
		bbox     [4]int
		upem     int
		glyphs   int

		ascent, descent, capHeight float64

		fsType uint16
	}

	// EmbeddingError reports an OS/2 fsType that forbids embedding.
	EmbeddingError struct {
		FsType uint16
	}
)

const (
	sfntTrueType      = 0x00010000
	sfntTrueTypeTag   = 0x74727565
	sfntOpenTypeCFF   = 0x4F54544F
	sfntCollection    = 0x74746366
	headMagic         = 0x5F0F3CF5
	maxTables         = 128
	minUnitsPerEm     = 16
	maxUnitsPerEm     = 16384
	maxGlyphID        = 0xFFFF
	directoryHeader   = 12
	directoryRecord   = 16
	headLength        = 54
	hheaLength        = 36
	maxpLength        = 6
	os2Length         = 10
	fsTypeRestricted  = 0x0002
	fsTypeBitmapOnly  = 0x0200
	fsTypeUsageMask   = 0x000F
	maxPostScriptName = 63

	// Field offsets inside the tables read here.
	headMagicOffset     = 12
	headUnitsPerEm      = 18
	headBBox            = 36
	headLocaFormat      = 50
	hheaMetricCount     = 34
	maxpGlyphCount      = 4
	os2FsType           = 8
	recordOffsetField   = 8
	recordLengthField   = 12
	hmtxMetricSize      = 4
	locaShortEntry      = 2
	locaLongEntry       = 4
	locaFormatShort     = 0
	locaFormatLong      = 1
	signedHalfRange     = 0x8000
	int16Size           = 2
	nameFallback        = "EmbeddedFont"
	nameHeader          = 6
	nameRecordSize      = 12
	nameCountOffset     = 2
	nameStorageOffset   = 4
	nameRecordPlatform  = 0
	nameRecordID        = 6
	nameRecordLength    = 8
	nameRecordOffset    = 10
	nameIDPostScript    = 6
	namePlatformWindows = 3
	namePlatformUnicode = 0
	nameCharWidthWide   = 2
	nameCharWidthNarrow = 1
	asciiDelete         = 0x7f
	nameReservedChars   = "[](){}<>/%"
)

// Error implements error.
func (e *EmbeddingError) Error() string {
	return fmt.Sprintf("font OS/2 fsType 0x%04x does not permit embedding", e.FsType)
}

func requiredTables() []string {
	return []string{"head", "hhea", "hmtx", "maxp", "cmap", "glyf", "loca", "OS/2"}
}

// LoadFont validates data as an embeddable static TrueType font. It retains data, which the caller
// must not modify afterwards. It never panics on malformed input; every defect is an error.
func LoadFont(data []byte) (*Font, error) {
	var font *Font

	err := protect(ErrMalformedFont, func() error {
		var loadErr error

		font, loadErr = parseFont(data)

		return loadErr
	})
	if err != nil {
		return nil, err
	}

	return font, nil
}

func parseFont(data []byte) (*Font, error) {
	tables, err := readTableDirectory(data)
	if err != nil {
		return nil, err
	}

	font := &Font{data: data, digest: sha256.Sum256(data)}

	err = font.readTables(tables)
	if err != nil {
		return nil, err
	}

	err = font.parse()
	if err != nil {
		return nil, err
	}

	return font, nil
}

func readTableDirectory(data []byte) (map[string][]byte, error) {
	err := checkSignature(data)
	if err != nil {
		return nil, err
	}

	count := int(binary.BigEndian.Uint16(data[4:]))
	if count == 0 || count > maxTables || directoryHeader+directoryRecord*count > len(data) {
		return nil, fmt.Errorf("%w: bad table directory", ErrMalformedFont)
	}

	tables := make(map[string][]byte, count)

	for index := range count {
		tag, table, recordErr := readTableRecord(data, index)
		if recordErr != nil {
			return nil, recordErr
		}

		_, duplicate := tables[tag]
		if duplicate {
			return nil, fmt.Errorf("%w: duplicate table %q", ErrMalformedFont, tag)
		}

		tables[tag] = table
	}

	return tables, nil
}

func checkSignature(data []byte) error {
	if len(data) < directoryHeader {
		return fmt.Errorf("%w: truncated header", ErrMalformedFont)
	}

	switch magic := binary.BigEndian.Uint32(data); magic {
	case sfntTrueType, sfntTrueTypeTag:
		return nil
	case sfntOpenTypeCFF:
		return fmt.Errorf("%w: OpenType CFF; only TrueType glyf outlines are supported", ErrUnsupportedFont)
	case sfntCollection:
		return fmt.Errorf("%w: TrueType collection; supply a single-face .ttf", ErrUnsupportedFont)
	default:
		return fmt.Errorf("%w: unknown sfnt signature 0x%08x", ErrUnsupportedFont, magic)
	}
}

// readTableRecord returns the tag and bytes of the index-th table of the directory.
func readTableRecord(data []byte, index int) (string, []byte, error) {
	record := data[directoryHeader+directoryRecord*index:]
	tag := string(record[:4])
	offset := int(binary.BigEndian.Uint32(record[recordOffsetField:]))
	length := int(binary.BigEndian.Uint32(record[recordLengthField:]))

	if offset < 0 || length < 0 || offset > len(data) || length > len(data)-offset {
		return "", nil, fmt.Errorf("%w: table %q extends past end of file", ErrMalformedFont, tag)
	}

	return tag, data[offset : offset+length], nil
}

// checkTablePresence requires the tables the writer and layout use and rejects variable and CFF fonts.
func checkTablePresence(tables map[string][]byte) error {
	for _, tag := range requiredTables() {
		_, ok := tables[tag]
		if !ok {
			return fmt.Errorf("%w: missing required table %q", ErrMalformedFont, tag)
		}
	}

	for _, tag := range []string{"fvar", "CFF ", "CFF2"} {
		_, ok := tables[tag]
		if ok {
			return fmt.Errorf("%w: table %q present (only static TrueType glyf fonts are supported)", ErrUnsupportedFont, tag)
		}
	}

	return nil
}

func u16(data []byte, offset int) uint16 { return binary.BigEndian.Uint16(data[offset:]) }

func u32(data []byte, offset int) uint32 { return binary.BigEndian.Uint32(data[offset:]) }

// int16At reads a signed 16-bit value.
func int16At(data []byte, offset int) int {
	value := u16(data, offset)

	return int(value&(signedHalfRange-1)) - int(value&signedHalfRange)
}

// checkGlyphLocations verifies that loca has an entry per glyph and that the offsets are ordered and
// inside glyf.
func checkGlyphLocations(tables map[string][]byte, glyphs int, format uint16) error {
	entry := locaShortEntry

	switch format {
	case locaFormatShort:
	case locaFormatLong:
		entry = locaLongEntry
	default:
		return fmt.Errorf("%w: bad indexToLocFormat", ErrMalformedFont)
	}

	loca, glyf := tables["loca"], tables["glyf"]
	if len(loca) < (glyphs+1)*entry {
		return fmt.Errorf("%w: loca table shorter than glyph count", ErrMalformedFont)
	}

	previous := 0

	for glyph := 0; glyph <= glyphs; glyph++ {
		var offset int
		if entry == locaShortEntry {
			offset = locaShortEntry * int(u16(loca, locaShortEntry*glyph))
		} else {
			offset = int(u32(loca, locaLongEntry*glyph))
		}

		if offset < previous || offset > len(glyf) {
			return fmt.Errorf("%w: loca offsets are not ordered within glyf", ErrMalformedFont)
		}

		previous = offset
	}

	return nil
}

// postScriptName returns name ID 6 restricted to the characters legal in a PDF name, or a fixed
// fallback.
func postScriptName(table []byte) string {
	if len(table) < nameHeader {
		return nameFallback
	}

	count, storage := int(u16(table, nameCountOffset)), int(u16(table, nameStorageOffset))

	for index := 0; index < count && nameHeader+nameRecordSize*(index+1) <= len(table); index++ {
		name := decodePostScriptRecord(table, table[nameHeader+nameRecordSize*index:], storage)
		if name != "" && len(name) <= maxPostScriptName {
			return name
		}
	}

	return nameFallback
}

// decodePostScriptRecord returns the legal characters of the name record when it holds the PostScript
// name, or "" for another record.
func decodePostScriptRecord(table, record []byte, storage int) string {
	platform, id := u16(record, nameRecordPlatform), u16(record, nameRecordID)
	length, offset := int(u16(record, nameRecordLength)), int(u16(record, nameRecordOffset))

	if id != nameIDPostScript || storage+offset+length > len(table) {
		return ""
	}

	raw := table[storage+offset : storage+offset+length]

	step := nameCharWidthNarrow
	if platform == namePlatformWindows || platform == namePlatformUnicode {
		step = nameCharWidthWide
	}

	var out []byte

	for pos := 0; pos+step <= len(raw); pos += step {
		char := rune(raw[pos+step-1])
		if step == nameCharWidthWide && raw[pos] != 0 {
			char = 0
		}

		if char > ' ' && char < asciiDelete && !strings.ContainsRune(nameReservedChars, char) {
			out = append(out, byte(char))
		}
	}

	return string(out)
}

// Identity returns the SHA-256 of the font file.
func (f *Font) Identity() Digest { return f.digest }

// Bytes returns the font file. The caller must not modify it.
func (f *Font) Bytes() []byte { return f.data }

// PostScriptName returns the font's name restricted to characters legal in a PDF name.
func (f *Font) PostScriptName() string { return f.name }

// UnitsPerEm returns the font design units per em.
func (f *Font) UnitsPerEm() int { return f.upem }

// BBox returns the head table's xMin, yMin, xMax, yMax in font units.
func (f *Font) BBox() [4]int { return f.bbox }

// Ascent returns the typographic ascent in font units.
func (f *Font) Ascent() float64 { return f.ascent }

// Descent returns the typographic descent in font units; it is negative.
func (f *Font) Descent() float64 { return f.descent }

// CapHeight returns the capital height in font units, or the ascent when the font declares none.
func (f *Font) CapHeight() float64 { return f.capHeight }

// GlyphAdvance returns the nominal horizontal advance of a glyph in font units, or 0 for an unknown glyph.
func (f *Font) GlyphAdvance(glyph uint16) int {
	if int(glyph) >= len(f.advances) {
		return 0
	}

	return f.advances[glyph]
}

// GlyphText returns the text a glyph stands for on its own: the smallest code point the cmap maps
// to it, or "" when no code point maps to it.
func (f *Font) GlyphText(glyph uint16) string {
	r, ok := f.textOf[gtfont.GID(glyph)]
	if ok {
		return string(r)
	}

	return ""
}

// readTables checks table presence and the header fields the writer and layout rely on.
func (f *Font) readTables(tables map[string][]byte) error {
	err := checkTablePresence(tables)
	if err != nil {
		return err
	}

	head, hhea, maxp, os2 := tables["head"], tables["hhea"], tables["maxp"], tables["OS/2"]
	if len(head) < headLength || len(hhea) < hheaLength || len(maxp) < maxpLength || len(os2) < os2Length {
		return fmt.Errorf("%w: table too short", ErrMalformedFont)
	}

	err = f.readHead(head)
	if err != nil {
		return err
	}

	f.glyphs = int(u16(maxp, maxpGlyphCount))
	if f.glyphs < 1 {
		return fmt.Errorf("%w: no glyphs", ErrMalformedFont)
	}

	err = checkGlyphLocations(tables, f.glyphs, u16(head, headLocaFormat))
	if err != nil {
		return err
	}

	metrics := int(u16(hhea, hheaMetricCount))
	if metrics == 0 || metrics*hmtxMetricSize > len(tables["hmtx"]) {
		return fmt.Errorf("%w: hmtx shorter than numberOfHMetrics", ErrMalformedFont)
	}

	err = f.readEmbeddingPermission(os2)
	if err != nil {
		return err
	}

	f.name = postScriptName(tables["name"])

	return nil
}

// readEmbeddingPermission reads the OS/2 fsType and rejects fonts that forbid embedding.
func (f *Font) readEmbeddingPermission(os2 []byte) error {
	f.fsType = u16(os2, os2FsType)

	// Bits 0-3 are mutually exclusive usage levels; 2 is a restricted license. Bit 9 limits embedding
	// to bitmaps. Both forbid embedding the outlines.
	if f.fsType&fsTypeUsageMask == fsTypeRestricted || f.fsType&fsTypeBitmapOnly != 0 {
		return &EmbeddingError{FsType: f.fsType}
	}

	return nil
}

// readHead reads the font-wide metrics of the head table.
func (f *Font) readHead(head []byte) error {
	if u32(head, headMagicOffset) != headMagic {
		return fmt.Errorf("%w: bad head magic number", ErrMalformedFont)
	}

	f.upem = int(u16(head, headUnitsPerEm))
	if f.upem < minUnitsPerEm || f.upem > maxUnitsPerEm {
		return fmt.Errorf("%w: unitsPerEm %d out of range", ErrMalformedFont, f.upem)
	}

	for index := range f.bbox {
		f.bbox[index] = int16At(head, headBBox+int16Size*index)
	}

	return nil
}

// parse builds the shaping face, metrics, advances and the glyph-to-text table.
func (f *Font) parse() error {
	face, err := gtfont.ParseTTF(bytes.NewReader(f.data))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMalformedFont, err)
	}

	f.face = face

	extents, ok := face.FontHExtents()
	if !ok || extents.Ascender-extents.Descender <= 0 {
		return fmt.Errorf("%w: no usable vertical metrics", ErrMalformedFont)
	}

	f.ascent, f.descent = float64(extents.Ascender), float64(extents.Descender)

	f.capHeight = float64(face.LineMetric(gtfont.CapHeight))
	if f.capHeight <= 0 {
		f.capHeight = f.ascent
	}

	f.advances = make([]int, f.glyphs)
	f.ink = make([]Rect, f.glyphs)

	for glyph := range f.glyphs {
		f.advances[glyph] = int(face.HorizontalAdvance(gtfont.GID(glyph)))
		if extent, hasExtent := face.GlyphExtents(gtfont.GID(glyph)); hasExtent {
			f.ink[glyph] = Rect{
				X:      float64(extent.XBearing),
				Y:      float64(extent.YBearing + extent.Height),
				Width:  float64(extent.Width),
				Height: -float64(extent.Height),
			}
		}
	}

	return f.readCmap(face)
}

// readCmap records, for every glyph, the smallest code point that maps to it.
func (f *Font) readCmap(face *gtfont.Face) error {
	f.textOf = make(map[gtfont.GID]rune)

	// go-text's format-12 iterator trusts group ranges: a corrupted group with end < start iterates
	// about 2^32 times. A valid cmap has at most one entry per code point, so bound the walk and treat
	// overflow as malformed.
	steps := 0

	for entries := face.Cmap.Iter(); entries.Next(); {
		steps++
		if steps > unicode.MaxRune+1 {
			return fmt.Errorf("%w: cmap enumerates more entries than code points exist", ErrMalformedFont)
		}

		char, glyph := entries.Char()
		if char < 0 || char > unicode.MaxRune || int(glyph) >= f.glyphs {
			continue
		}

		old, seen := f.textOf[glyph]
		if !seen || char < old {
			f.textOf[glyph] = char
		}
	}

	return nil
}
