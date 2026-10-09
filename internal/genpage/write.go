// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package genpage

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// Producer supplies the next sequential page, whose shaped data is released after writing.
	Producer func(int) (Page, error)
	// Color is an sRGB color.
	Color struct {
		R, G, B uint8
	}

	// Page is one generated page specification.
	Page struct {
		// Background paints the whole page when not nil.
		Background *Color
		// Text is the placed text block, or nil for a background-only page. Its coordinates are page
		// coordinates of a page of this Width and Height.
		Text *typeset.Placed
		// Width and Height are the page sides in points, MinPageSide through MaxPageSide of package typeset.
		Width, Height float64
		// TextColor paints Text.
		TextColor Color
	}

	// fontSet is the distinct fonts of the pages and, per font, the glyphs the pages use.
	fontSet struct {
		index map[typeset.Digest]int
		list  []*typeset.Font
		used  [][glyphSetWords]uint64
	}

	document struct {
		out     *sink
		fonts   *fontSet
		flate   *zlib.Writer
		offsets []int64 // offsets[n] is the byte offset of object n
		content []byte
		packed  bytes.Buffer
		firstPg int
	}

	// sink counts bytes and keeps the first write error.
	sink struct {
		w   *bufio.Writer
		err error
		n   int64
	}

	// lineWriter writes the text-showing operators of one line and tracks the two pens: where the shaper's
	// pen is and where the PDF text pen is, both in font units from the line start.
	lineWriter struct {
		doc       *document
		font      *typeset.Font
		array     []byte
		size      float64
		perEm     float64
		extraUnit float64
		shapedPen float64
		pdfPen    float64
		rise      int
		fontIndex int
		seenWord  bool
	}
)

const (
	// MaxPages is the largest number of pages in one resource document, the generated-page limit of a job.
	MaxPages = 1_000_000

	fontObjects      = 5 // Type0, CIDFont, FontDescriptor, FontFile2, ToUnicode
	cidFontObject    = 1 // object offsets from a font's first object
	descriptorObject = 2
	fontFileObject   = 3
	toUnicodeObject  = 4
	decimalBase      = 10
	firstFontObject  = 3
	maxOffsetDigits  = 9_999_999_999
	glyphSetWords    = 1 << 10 // 65,536 glyph identifiers as bits
	glyphWordShift   = 6       // glyph identifier bits that select the word
	glyphBitMask     = 1<<glyphWordShift - 1
	thousandthsPerEm = 1000.0
	bfCharChunk      = 100
	writeBufferSize  = 1 << 16
	refsBufferSize   = 1 << 12
	objectsPerPage   = 2 // the page and its content stream
	hexDigitsPerUnit = 4 // UTF-16 code unit in hexadecimal
	minPageSide      = typeset.MinPageSide
	maxPageSide      = typeset.MaxPageSide

	// cmapPrologue starts the ToUnicode CMap stream; cmapEpilogue ends it.
	cmapPrologue = "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	cmapEpilogue = "endcmap\nCMapName currentdict /CMap defineresource pop\n" + "end\n" + "end\n"
)

var (
	// ErrTooManyPages reports a document above MaxPages.
	ErrTooManyPages = errors.New("genpage: too many pages")
	// ErrNoPages reports a document without pages.
	ErrNoPages = errors.New("genpage: no pages")
	// ErrInvalidPage reports a page whose size or text block is outside what the writer supports.
	ErrInvalidPage = errors.New("genpage: invalid page")
	// ErrDocumentTooLarge reports a document whose byte offsets do not fit the cross-reference table.
	ErrDocumentTooLarge = errors.New("genpage: document exceeds the cross-reference offset range")
)

// Write writes the resource document for pages to w and returns the byte count. Page i of the
// document is pages[i]. Fonts are the distinct fonts of the pages' text, by identity, in order of
// first use; each is embedded once. Write stops with ctx's error between pages. Output is
// deterministic.
func Write(ctx context.Context, w io.Writer, pages []Page) (int64, error) {
	if len(pages) == 0 {
		return 0, ErrNoPages
	}

	if len(pages) > MaxPages {
		return 0, fmt.Errorf("%w: %d pages exceed the limit of %d", ErrTooManyPages, len(pages), MaxPages)
	}

	fonts, err := collectFonts(pages)
	if err != nil {
		return 0, err
	}

	doc := &document{
		out:     &sink{w: bufio.NewWriterSize(w, writeBufferSize)},
		fonts:   fonts,
		firstPg: firstFontObject + fontObjects*len(fonts.list),
	}
	doc.offsets = make([]int64, doc.firstPg+objectsPerPage*len(pages))

	return doc.write(ctx, len(pages), func(index int) (Page, error) { return pages[index], nil })
}

