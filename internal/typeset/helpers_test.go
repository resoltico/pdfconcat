// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

// sfntTables splits an sfnt file into its tables.
const (
	headTableTag       = "head"
	os2TableTag        = "OS/2"
	testFontName       = "Font"
	gotValueFormat     = "got %v"
	rightEdgeCase      = "right edge"
	messageValueFormat = "message %q"
)

func sfntTables(tb testing.TB, data []byte) map[string][]byte {
	tb.Helper()

	count := int(binary.BigEndian.Uint16(data[4:]))
	tables := map[string][]byte{}

	for index := range count {
		record := data[12+16*index:]
		offset, length := binary.BigEndian.Uint32(record[8:]), binary.BigEndian.Uint32(record[12:])
		tables[string(record[:4])] = slices.Clone(data[offset : offset+length])
	}

	return tables
}

// uint16Of converts a length to the 16 bits of an sfnt field.
func uint16Of(tb testing.TB, value int) uint16 {
	tb.Helper()

	if value < 0 || value > math.MaxUint16 {
		tb.Fatalf("%d does not fit 16 bits", value)

		return 0
	}

	return uint16(value)
}

// uint32Of converts a length to the 32 bits of an sfnt field.
func uint32Of(tb testing.TB, value int) uint32 {
	tb.Helper()

	if value < 0 || value > math.MaxUint32 {
		tb.Fatalf("%d does not fit 32 bits", value)

		return 0
	}

	return uint32(value)
}

// buildSFNT assembles a font file from tables with the given sfnt signature.
func buildSFNT(tb testing.TB, magic uint32, tables map[string][]byte) []byte {
	tb.Helper()

	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}

	slices.Sort(tags)

	header := make([]byte, 12+16*len(tags))
	binary.BigEndian.PutUint32(header, magic)
	binary.BigEndian.PutUint16(header[4:], uint16Of(tb, len(tags)))

	var body []byte

	for index, tag := range tags {
		record := header[12+16*index:]
		copy(record, tag)
		binary.BigEndian.PutUint32(record[8:], uint32Of(tb, len(header)+len(body)))
		binary.BigEndian.PutUint32(record[12:], uint32Of(tb, len(tables[tag])))
		body = append(body, tables[tag]...)

		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}

	return slices.Concat(header, body)
}

// mutatedFont returns the default font with edit applied to its tables.
func mutatedFont(tb testing.TB, edit func(tables map[string][]byte)) []byte {
	tb.Helper()

	tables := sfntTables(tb, defaultFontFile(tb))
	edit(tables)

	return buildSFNT(tb, 0x00010000, tables)
}

func defaultFontFile(tb testing.TB) []byte {
	tb.Helper()

	return slices.Clone(defaultFont(tb).Bytes())
}

func defaultFont(tb testing.TB) *typeset.Font {
	tb.Helper()

	font, err := typeset.LoadDefaultFont()
	if err != nil {
		tb.Fatal(err)
	}

	return font
}

// baseParams is a valid parameter set tests then vary.
func baseParams(text string) typeset.Params {
	return typeset.Params{
		Text: text, Size: 12, PageWidth: 200, PageHeight: 200,
		Anchor: typeset.AnchorTopLeft, LineSpacing: 1, Overflow: typeset.OverflowAllow,
	}
}
