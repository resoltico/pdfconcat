// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package genpage_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// glyphs are the glyphs of the default font that the tests place by hand.
	glyphs struct {
		a, b, space glyphRef
	}

	// glyphRef is one glyph of the default font with its source text.
	glyphRef struct {
		text string
		id   uint16
	}

	// contentRow is a hand-built set of lines and the exact content stream the writer must produce for it.
	contentRow struct {
		name  string
		want  string
		lines []typeset.Line
	}
)

const (
	// patchedUnitsPerEm is a units-per-em value other than 1,000, so that scaling font units to thousandths
	// of an em is not the identity.
	patchedUnitsPerEm = 2000
	headTag           = "head"
	unitsPerEmOffset  = 18
	tableRecordSize   = 16
	tableDirectory    = 12
	tableOffsetField  = 8
	numTablesField    = 4
	hangDeadline      = 20 * time.Second
	toUnicodeHeader   = "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	contentHead     = "0 0 0 rg\nBT\n/F1 10 Tf\n1 0 0 1 12.346 190.25 Tm\n"
	contentTail     = "ET\n"
	toUnicodeFooter = "endcmap\nCMapName currentdict /CMap defineresource pop\n" + "end\n" + "end\n"
)

var beginBfChar = regexp.MustCompile(`(\d+) beginbfchar\n`)

// halfEmFont returns the default font with a units-per-em of 2,000, which halves every thousandths-of-an-em value.
func halfEmFont(tb testing.TB) *typeset.Font {
	tb.Helper()

	data := append([]byte(nil), defaultFont(tb).Bytes()...)
	tables := int(binary.BigEndian.Uint16(data[numTablesField:]))

	for index := range tables {
		record := data[tableDirectory+tableRecordSize*index:]
		if string(record[:len(headTag)]) != headTag {
			continue
		}

		offset := binary.BigEndian.Uint32(record[tableOffsetField:])
		binary.BigEndian.PutUint16(data[offset+unitsPerEmOffset:], patchedUnitsPerEm)

		font, err := typeset.LoadFont(data)
		if err != nil {
			tb.Fatal(err)
		}

		return font
	}

	tb.Fatal("font has no head table")

	return nil
}

// handGlyphs shapes "A B" to learn the glyphs of A, space and B.
func handGlyphs(tb testing.TB) glyphs {
	tb.Helper()

	clusters := placed(tb, defaultFont(tb), params("A B")).Lines[0].Clusters
	ref := func(index int) glyphRef {
		return glyphRef{id: clusters[index].Glyphs[0].ID, text: clusters[index].Text}
	}

	return glyphs{a: ref(0), space: ref(1), b: ref(2)}
}

// cluster is one cluster of a single glyph with the given text, advancing by the font's own advance.
func cluster(font *typeset.Font, ref glyphRef, xOffset, yOffset int) typeset.Cluster {
	return typeset.Cluster{
		Text:   ref.text,
		Glyphs: []typeset.Glyph{{ID: ref.id, Advance: font.GlyphAdvance(ref.id), XOffset: xOffset, YOffset: yOffset}},
	}
}

func hex4(id uint16) string { return fmt.Sprintf("<%04X>", id) }

// objectStream returns the decoded data of stream object number of the file, read by qpdf.
func objectStream(tb testing.TB, path string, number int) string {
	tb.Helper()

	return string(run(tb, qpdfTool, "--show-object="+strconv.Itoa(number), "--filtered-stream-data", path))
}

// handLine is a line at a fixed position holding clusters, justified by extra points per interior space.
func handLine(extra float64, clusters ...typeset.Cluster) typeset.Line {
	return typeset.Line{X: 12.3456, Baseline: 190.25, ExtraSpace: extra, Clusters: clusters}
}