func collectFonts(pages []Page) (*fontSet, error) {
	set := &fontSet{index: map[typeset.Digest]int{}}

	for number, page := range pages {
		if !(page.Width >= minPageSide && page.Width <= maxPageSide && page.Height >= minPageSide && page.Height <= maxPageSide) {
			return nil, fmt.Errorf("%w %d: size %v x %v pt outside %v to %v",
				ErrInvalidPage, number, page.Width, page.Height, minPageSide, maxPageSide)
		}

		if page.Text == nil || len(page.Text.Lines) == 0 {
			continue
		}

		if page.Text.Font == nil || !(page.Text.Size >= typeset.MinFontSize && page.Text.Size <= typeset.MaxFontSize) {
			return nil, fmt.Errorf("%w %d: text has no font or an invalid size", ErrInvalidPage, number)
		}

		identity := page.Text.Font.Identity()

		_, known := set.index[identity]
		if !known {
			set.index[identity] = len(set.list)
			set.list = append(set.list, page.Text.Font)
		}
	}

	set.used = make([][glyphSetWords]uint64, len(set.list))

	return set, nil
}

func (s *sink) write(data []byte) {
	if s.err != nil {
		return
	}

	written, err := s.w.Write(data)
	s.n += int64(written)
	s.err = err
}

func (s *sink) textf(format string, args ...any) {
	s.write(fmt.Appendf(nil, format, args...))
}

func (d *document) begin(obj int) {
	d.offsets[obj] = d.out.n
	d.out.textf("%d 0 obj\n", obj)
}

func (d *document) stream(obj int, dict string, data []byte) {
	d.begin(obj)
	d.out.textf("<< %s /Length %d >>\nstream\n", dict, len(data))
	d.out.write(data)
	d.out.write([]byte("\nendstream\nendobj\n"))
}

// deflate compresses data into a buffer owned by d; the result is valid until the next call.
func (d *document) deflate(data []byte) []byte {
	d.packed.Reset()

	if d.flate == nil {
		d.flate = zlib.NewWriter(&d.packed)
	} else {
		d.flate.Reset(&d.packed)
	}

	// The compressor writes exclusively to bytes.Buffer, whose writes cannot fail.
	_, _ = d.flate.Write(data)
	_ = d.flate.Close()

	return d.packed.Bytes()
}

