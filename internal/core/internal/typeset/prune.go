// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// EmbeddedFont holds packet-wide glyph resources. PDF writers must use the same
// Glyphs for widths and ToUnicode as well as the same Program for every item.
type EmbeddedFont struct {
	Font    *Font
	Program []byte
	Glyphs  []uint16
	Subset  bool // Program was pruned; false when licensing requires the full font.
}

// PrepareFonts unions shaped glyphs by font identity in deterministic GID order.
func PrepareFonts(placements []*Placed) (map[Digest]EmbeddedFont, error) {
	fonts := map[Digest]*Font{}
	used := map[Digest]map[uint16]bool{}
	for _, p := range placements {
		if p == nil || p.Font == nil || len(p.Lines) == 0 {
			continue
		}
		id := p.Font.Identity()
		fonts[id] = p.Font
		if used[id] == nil {
			used[id] = map[uint16]bool{0: true}
		}
		for _, line := range p.Lines {
			for _, cluster := range line.Clusters {
				for _, g := range cluster.Glyphs {
					used[id][g.ID] = true
				}
			}
		}
	}
	result := make(map[Digest]EmbeddedFont, len(fonts))
	for id, f := range fonts {
		glyphs := make([]uint16, 0, len(used[id]))
		for g := range used[id] {
			glyphs = append(glyphs, g)
		}
		slices.Sort(glyphs)
		program, err := f.Prune(glyphs)
		if err != nil {
			return nil, err
		}
		result[id] = EmbeddedFont{Font: f, Program: program, Glyphs: glyphs, Subset: f.fsType&fsTypeNoSubsetting == 0}
	}
	return result, nil
}

// Prune keeps glyph IDs and metrics unchanged and empties unused outlines.
// Shaping uses the original font; this program is solely for embedding in PDFs.
// Fonts whose license forbids subsetting are returned byte-for-byte unchanged.
func (f *Font) Prune(glyphs []uint16) ([]byte, error) {
	if f.fsType&fsTypeNoSubsetting != 0 {
		return f.data, nil
	}
	tables, err := readTableDirectory(f.data)
	if err != nil {
		return nil, err
	}
	originalLoca := tables["loca"]
	location := func(g int) int {
		if u16(tables["head"], headLocaFormat) == locaFormatShort {
			return int(u16(originalLoca, 2*g)) * 2
		}
		return int(u32(originalLoca, 4*g))
	}
	keep := make([]bool, f.glyphs)
	visiting := make([]bool, f.glyphs)
	var retain func(int, int) error
	retain = func(g, depth int) error {
		if g < 0 || g >= f.glyphs || depth > 64 {
			return fmt.Errorf("%w: composite glyph reference out of range or depth", ErrMalformedFont)
		}
		if visiting[g] {
			return fmt.Errorf("%w: composite glyph cycle", ErrMalformedFont)
		}
		if keep[g] {
			return nil
		}
		keep[g] = true
		visiting[g] = true
		raw := tables["glyf"][location(g):location(g+1)]
		if len(raw) > 0 {
			if len(raw) < 10 {
				return fmt.Errorf("%w: glyph %d header truncated", ErrMalformedFont, g)
			}
			if int16At(raw, 0) < 0 {
				for pos := 10; ; {
					if pos+4 > len(raw) {
						return fmt.Errorf("%w: composite glyph %d truncated", ErrMalformedFont, g)
					}
					flags, child := u16(raw, pos), u16(raw, pos+2)
					if err := retain(int(child), depth+1); err != nil {
						return err
					}
					pos += 4
					if flags&1 != 0 {
						pos += 4
					} else {
						pos += 2
					}
					switch {
					case flags&8 != 0:
						pos += 2
					case flags&64 != 0:
						pos += 4
					case flags&128 != 0:
						pos += 8
					}
					if pos > len(raw) {
						return fmt.Errorf("%w: composite transform truncated", ErrMalformedFont)
					}
					if flags&32 == 0 {
						break
					}
				}
			}
		}
		visiting[g] = false
		return nil
	}
	for _, g := range append([]uint16{0}, glyphs...) {
		if err = retain(int(g), 0); err != nil {
			return nil, err
		}
	}
	glyf := make([]byte, 0, len(tables["glyf"]))
	loca := make([]byte, 4*(f.glyphs+1))
	for g := range f.glyphs {
		offset := len(glyf)
		if offset > math.MaxUint32 {
			return nil, fmt.Errorf("%w: glyf exceeds the sfnt offset range", ErrMalformedFont)
		}
		binary.BigEndian.PutUint32(loca[4*g:], uint32(offset))
		if keep[g] {
			glyf = append(glyf, tables["glyf"][location(g):location(g+1)]...)
			for len(glyf)%4 != 0 {
				glyf = append(glyf, 0)
			}
		}
	}
	lastOffset := len(glyf)
	if lastOffset > math.MaxUint32 {
		return nil, fmt.Errorf("%w: glyf exceeds the sfnt offset range", ErrMalformedFont)
	}
	binary.BigEndian.PutUint32(loca[4*f.glyphs:], uint32(lastOffset))
	selected := map[string][]byte{}
	for _, tag := range []string{"head", "hhea", "maxp", "OS/2", "name", "hmtx", "cvt ", "fpgm", "prep"} {
		if table, ok := tables[tag]; ok {
			selected[tag] = slices.Clone(table)
		}
	}
	selected["glyf"], selected["loca"] = glyf, loca
	post := make([]byte, 32)
	copy(post, tables["post"])
	binary.BigEndian.PutUint32(post, 0x00030000)
	selected["post"] = post
	binary.BigEndian.PutUint16(selected["head"][headLocaFormat:], locaFormatLong)
	binary.BigEndian.PutUint32(selected["head"][8:], 0)
	return encodeSFNT(selected)
}

