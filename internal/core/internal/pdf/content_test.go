// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
	"github.com/resoltico/pdfconcat/internal/oracle"
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
	contentHead     = "0 0 0 rg\nBT\n/F1 10 Tf\n1 0 0 1 12.3456 190.25 Tm\n"
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

// objectStream resolves a semantic stream reference in independent qpdf JSON.
func objectStream(tb testing.TB, path, key string) string {
	tb.Helper()
	for _, dict := range qpdfDictionaries(tb, path) {
		if ref, ok := dict[key].(string); ok {
			return string(run(tb, qpdfTool, "--show-object="+strings.Fields(ref)[0], "--filtered-stream-data", path))
		}
	}
	tb.Fatalf("missing %s stream", key)
	return ""
}

func qpdfDictionaries(tb testing.TB, path string) []map[string]any {
	tb.Helper()
	var dump any
	if err := json.Unmarshal(run(tb, qpdfTool, "--json", path), &dump); err != nil {
		tb.Fatal(err)
	}
	var result []map[string]any
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			result = append(result, v)
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(dump)
	return result
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

		path, _ := writePDF(t, []compose.Page{{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}})
		qpdfCheck(t, path)

		const contents = "/Contents"
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
	page := compose.Page{
		Width: 100.5, Height: 50,
		Background: &packet.Color{R: 255, G: 51, B: 0}, Blocks: []compose.TextBlock{{Text: block, Color: packet.Color{R: 51, G: 102, B: 153}}},
	}

	path, _ := writePDF(t, []compose.Page{page})

	want := "1 0.2 0 rg\n0 0 100.5 50 re\nf\n0.2 0.4 0.6 rg\nBT\n/F1 10 Tf\n1 0 0 1 1 2 Tm\n[" + hex4(handGlyphs(t).a.id) + "] TJ\nET\n"
	if got := objectStream(t, path, "/Contents"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFontDescriptorAndWidthsAreScaledToThousandthsOfAnEm(t *testing.T) {
	t.Parallel()

	font := halfEmFont(t)
	hand := handGlyphs(t)

	block := &typeset.Placed{Font: font, Size: 10, Lines: []typeset.Line{{Clusters: []typeset.Cluster{
		cluster(font, hand.b, 0, 0), cluster(font, hand.a, 0, 0),
	}}}}

	path, _ := writePDF(t, []compose.Page{{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}})
	foundWidths, foundDescriptor := false, false
	for _, dict := range qpdfDictionaries(t, path) {
		if actual, ok := dict["/W"]; ok && dict["/Subtype"] == "/CIDFontType2" {
			foundWidths = true
			want := []any{float64(hand.a.id), []any{float64(font.GlyphAdvance(hand.a.id)) / 2}, float64(hand.b.id), []any{float64(font.GlyphAdvance(hand.b.id)) / 2}}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("scaled widths: %v want %v", actual, want)
			}
		}
		if dict["/Type"] == "/FontDescriptor" {
			foundDescriptor = true
			if !reflect.DeepEqual(dict["/FontBBox"], []any{-310.5, -194.5, 1400.0, 533.5}) || dict["/Ascent"] != 534.5 || dict["/Descent"] != -146.5 || dict["/CapHeight"] != 357.0 {
				t.Fatalf("scaled descriptor: %v", dict)
			}
		}
	}
	if !foundWidths || !foundDescriptor {
		t.Fatal("missing independent font dictionary facts")
	}

}

func TestToUnicodeMapsExactlyTheUsedGlyphs(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)
	hand := handGlyphs(t)

	block := &typeset.Placed{Font: font, Size: 10, Lines: []typeset.Line{{Clusters: []typeset.Cluster{
		cluster(font, hand.b, 0, 0), cluster(font, hand.a, 0, 0),
	}}}}

	path, _ := writePDF(t, []compose.Page{{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}})

	want := toUnicodeHeader + "2 beginbfchar\n<0024> <0041>\n<0025> <0042>\nendbfchar\n" + toUnicodeFooter
	if got := objectStream(t, path, "/ToUnicode"); got != want {
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
		got := toUnicodeCounts(t, compose.Page{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}})

		if !equalStrings(got, row.want) {
			t.Errorf("%s: chunk sizes %v, want %v", row.name, got, row.want)
		}
	}
}

// toUnicodeCounts writes the page, guarding against a writer that never finishes, and returns the entry count
// of each beginbfchar section of the font's ToUnicode map.
func toUnicodeCounts(tb testing.TB, page compose.Page) []string {
	tb.Helper()

	var buffer bytes.Buffer

	done := make(chan error, 1)

	go func() {
		_, err := pdf.Write(context.Background(), &buffer, []compose.Page{page})
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

	path, _ := writePDF(tb, []compose.Page{page})

	matches := beginBfChar.FindAllStringSubmatch(objectStream(tb, path, "/ToUnicode"), -1)
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

		_, err := pdf.Write(context.Background(), io.Discard, []compose.Page{{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}})
		if (err != nil) != row.wantErr {
			t.Errorf("%s: %v", row.name, err)
		}
	}
}

func TestFontsAreCollectedFromEveryPage(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)

	pages := []compose.Page{
		{Width: 100, Height: 50},
		{Width: 100, Height: 50, Blocks: []compose.TextBlock{{Text: &typeset.Placed{Font: font, Size: 10}}}},
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
	pages := make([]compose.Page, pdf.MaxPages)
	for index := range pages {
		pages[index] = compose.Page{Width: 10, Height: 10}
	}

	_, err := pdf.Write(ctx, io.Discard, pages)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("a document of exactly MaxPages pages: %v", err)
	}
}

func TestGeneratedBlocksShareFontAndLinkFinalPhysicalPage(t *testing.T) {
	font := defaultFont(t)
	digits, err := typeset.NewDigits(font, 12)
	if err != nil {
		t.Fatal(err)
	}
	numeric, err := digits.Place("123", 260, 100)
	if err != nil {
		t.Fatal(err)
	}
	first := textPage(t, font, "Heading")
	first.Blocks = append(first.Blocks, compose.TextBlock{Text: numeric})
	first.Links = []compose.Link{{Rect: typeset.Rect{X: 10, Y: 90, Width: 270, Height: 100}, TargetPage: 2}}
	path, _ := writePDF(t, []compose.Page{first, textPage(t, font, "TARGET")})
	qpdfCheck(t, path)
	texts := extractRaw(t, path)
	if len(texts) != 2 || strings.Join(strings.Fields(texts[0]), " ") != "Heading 123" || strings.TrimSpace(texts[1]) != "TARGET" {
		t.Fatalf("multi-block extraction: %q", texts)
	}
	doc, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	objects := doc.PageObjects()
	found := false
	for _, dict := range qpdfDictionaries(t, path) {
		if dict["/Subtype"] != "/Link" {
			continue
		}
		found = true
		if !reflect.DeepEqual(dict["/Dest"], []any{objects[1], "/Fit"}) || !reflect.DeepEqual(dict["/Rect"], []any{10.0, 90.0, 280.0, 190.0}) {
			t.Fatalf("generated row link: %v", dict)
		}
	}
	if !found {
		t.Fatal("missing row link")
	}
	names, programs := finalFontFacts(t, path)
	if names[font.PostScriptName()] != 3 || len(programs) != 1 {
		t.Fatalf("blocks did not share font: %v", names)
	}
}