func (d *document) write(ctx context.Context, count int, produce Producer) (int64, error) {
	d.out.write([]byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"))
	d.begin(1)
	d.out.textf("<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	d.writePageTree(count)

	for index := range count {
		err := ctx.Err()
		if err != nil {
			return d.out.n, fmt.Errorf("genpage: %w", err)
		}

		page, err := produce(index)
		if err != nil {
			return d.out.n, err
		}

		err = d.writePage(index, page)
		if err != nil {
			return d.out.n, err
		}
	}

	for index := range d.fonts.list {
		err := d.writeFont(index)
		if err != nil {
			return d.out.n, err
		}
	}

	return d.finish()
}

func (d *document) writePageTree(count int) {
	d.begin(2)
	d.out.textf("<< /Type /Pages /Count %d /Kids [", count)

	var refs []byte

	for index := range count {
		refs = strconv.AppendInt(refs, int64(d.firstPg+objectsPerPage*index), decimalBase)
		refs = append(refs, " 0 R "...)

		if len(refs) > refsBufferSize {
			d.out.write(refs)
			refs = refs[:0]
		}
	}

	d.out.write(refs)
	d.out.write([]byte("] >>\nendobj\n"))
}

func (d *document) writePage(index int, page Page) error {
	obj := d.firstPg + objectsPerPage*index

	d.content = d.content[:0]
	font := d.appendContent(page)

	data := d.deflate(d.content)

	d.begin(obj)
	d.out.textf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] ", number(page.Width), number(page.Height))

	if font >= 0 {
		d.out.textf("/Resources << /Font << /F1 %d 0 R >> >> ", firstFontObject+fontObjects*font)
	} else {
		d.out.textf("/Resources << >> ")
	}

	d.out.textf("/Contents %d 0 R >>\nendobj\n", obj+1)
	d.stream(obj+1, "/Filter /FlateDecode", data)

	return d.out.err
}

// appendContent writes the page's content stream into d.content and returns the used font index, or -1.
func (d *document) appendContent(page Page) int {
	if page.Background != nil {
		d.appendColor(*page.Background)
		d.content = append(d.content, "0 0 "...)
		d.content = append(d.content, number(page.Width)...)
		d.content = append(d.content, ' ')
		d.content = append(d.content, number(page.Height)...)
		d.content = append(d.content, " re\nf\n"...)
	}

	if page.Text == nil || len(page.Text.Lines) == 0 {
		return -1
	}

	font := d.fonts.index[page.Text.Font.Identity()]
	d.appendColor(page.TextColor)
	d.appendText(page.Text, font)

	return font
}

func (d *document) appendColor(color Color) {
	for _, component := range []uint8{color.R, color.G, color.B} {
		d.content = append(d.content, number(float64(component)/math.MaxUint8)...)
		d.content = append(d.content, ' ')
	}

	d.content = append(d.content, "rg\n"...)
}

// number emits round-trip decimal precision without scientific notation. Canvas dimensions and
// captured physical placements must describe the numbers actually written into the resource PDF.
func number(value float64) string {
	if value == 0 {
		return "0"
	}

	return strconv.FormatFloat(value, 'f', -1, 64)
}

func utf16Hex(text string) string {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 0, hexDigitsPerUnit*len(units))

	for _, unit := range units {
		out = fmt.Appendf(out, "%04X", unit)
	}

	return string(out)
}

// needsActualText reports whether extraction from the glyphs' own ToUnicode text would differ from the
// cluster's source text, or the cluster has several characters (extractors that order glyphs by
// geometry could otherwise emit a mark left of its base).
func needsActualText(font *typeset.Font, cluster typeset.Cluster) bool {
	if utf8.RuneCountInString(cluster.Text) > 1 {
		return true
	}

	joined := make([]byte, 0, len(cluster.Text))
	for _, glyph := range cluster.Glyphs {
		joined = append(joined, font.GlyphText(glyph.ID)...)
	}

	return string(joined) != cluster.Text
}

// appendText writes the text object of a placed block. A cluster that needs ActualText gets one span
// per glyph: the first glyph's span carries the cluster text, later glyphs (marks) an empty one, because
// one span over base and mark makes poppler insert a spurious space after the mark.
func (d *document) appendText(placed *typeset.Placed, fontIndex int) {
	d.content = append(d.content, "BT\n/F1 "...)
	d.content = append(d.content, number(placed.Size)...)
	d.content = append(d.content, " Tf\n"...)

	for _, line := range placed.Lines {
		if len(line.Clusters) == 0 {
			continue
		}

		d.content = fmt.Appendf(d.content, "1 0 0 1 %s %s Tm\n", number(line.X), number(line.Baseline))
		d.appendLine(placed, line, fontIndex)
	}

	d.content = append(d.content, "ET\n"...)
}

func (d *document) appendLine(placed *typeset.Placed, line typeset.Line, fontIndex int) {
	font := placed.Font
	unitsPerEm := float64(font.UnitsPerEm())
	writer := lineWriter{
		doc: d, font: font, fontIndex: fontIndex, size: placed.Size,
		perEm: thousandthsPerEm / unitsPerEm, extraUnit: line.ExtraSpace / placed.Size * unitsPerEm,
	}

	for _, cluster := range line.Clusters {
		writer.showCluster(cluster)
	}

	writer.flush()

	if writer.rise != 0 {
		d.content = append(d.content, "0 Ts\n"...)
	}
}

// flush writes the pending glyphs of the array as one TJ operator.
func (w *lineWriter) flush() {
	if len(w.array) == 0 {
		return
	}

	w.doc.content = append(w.doc.content, '[')
	w.doc.content = append(w.doc.content, w.array...)
	w.doc.content = append(w.doc.content, "] TJ\n"...)
	w.array = w.array[:0]
}