// offsetRows are the rows about glyph offsets, justification and rise.
func offsetRows(font *typeset.Font, hand glyphs) []contentRow {
	space, glyphA, glyphB := hex4(hand.space.id), hex4(hand.a.id), hex4(hand.b.id)
	plain := func(ref glyphRef) typeset.Cluster { return cluster(font, ref, 0, 0) }
	tenth, quarter := patchedUnitsPerEm/10, patchedUnitsPerEm/4

	return []contentRow{
		{
			"plain glyphs share one array", contentHead + "[" + glyphA + glyphB + "] TJ\n" + contentTail,
			[]typeset.Line{handLine(0, plain(hand.a), plain(hand.b))},
		},
		{
			"a glyph offset becomes a thousandths-of-an-em adjustment and is undone after",
			contentHead + "[" + glyphA + "-100" + glyphB + "100" + glyphA + "] TJ\n" + contentTail,
			[]typeset.Line{handLine(0, plain(hand.a), cluster(font, hand.b, tenth, 0), plain(hand.a))},
		},
		{
			"a negative glyph offset moves the other way", contentHead + "[" + glyphA + "100" + glyphB + "] TJ\n" + contentTail,
			[]typeset.Line{handLine(0, plain(hand.a), cluster(font, hand.b, -tenth, 0))},
		},
		{
			"justification widens interior spaces only, not leading ones",
			contentHead + "[" + space + glyphA + space + "-250" + glyphB + space + "-250" + glyphA + "] TJ\n" + contentTail,
			[]typeset.Line{
				handLine(2.5, plain(hand.space), plain(hand.a), plain(hand.space), plain(hand.b), plain(hand.space), plain(hand.a)),
			},
		},
		{
			"a raised glyph is set with a rise and the rise is restored",
			contentHead + "[" + glyphA + "] TJ\n2.5 Ts\n[" + glyphB + "] TJ\n0 Ts\n[" + glyphA + "] TJ\n" + contentTail,
			[]typeset.Line{handLine(0, plain(hand.a), cluster(font, hand.b, 0, quarter), plain(hand.a))},
		},
		{
			"a line that ends lowered resets the rise",
			contentHead + "[" + glyphA + "] TJ\n-2.5 Ts\n[" + glyphB + "] TJ\n0 Ts\n" + contentTail,
			[]typeset.Line{handLine(0, plain(hand.a), cluster(font, hand.b, 0, -quarter))},
		},
	}
}

// spanRows are the rows about marked-content spans and empty lines.
func spanRows(font *typeset.Font, hand glyphs) []contentRow {
	glyphA, glyphB := hex4(hand.a.id), hex4(hand.b.id)
	plain := func(ref glyphRef) typeset.Cluster { return cluster(font, ref, 0, 0) }
	mismatch := typeset.Cluster{Text: "x", Glyphs: plain(hand.b).Glyphs}
	pair := typeset.Cluster{Text: "AB", Glyphs: append(plain(hand.a).Glyphs, plain(hand.b).Glyphs...)}
	emoji := typeset.Cluster{Text: "\U0001F600", Glyphs: plain(hand.a).Glyphs}
	span := func(text, glyph string) string {
		return "/Span <</ActualText <FEFF" + text + ">>> BDC\n[" + glyph + "] TJ\nEMC\n"
	}

	return []contentRow{
		{
			"a cluster whose glyphs do not spell its text carries that text",
			contentHead + "[" + glyphA + "] TJ\n" + span("0078", glyphB) + "[" + glyphB + "] TJ\n" + contentTail,
			[]typeset.Line{handLine(0, plain(hand.a), mismatch, plain(hand.b))},
		},
		{
			"a multi-character cluster carries its text on the first glyph and an empty span on the next",
			contentHead + span("00410042", glyphA) + span("", glyphB) + contentTail,
			[]typeset.Line{handLine(0, pair)},
		},
		{
			"text outside the basic plane is written as a surrogate pair",
			contentHead + span("D83DDE00", glyphA) + contentTail,
			[]typeset.Line{handLine(0, emoji)},
		},
		{
			"empty lines produce no operators", contentHead + "[" + glyphA + "] TJ\n" + contentTail,
			[]typeset.Line{{X: 1, Baseline: 2}, handLine(0, plain(hand.a))},
		},
	}
}

func TestContentStreamOperators(t *testing.T) {
	t.Parallel()

	font := halfEmFont(t)
	hand := handGlyphs(t)

	for _, row := range append(offsetRows(font, hand), spanRows(font, hand)...) {
		block := &typeset.Placed{Font: font, Size: 10, Lines: row.lines}

		path, _ := writePDF(t, []genpage.Page{{Width: 300, Height: 200, Text: block}})
		qpdfCheck(t, path)

		// Pages are numbered after the catalog, the page tree and the five objects of the font (the first page
		// is object 8); its content stream is the next object.
		const contents = 9
		if got := objectStream(t, path, contents); got != row.want {
			t.Errorf("%s:\n got %q\nwant %q", row.name, got, row.want)
		}
	}
}