func fontChecksum(data []byte) uint32 {
	var sum uint32
	for len(data) >= 4 {
		sum += binary.BigEndian.Uint32(data)
		data = data[4:]
	}
	var tail [4]byte
	copy(tail[:], data)
	return sum + binary.BigEndian.Uint32(tail[:])
}

func encodeSFNT(tables map[string][]byte) ([]byte, error) {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	out := make([]byte, directoryHeader+directoryRecord*len(tags))
	binary.BigEndian.PutUint32(out, sfntTrueType)
	binary.BigEndian.PutUint16(out[4:], uint16(len(tags)))
	power, selector := 1, 0
	for power*2 <= len(tags) {
		power *= 2
		selector++
	}
	binary.BigEndian.PutUint16(out[6:], uint16(power*directoryRecord))
	binary.BigEndian.PutUint16(out[8:], uint16(selector))
	binary.BigEndian.PutUint16(out[10:], uint16((len(tags)-power)*directoryRecord))
	headOffset := 0
	for i, tag := range tags {
		table := tables[tag]
		tableLength := len(table)
		if tableLength > math.MaxUint32 {
			return nil, fmt.Errorf("%w: table exceeds the sfnt length range", ErrMalformedFont)
		}
		offset := len(out)
		if offset > math.MaxUint32 || uint64(offset)+uint64(len(table))+3 > math.MaxUint32 {
			return nil, fmt.Errorf("%w: font exceeds the sfnt offset range", ErrMalformedFont)
		}
		record := out[directoryHeader+directoryRecord*i:]
		copy(record, tag)
		binary.BigEndian.PutUint32(record[4:], fontChecksum(table))
		binary.BigEndian.PutUint32(record[8:], uint32(offset))
		binary.BigEndian.PutUint32(record[12:], uint32(tableLength))
		if tag == "head" {
			headOffset = len(out)
		}
		out = append(out, table...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	binary.BigEndian.PutUint32(out[headOffset+8:], 0xb1b0afba-fontChecksum(out))
	return out, nil
}