func (w *lineWriter) showCluster(cluster typeset.Cluster) {
	actual := needsActualText(w.font, cluster)
	pen := w.shapedPen

	for index, glyph := range cluster.Glyphs {
		origin := pen + float64(glyph.XOffset)
		pen += float64(glyph.Advance)

		if cluster.IsSpace() && w.seenWord {
			pen += w.extraUnit
		}

		switch {
		case !actual:
			w.showGlyph(glyph, origin)
		case index == 0:
			w.showSpanned(glyph, origin, cluster.Text)
		default:
			w.showSpanned(glyph, origin, "")
		}
	}

	w.shapedPen = pen
	w.seenWord = w.seenWord || !cluster.IsSpace()
}

// showSpanned shows one glyph inside a marked-content span whose ActualText is text.
func (w *lineWriter) showSpanned(glyph typeset.Glyph, origin float64, text string) {
	w.flush()

	w.doc.content = fmt.Appendf(w.doc.content, "/Span <</ActualText <FEFF%s>>> BDC\n", utf16Hex(text))

	w.showGlyph(glyph, origin)
	w.flush()

	w.doc.content = append(w.doc.content, "EMC\n"...)
}

// showGlyph shows one glyph drawn at origin font units from the line start.
func (w *lineWriter) showGlyph(glyph typeset.Glyph, origin float64) {
	if glyph.YOffset != w.rise {
		w.flush()

		w.rise = glyph.YOffset
		w.doc.content = fmt.Appendf(w.doc.content, "%s Ts\n", number(float64(w.rise)*w.size/float64(w.font.UnitsPerEm())))
	}

	w.doc.fonts.used[w.fontIndex][glyph.ID>>glyphWordShift] |= 1 << (glyph.ID & glyphBitMask)

	adjustment := number(-(origin - w.pdfPen) * w.perEm)
	if adjustment != "0" {
		w.array = append(w.array, adjustment...)
	}

	w.array = fmt.Appendf(w.array, "<%04X>", glyph.ID)
	w.pdfPen = origin + float64(w.font.GlyphAdvance(glyph.ID))
}

// usedGlyphs returns the identifiers of the glyphs the pages use of the font, in increasing order.
func (d *document) usedGlyphs(font int) []uint16 {
	var glyphs []uint16

	for word := range uint16(glyphSetWords) {
		bitsSet := d.fonts.used[font][word]
		if bitsSet == 0 {
			continue
		}

		for bit := range uint16(1 << glyphWordShift) {
			if bitsSet&(1<<bit) != 0 {
				glyphs = append(glyphs, word<<glyphWordShift|bit)
			}
		}
	}

	return glyphs
}

func (d *document) writeFont(index int) error {
	font := d.fonts.list[index]
	base := firstFontObject + fontObjects*index
	glyphs := d.usedGlyphs(index)
	perEm := thousandthsPerEm / float64(font.UnitsPerEm())

	var widths []byte
	for _, glyph := range glyphs {
		widths = fmt.Appendf(widths, "%d [%s] ", glyph, number(float64(font.GlyphAdvance(glyph))*perEm))
	}

	name := font.PostScriptName()

	d.begin(base)
	d.out.textf(
		"<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>\nendobj\n",
		name,
		base+cidFontObject,
		base+toUnicodeObject,
	)
	d.begin(base + cidFontObject)
	d.out.textf(
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> "+
			"/FontDescriptor %d 0 R /DW 1000 /W [%s] /CIDToGIDMap /Identity >>\nendobj\n",
		name,
		base+descriptorObject,
		widths,
	)
	d.begin(base + descriptorObject)
	d.out.textf("<< /Type /FontDescriptor /FontName /%s /Flags 4 %s /ItalicAngle 0 %s /StemV 80 /FontFile2 %d 0 R >>\nendobj\n",
		name, fontBox(font, perEm), fontHeights(font, perEm), base+fontFileObject)

	packed := d.deflate(font.Bytes())

	d.stream(base+fontFileObject, fmt.Sprintf("/Filter /FlateDecode /Length1 %d", len(font.Bytes())), packed)

	packed = d.deflate(toUnicodeCMap(font, glyphs))

	d.stream(base+toUnicodeObject, "/Filter /FlateDecode", packed)

	return d.out.err
}

// fontBox is the /FontBBox entry of a font descriptor; perEm scales font units to thousandths of an em.
func fontBox(font *typeset.Font, perEm float64) string {
	box := font.BBox()

	return fmt.Sprintf("/FontBBox [%s %s %s %s]",
		number(float64(box[0])*perEm), number(float64(box[1])*perEm), number(float64(box[2])*perEm), number(float64(box[3])*perEm))
}