func TestContentStreamPaintsBackgroundAndTextColor(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)
	block := &typeset.Placed{Font: font, Size: 10, Lines: []typeset.Line{{X: 1, Baseline: 2, Clusters: []typeset.Cluster{
		cluster(font, handGlyphs(t).a, 0, 0),
	}}}}
	page := genpage.Page{
		Width: 100.5, Height: 50, Text: block,
		Background: &genpage.Color{R: 255, G: 51, B: 0}, TextColor: genpage.Color{R: 51, G: 102, B: 153},
	}

	path, _ := writePDF(t, []genpage.Page{page})

	want := "1 0.2 0 rg\n0 0 100.5 50 re\nf\n0.2 0.4 0.6 rg\nBT\n/F1 10 Tf\n1 0 0 1 1 2 Tm\n[" + hex4(handGlyphs(t).a.id) + "] TJ\nET\n"
	if got := objectStream(t, path, 3+5+1); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDocumentObjectNumbersAndOffsets(t *testing.T) {
	t.Parallel()

	first := defaultFont(t)
	second := halfEmFont(t)
	pages := []genpage.Page{
		textPage(t, first, firstPageText),
		{Width: 100, Height: 50},
		textPage(t, second, secondPageText),
	}

	path, data := writePDF(t, pages)
	qpdfCheck(t, path)

	text := string(data)

	// Objects 1 and 2 are the catalog and page tree, 3 to 7 and 8 to 12 the two fonts, 13 on the pages.
	for _, want := range []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Count 3 /Kids [13 0 R 15 0 R 17 0 R ] >>\nendobj\n",
		"3 0 obj\n<< /Type /Font /Subtype /Type0 /BaseFont /NotoSans-Regular /Encoding /Identity-H " +
			"/DescendantFonts [4 0 R] /ToUnicode 7 0 R >>\n",
		"/FontDescriptor 5 0 R /DW 1000 /W [",
		"/Flags 4 /FontBBox [-621 -389 2800 1067] /ItalicAngle 0 /Ascent 1069 /Descent -293 /CapHeight 714 /StemV 80 /FontFile2 6 0 R >>",
		"8 0 obj\n<< /Type /Font /Subtype /Type0",
		"/DescendantFonts [9 0 R] /ToUnicode 12 0 R >>\n",
		"13 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] " +
			"/Resources << /Font << /F1 3 0 R >> >> /Contents 14 0 R >>\nendobj\n",
		"15 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 50] /Resources << >> /Contents 16 0 R >>\nendobj\n",
		"17 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] " +
			"/Resources << /Font << /F1 8 0 R >> >> /Contents 18 0 R >>\nendobj\n",
		"trailer\n<< /Size 19 /Root 1 0 R >>\nstartxref\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}

	checkCrossReference(t, data, 19)
}

