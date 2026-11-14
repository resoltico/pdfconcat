// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"seehuhn.de/go/membudget"
	"seehuhn.de/go/postscript/cid"
	"seehuhn.de/go/sfnt/cff"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func TestStaticCFFSerialization(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fonts/source-serif/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}
	if font.Outline() != typeset.OutlineCFF {
		t.Fatal("CFF not recognized")
	}
	page := textPage(t, font, "Rīgas office café")
	path, first := writePDF(t, []compose.Page{page})
	var second bytes.Buffer
	if _, err = pdf.Write(context.Background(), &second, []compose.Page{page}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second.Bytes()) {
		t.Fatal("serialization differs")
	}
	if text := extractRaw(t, path); len(text) != 1 || !strings.Contains(text[0], "Rīgas office café") {
		t.Fatalf("CFF extraction: %q", text)
	}
	if err := (rasterOracle{dpi: 144, page: page}).check(t, path); err != nil {
		t.Fatal(err)
	}
	for _, d := range qpdfDictionaries(t, path) {
		if d["/Subtype"] == "/CIDFontType0" {
			return
		}
	}
	t.Fatal("no CIDFontType0 resource")
}

// cidKeyedFixture retains the official font's glyphs, cmap and metrics while
// assigning deliberately nonidentity CIDs. This is a test input, not the output
// conversion under test; the renderer must still see the original glyphs.
func cidKeyedFixture(t *testing.T, data []byte) []byte {
	t.Helper()
	count := int(binary.BigEndian.Uint16(data[4:6]))
	tables := map[string][]byte{}
	for i := range count {
		record := data[12+16*i:]
		offset, length := int(binary.BigEndian.Uint32(record[8:])), int(binary.BigEndian.Uint32(record[12:]))
		tables[string(record[:4])] = bytes.Clone(data[offset : offset+length])
	}
	font, err := cff.Read(bytes.NewReader(tables["CFF "]), membudget.New(128<<20))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]cid.CID, len(font.Glyphs))
	next := cid.CID(1)
	for i := len(ids) - 1; i > 0; i-- {
		ids[i] = next
		next++
	}
	font.MakeCIDKeyed(&cid.SystemInfo{Registry: "Adobe", Ordering: "Identity", Supplement: 0}, ids)
	var program bytes.Buffer
	if err = font.Write(&program); err != nil {
		t.Fatal(err)
	}
	tables["CFF "] = program.Bytes()
	checksum := func(data []byte) uint32 {
		var sum uint32
		for i := 0; i < len(data); i += 4 {
			var word [4]byte
			copy(word[:], data[i:min(i+4, len(data))])
			sum += binary.BigEndian.Uint32(word[:])
		}
		return sum
	}
	binary.BigEndian.PutUint32(tables["head"][8:], 0)
	keys := make([]string, 0, len(tables))
	for name := range tables {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	out := make([]byte, 12+16*count)
	copy(out, data[:12])
	headOffset := 0
	for i, name := range keys {
		table := tables[name]
		offset := len(out)
		tableLength := len(table)
		if offset > math.MaxUint32 || tableLength > math.MaxUint32 {
			t.Fatal("fixture table exceeds SFNT range")
			return nil
		}
		record := out[12+16*i:]
		copy(record, name)
		binary.BigEndian.PutUint32(record[4:], checksum(table))
		binary.BigEndian.PutUint32(record[8:], uint32(offset))
		binary.BigEndian.PutUint32(record[12:], uint32(tableLength))
		if name == "head" {
			headOffset = offset
		}
		out = append(out, table...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	binary.BigEndian.PutUint32(out[headOffset+8:], 0xb1b0afba-checksum(out))
	return out
}

func TestNameAndCIDKeyedCFFRenderTheSameGlyphs(t *testing.T) {
	original, err := os.ReadFile("../../testdata/fonts/source-serif/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	var reference bitmap
	for i, data := range [][]byte{original, cidKeyedFixture(t, original)} {
		font, err := typeset.LoadFont(data)
		if err != nil {
			t.Fatal(err)
		}
		page := textPage(t, font, "Office Rīgas café Ελληνικά")
		path, _ := writePDF(t, []compose.Page{page})
		qpdfCheck(t, path)
		image := renderGray(t, path)
		if i == 0 {
			reference = image
		} else if reference.sumAbsDiff(image) != 0 {
			t.Fatal("nonidentity CID input changed rendered glyphs")
		}
		if text := extractRaw(t, path); len(text) != 1 || !strings.Contains(text[0], "Office Rīgas café Ελληνικά") {
			t.Fatalf("CID extraction changed: %q", text)
		}
	}
}