// fontHeights is the /Ascent, /Descent and /CapHeight entries of a font descriptor.
func fontHeights(font *typeset.Font, perEm float64) string {
	return fmt.Sprintf("/Ascent %s /Descent %s /CapHeight %s",
		number(font.Ascent()*perEm), number(font.Descent()*perEm), number(font.CapHeight()*perEm))
}

func toUnicodeCMap(font *typeset.Font, glyphs []uint16) []byte {
	entries := make([]string, 0, len(glyphs))

	for _, glyph := range glyphs {
		text := font.GlyphText(glyph)
		if text != "" {
			entries = append(entries, fmt.Sprintf("<%04X> <%s>", glyph, utf16Hex(text)))
		}
	}

	var cmap bytes.Buffer

	cmap.WriteString(cmapPrologue)

	for start := 0; start < len(entries); start += bfCharChunk {
		end := min(start+bfCharChunk, len(entries))
		fmt.Fprintf(&cmap, "%d beginbfchar\n", end-start)

		for _, entry := range entries[start:end] {
			cmap.WriteString(entry + "\n")
		}

		cmap.WriteString("endbfchar\n")
	}

	cmap.WriteString(cmapEpilogue)

	return cmap.Bytes()
}

func (d *document) finish() (int64, error) {
	xref := d.out.n

	d.out.textf("xref\n0 %d\n0000000000 65535 f \n", len(d.offsets))

	for object := 1; object < len(d.offsets); object++ {
		if d.offsets[object] > maxOffsetDigits {
			return d.out.n, ErrDocumentTooLarge
		}

		d.out.textf("%010d 00000 n \n", d.offsets[object])
	}

	d.out.textf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(d.offsets), xref)

	if d.out.err == nil {
		d.out.err = d.out.w.Flush()
	}

	if d.out.err != nil {
		return d.out.n, fmt.Errorf("genpage: write: %w", d.out.err)
	}

	return d.out.n, nil
}

// WriteProduced shapes and writes one page at a time through produce. Font resources are shared
// job-wide; only used-glyph bitsets survive a page. The producer may release its lines after return.
func WriteProduced(ctx context.Context, w io.Writer, count int, fonts []*typeset.Font, produce Producer) (int64, error) {
	if count < 1 {
		return 0, ErrNoPages
	}

	if count > MaxPages {
		return 0, ErrTooManyPages
	}

	set, err := declaredFonts(fonts)
	if err != nil {
		return 0, err
	}

	doc := &document{
		out:     &sink{w: bufio.NewWriterSize(w, writeBufferSize)},
		fonts:   set,
		firstPg: firstFontObject + fontObjects*len(set.list),
	}
	doc.offsets = make([]int64, doc.firstPg+objectsPerPage*count)

	return doc.write(ctx, count, func(index int) (Page, error) {
		page, produceErr := produce(index)
		if produceErr != nil {
			return Page{}, produceErr
		}

		if failure := validateProducedPage(page, set); failure != nil {
			return Page{}, failure
		}

		return page, nil
	})
}

func declaredFonts(fonts []*typeset.Font) (*fontSet, error) {
	set := &fontSet{index: map[typeset.Digest]int{}}

	for _, font := range fonts {
		if font == nil {
			return nil, ErrInvalidPage
		}

		if _, found := set.index[font.Identity()]; !found {
			set.index[font.Identity()] = len(set.list)
			set.list = append(set.list, font)
		}
	}

	set.used = make([][glyphSetWords]uint64, len(set.list))

	return set, nil
}

func validateProducedPage(page Page, set *fontSet) error {
	if !(page.Width >= minPageSide && page.Width <= maxPageSide && page.Height >= minPageSide && page.Height <= maxPageSide) {
		return ErrInvalidPage
	}

	if page.Text != nil && len(page.Text.Lines) > 0 {
		if page.Text.Font == nil || !(page.Text.Size >= typeset.MinFontSize && page.Text.Size <= typeset.MaxFontSize) {
			return ErrInvalidPage
		}

		if _, found := set.index[page.Text.Font.Identity()]; !found {
			return ErrInvalidPage
		}
	}

	return nil
}