// checkCrossReference verifies the cross-reference table against the bytes: every object starts where its
// entry says and startxref points at the table.
func checkCrossReference(tb testing.TB, data []byte, size int) {
	tb.Helper()

	startxref := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(data)
	if startxref == nil {
		tb.Fatal("no startxref")
	}

	table, err := strconv.Atoi(string(startxref[1]))
	if err != nil || !bytes.HasPrefix(data[table:], []byte(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", size))) {
		tb.Fatalf("startxref %s does not point at the table", startxref[1])
	}

	entries := regexp.MustCompile(`(\d{10}) 0{5} n \n`).FindAllSubmatch(data[table:], -1)
	if len(entries) != size-1 {
		tb.Fatalf("%d entries, want %d", len(entries), size-1)
	}

	for index, entry := range entries {
		offset, convErr := strconv.Atoi(string(entry[1]))
		if convErr != nil || !bytes.HasPrefix(data[offset:], []byte(fmt.Sprintf("%d 0 obj\n", index+1))) {
			tb.Errorf("object %d: offset %s does not point at its header", index+1, entry[1])
		}
	}
}

func TestFontDescriptorAndWidthsAreScaledToThousandthsOfAnEm(t *testing.T) {
	t.Parallel()

	font := halfEmFont(t)
	hand := handGlyphs(t)

	block := &typeset.Placed{Font: font, Size: 10, Lines: []typeset.Line{{Clusters: []typeset.Cluster{
		cluster(font, hand.b, 0, 0), cluster(font, hand.a, 0, 0),
	}}}}

	_, data := writePDF(t, []genpage.Page{{Width: 300, Height: 200, Text: block}})
	text := string(data)

	// Half of each font-unit value: the font is 2,000 units to the em, so a thousandth of an em is two units.
	widths := fmt.Sprintf("/W [%d [%s] %d [%s] ]", hand.a.id, trimNumber(float64(font.GlyphAdvance(hand.a.id))/2),
		hand.b.id, trimNumber(float64(font.GlyphAdvance(hand.b.id))/2))

	for _, want := range []string{
		widths,
		"/FontBBox [-310.5 -194.5 1400 533.5]",
		"/Ascent 534.5 /Descent -146.5 /CapHeight 357",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestToUnicodeMapsExactlyTheUsedGlyphs(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)
	hand := handGlyphs(t)

	block := &typeset.Placed{Font: font, Size: 10, Lines: []typeset.Line{{Clusters: []typeset.Cluster{
		cluster(font, hand.b, 0, 0), cluster(font, hand.a, 0, 0),
	}}}}

	path, _ := writePDF(t, []genpage.Page{{Width: 300, Height: 200, Text: block}})

	want := toUnicodeHeader + "2 beginbfchar\n<0024> <0041>\n<0025> <0042>\nendbfchar\n" + toUnicodeFooter
	if got := objectStream(t, path, 3+4); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestToUnicodeChunksEveryHundredEntries(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	// Glyph 0 has no text, so a page that uses only it maps nothing.
	if font.GlyphText(0) != "" {
		t.Fatal("glyph 0 is expected to have no text")
	}

	var mapped []glyphRef

	for id := uint16(1); len(mapped) < 201; id++ {
		if text := font.GlyphText(id); text != "" {
			mapped = append(mapped, glyphRef{id: id, text: text})
		}
	}

	rows := []struct {
		name   string
		glyphs []glyphRef
		want   []string
	}{
		{"no mapped glyph", []glyphRef{{id: 0}}, nil},
		{"one entry", mapped[:1], []string{"1"}},
		{"one full chunk", mapped[:100], []string{"100"}},
		{"a full chunk and one entry", mapped[:101], []string{"100", "1"}},
		{"two full chunks", mapped[:200], []string{"100", "100"}},
		{"two chunks and one entry", mapped[:201], []string{"100", "100", "1"}},
	}

	for _, row := range rows {
		clusters := make([]typeset.Cluster, len(row.glyphs))
		for index, ref := range row.glyphs {
			clusters[index] = cluster(font, ref, 0, 0)
		}

		block := &typeset.Placed{Font: font, Size: 10, Lines: []typeset.Line{{Clusters: clusters}}}
		got := toUnicodeCounts(t, genpage.Page{Width: 300, Height: 200, Text: block})

		if !equalStrings(got, row.want) {
			t.Errorf("%s: chunk sizes %v, want %v", row.name, got, row.want)
		}
	}
}

// toUnicodeCounts writes the page, guarding against a writer that never finishes, and returns the entry count
// of each beginbfchar section of the font's ToUnicode map.
func toUnicodeCounts(tb testing.TB, page genpage.Page) []string {
	tb.Helper()

	var buffer bytes.Buffer

	done := make(chan error, 1)

	go func() {
		_, err := genpage.Write(context.Background(), &buffer, []genpage.Page{page})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			tb.Fatal(err)
		}
	case <-time.After(hangDeadline):
		tb.Fatal("Write did not finish")
	}

	path, _ := writePDF(tb, []genpage.Page{page})

	matches := beginBfChar.FindAllStringSubmatch(objectStream(tb, path, 3+4), -1)
	counts := make([]string, 0, len(matches))

	for _, match := range matches {
		counts = append(counts, match[1])
	}

	return counts
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}

func TestFontSizeBoundaries(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)
	line := typeset.Line{Clusters: []typeset.Cluster{cluster(font, handGlyphs(t).a, 0, 0)}}

	rows := []struct {
		name    string
		size    float64
		wantErr bool
	}{
		{"smallest", typeset.MinFontSize, false},
		{"largest", typeset.MaxFontSize, false},
		{"below the smallest", math.Nextafter(typeset.MinFontSize, 0), true},
		{"above the largest", math.Nextafter(typeset.MaxFontSize, math.Inf(1)), true},
	}

	for _, row := range rows {
		block := &typeset.Placed{Font: font, Size: row.size, Lines: []typeset.Line{line}}

		_, err := genpage.Write(context.Background(), io.Discard, []genpage.Page{{Width: 300, Height: 200, Text: block}})
		if (err != nil) != row.wantErr {
			t.Errorf("%s: %v", row.name, err)
		}
	}
}

func TestFontsAreCollectedFromEveryPage(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	pages := []genpage.Page{
		{Width: 100, Height: 50},
		{Width: 100, Height: 50, Text: &typeset.Placed{Font: font, Size: 10}},
		textPage(t, font, "late text"),
	}

	path, _ := writePDF(t, pages)
	qpdfCheck(t, path)

	if got := extractRaw(t, path); strings.TrimSpace(got[2]) != "late text" {
		t.Errorf("extracted %q", got)
	}
}

func TestPageLimitIsInclusive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A canceled context stops the write right after the header and page tree, so reaching that point
	// shows the page count was accepted without writing a million pages.
	pages := make([]genpage.Page, genpage.MaxPages)
	for index := range pages {
		pages[index] = genpage.Page{Width: 10, Height: 10}
	}

	_, err := genpage.Write(ctx, io.Discard, pages)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("a document of exactly MaxPages pages: %v", err)
	}
}
