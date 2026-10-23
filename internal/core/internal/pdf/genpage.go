// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type fontSet struct {
	index map[typeset.Digest]int
	list  []*typeset.Font
	used  [][glyphSetWords]uint64
}
type document struct {
	fonts    *fontSet
	content  []byte
	fontKeys []string
}
type lineWriter struct {
	doc                                       *document
	font                                      *typeset.Font
	array                                     []byte
	size, perEm, extraUnit, shapedPen, pdfPen float64
	rise, fontIndex                           int
	seenWord                                  bool
}
type generatedLink struct {
	dict   types.Dict
	target int64
}

const (
	// MaxPages limits newly generated pages in one resource document.
	MaxPages         = 100_000
	glyphSetWords    = 1 << 10
	glyphWordShift   = 6
	glyphBitMask     = 1<<glyphWordShift - 1
	thousandthsPerEm = 1000.0
	bfCharChunk      = 100
	hexDigitsPerUnit = 4
	minPageSide      = typeset.MinPageSide
	maxPageSide      = typeset.MaxPageSide
	cmapPrologue     = "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" + "/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" + "1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	cmapEpilogue     = "endcmap\nCMapName currentdict /CMap defineresource pop\n" + "end\n" + "end\n"
)

var (
	// ErrTooManyPages reports the generated-page count limit.
	ErrTooManyPages = errors.New("pdf: generated page: too many pages")
	// ErrNoPages reports an empty generated document.
	ErrNoPages = errors.New("pdf: generated page: no pages")
	// ErrInvalidPage reports an invalid generated canvas or missing font union.
	ErrInvalidPage = errors.New("pdf: generated page: invalid page")
)

func collectFonts(c context.Context, pages []compose.Page) (*fontSet, error) {
	set := &fontSet{index: map[typeset.Digest]int{}}
	for number, page := range pages {
		if err := c.Err(); err != nil {
			return nil, err
		}
		if !(page.Width >= minPageSide && page.Width <= maxPageSide && page.Height >= minPageSide && page.Height <= maxPageSide) {
			return nil, fmt.Errorf("%w %d: size %v x %v pt outside %v to %v", ErrInvalidPage, number, page.Width, page.Height, minPageSide, maxPageSide)
		}
		for _, block := range page.Blocks {
			text := block.Text
			if text == nil || len(text.Lines) == 0 {
				continue
			}
			if text.Font == nil || !(text.Size >= typeset.MinFontSize && text.Size <= typeset.MaxFontSize) {
				return nil, fmt.Errorf("%w %d: text has no font or an invalid size", ErrInvalidPage, number)
			}
			if err := ValidateFontName(text.Font); err != nil {
				return nil, err
			}
			id := text.Font.Identity()
			index, known := set.index[id]
			if !known {
				index = len(set.list)
				set.index[id] = index
				set.list = append(set.list, text.Font)
				set.used = append(set.used, [glyphSetWords]uint64{})
			}
			for _, line := range text.Lines {
				for _, cluster := range line.Clusters {
					for _, g := range cluster.Glyphs {
						set.used[index][g.ID>>glyphWordShift] |= 1 << (g.ID & glyphBitMask)
					}
				}
			}
		}
	}
	return set, nil
}

// Write serializes the same native builder used by assembly, retaining full fonts
// for the independent original-program comparison path.
func Write(c context.Context, w io.Writer, pages []compose.Page) (int64, error) {
	s, links, err := generatedContext(c, pages, len(pages), nil, nil)
	if err != nil {
		return 0, err
	}
	if err = finalizeGeneratedLinks(c, s, links); err != nil {
		return 0, err
	}
	return serializeGenerated(c, s, w)
}

// WriteRepeated serializes a repeated native generated item with its job union.
func WriteRepeated(c context.Context, w io.Writer, page compose.Page, count int, prepared map[typeset.Digest]typeset.EmbeddedFont) (int64, error) {
	names, err := generatedFontNames(prepared, nil)
	if err != nil {
		return 0, err
	}
	return writeRepeatedNamed(c, w, page, count, prepared, names)
}
func writeRepeatedNamed(c context.Context, w io.Writer, page compose.Page, count int, prepared map[typeset.Digest]typeset.EmbeddedFont, names map[typeset.Digest]string) (int64, error) {
	if prepared == nil {
		prepared = map[typeset.Digest]typeset.EmbeddedFont{}
	}
	s, links, err := generatedContext(c, []compose.Page{page}, count, prepared, names)
	if err != nil {
		return 0, err
	}
	if err = finalizeGeneratedLinks(c, s, links); err != nil {
		return 0, err
	}
	return serializeGenerated(c, s, w)
}

type generatedOutput struct {
	writer  io.Writer
	context context.Context
	n       int64
	err     error
}

func (o *generatedOutput) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if o.err == nil {
			o.err = o.context.Err()
		}
		if o.err != nil {
			return written, o.err
		}
		// Native serialization can forward an entire font stream in one Write.
		// Bound each downstream write so cancellation is observed between chunks.
		chunk := data[:min(len(data), 32*1024)]
		n, err := o.writer.Write(chunk)
		written += n
		o.n += int64(n)
		if err == nil && n != len(chunk) {
			err = io.ErrShortWrite
		}
		o.err = err
		if err != nil {
			return written, err
		}
		data = data[n:]
	}
	return written, nil
}
func serializeGenerated(c context.Context, s *model.Context, w io.Writer) (int64, error) {
	output := &generatedOutput{writer: w, context: c}
	err := api.WriteContext(c, s, output)
	if output.err != nil {
		return output.n, fmt.Errorf("pdf: generated page: write: %w", output.err)
	}
	return output.n, err
}

func newGeneratedContext() (*model.Context, error) {
	conf, err := configuration()
	if err != nil {
		return nil, err
	}
	s, err := model.NewContext(bytes.NewReader(nil), conf)
	if err != nil {
		return nil, err
	}
	s.Table[0] = model.NewFreeHeadXRefTableEntry()
	size := 1
	s.Size = &size
	version := model.V17
	s.HeaderVersion = &version
	catalog := types.Dict{"Type": types.Name("Catalog")}
	s.Root, err = s.IndRefForNewObject(catalog)
	if err != nil {
		return nil, err
	}
	root, err := s.IndRefForNewObject(types.Dict{"Type": types.Name("Pages")})
	if err != nil {
		return nil, err
	}
	catalog["Pages"] = *root
	return s, nil
}

// generatedContext builds one native resource document. A one-page slice repeats
// to count; otherwise each supplied page contributes exactly one physical page.
func generatedContext(c context.Context, pages []compose.Page, count int, prepared map[typeset.Digest]typeset.EmbeddedFont, names map[typeset.Digest]string) (*model.Context, []generatedLink, error) {
	if count < 1 || len(pages) == 0 {
		return nil, nil, ErrNoPages
	}
	if count > MaxPages {
		return nil, nil, ErrTooManyPages
	}
	if len(pages) != 1 && len(pages) != count {
		return nil, nil, ErrInvalidPage
	}
	if err := c.Err(); err != nil {
		return nil, nil, err
	}
	fonts, err := collectFonts(c, pages)
	if err != nil {
		return nil, nil, err
	}
	s, err := newGeneratedContext()
	if err != nil {
		return nil, nil, err
	}
	doc := &document{fonts: fonts}
	resources := types.Dict{"Font": types.Dict{}}
	fontDict := types.Dict{}
	resources["Font"] = fontDict
	for index, font := range fonts.list {
		if err = c.Err(); err != nil {
			return nil, nil, err
		}
		embedded := typeset.EmbeddedFont{Font: font, Program: font.Bytes(), Glyphs: doc.usedGlyphs(index)}
		if prepared != nil {
			var ok bool
			embedded, ok = prepared[font.Identity()]
			if !ok {
				return nil, nil, ErrInvalidPage
			}
		}
		name := font.PostScriptName()
		if assigned, ok := names[font.Identity()]; ok {
			name = assigned
		}
		ref, e := buildFontResource(c, s, embedded, name)
		if e != nil {
			return nil, nil, e
		}
		fontDict[fmt.Sprintf("F%d", index+1)] = *ref
	}
	resourceRef, err := s.IndRefForNewObject(resources)
	if err != nil {
		return nil, nil, err
	}
	refs := make([]types.IndirectRef, 0, count)
	var pending []generatedLink
	var repeatedContent *types.IndirectRef
	for index := range count {
		if err = c.Err(); err != nil {
			return nil, nil, err
		}
		page := pages[0]
		if len(pages) > 1 {
			page = pages[index]
		}
		pageDict := types.Dict{"Type": types.Name("Page"), "MediaBox": types.NewNumberArray(0, 0, page.Width, page.Height), "Resources": *resourceRef}
		pageRef, e := s.IndRefForNewObject(pageDict)
		if e != nil {
			return nil, nil, e
		}
		refs = append(refs, *pageRef)
		contentRef := repeatedContent
		if contentRef == nil || len(pages) > 1 {
			doc.content = nil
			doc.appendContent(page)
			contentRef, e = addStream(s, doc.content, nil)
			if e != nil {
				return nil, nil, e
			}
			if len(pages) == 1 {
				repeatedContent = contentRef
			}
		}
		pageDict["Contents"] = *contentRef
		if len(page.Links) > 0 {
			annots := types.Array{}
			for _, link := range page.Links {
				r := link.Rect
				annotation := types.Dict{"Type": types.Name("Annot"), "Subtype": types.Name("Link"), "Rect": types.NewNumberArray(r.X, r.Y, r.X+r.Width, r.Y+r.Height), "Border": types.NewIntegerArray(0, 0, 0), "Dest": types.Array{*pageRef, types.Name("Fit")}}
				ref, e := s.IndRefForNewObject(annotation)
				if e != nil {
					return nil, nil, e
				}
				annots = append(annots, *ref)
				pending = append(pending, generatedLink{dict: annotation, target: link.TargetPage})
			}
			pageDict["Annots"] = annots
		}
	}
	if err = buildGeneratedPageTree(c, s, refs); err != nil {
		return nil, nil, err
	}
	s.PageCount = count
	if err = api.ValidateContext(c, s); err != nil {
		return nil, nil, err
	}
	return s, pending, nil
}

func addStream(s *model.Context, data []byte, extra types.Dict) (*types.IndirectRef, error) {
	stream, err := s.NewStreamDictForBuf(data)
	if err != nil {
		return nil, err
	}
	for key, value := range extra {
		stream.Dict[key] = value
	}
	if err = stream.Encode(); err != nil {
		return nil, err
	}
	return s.IndRefForNewObject(*stream)
}

func buildGeneratedPageTree(c context.Context, s *model.Context, refs []types.IndirectRef) error {
	root, err := s.Pages()
	if err != nil {
		return err
	}
	type node struct {
		ref   types.IndirectRef
		count int
	}
	level := make([]node, len(refs))
	for i, ref := range refs {
		level[i] = node{ref: ref, count: 1}
	}
	for len(level) > 64 {
		var upper []node
		for start := 0; start < len(level); start += 64 {
			if err = c.Err(); err != nil {
				return err
			}
			dict := types.Dict{"Type": types.Name("Pages")}
			ref, e := s.IndRefForNewObject(dict)
			if e != nil {
				return e
			}
			kids := types.Array{}
			count := 0
			for _, child := range level[start:min(start+64, len(level))] {
				kids = append(kids, child.ref)
				count += child.count
				d, e := s.DereferenceDict(child.ref)
				if e != nil {
					return e
				}
				d["Parent"] = *ref
			}
			dict["Kids"] = kids
			dict["Count"] = types.Integer(count)
			upper = append(upper, node{ref: *ref, count: count})
		}
		level = upper
	}
	dict, err := s.DereferenceDict(*root)
	if err != nil {
		return err
	}
	kids := types.Array{}
	count := 0
	for _, child := range level {
		kids = append(kids, child.ref)
		count += child.count
		d, e := s.DereferenceDict(child.ref)
		if e != nil {
			return e
		}
		d["Parent"] = *root
	}
	dict["Kids"] = kids
	dict["Count"] = types.Integer(count)
	return nil
}

func finalizeGeneratedLinks(c context.Context, s *model.Context, links []generatedLink) error {
	for _, link := range links {
		if err := c.Err(); err != nil {
			return err
		}
		if link.target < 1 || link.target > int64(s.PageCount) {
			return ErrInvalidPage
		}
		_, ref, _, err := s.PageDict(c, int(link.target), false)
		if err != nil {
			return err
		}
		link.dict["Dest"] = types.Array{*ref, types.Name("Fit")}
	}
	return nil
}

func buildFontResource(c context.Context, s *model.Context, font typeset.EmbeddedFont, name string) (*types.IndirectRef, error) {
	if err := ValidateFontName(font.Font); err != nil {
		return nil, err
	}
	dictionaries := make([]types.Dict, 5)
	refs := make([]types.IndirectRef, 5)
	for index := range dictionaries {
		dictionaries[index] = types.Dict{}
		ref, err := s.IndRefForNewObject(dictionaries[index])
		if err != nil {
			return nil, err
		}
		refs[index] = *ref
	}
	f := font.Font
	perEm := thousandthsPerEm / float64(f.UnitsPerEm())
	widths := types.Array{}
	for _, g := range font.Glyphs {
		if err := c.Err(); err != nil {
			return nil, err
		}
		widths = append(widths, types.Integer(g), types.Array{types.Float(float64(f.GlyphAdvance(g)) * perEm)})
	}
	dictionaries[0]["Type"] = types.Name("Font")
	dictionaries[0]["Subtype"] = types.Name("Type0")
	dictionaries[0]["BaseFont"] = types.Name(name)
	dictionaries[0]["Encoding"] = types.Name("Identity-H")
	dictionaries[0]["DescendantFonts"] = types.Array{refs[1]}
	dictionaries[0]["ToUnicode"] = refs[4]
	dictionaries[1]["Type"] = types.Name("Font")
	dictionaries[1]["Subtype"] = types.Name("CIDFontType2")
	dictionaries[1]["BaseFont"] = types.Name(name)
	dictionaries[1]["CIDSystemInfo"] = types.Dict{"Registry": types.StringLiteral("Adobe"), "Ordering": types.StringLiteral("Identity"), "Supplement": types.Integer(0)}
	dictionaries[1]["FontDescriptor"] = refs[2]
	dictionaries[1]["DW"] = types.Integer(1000)
	dictionaries[1]["W"] = widths
	dictionaries[1]["CIDToGIDMap"] = types.Name("Identity")
	box := f.BBox()
	dictionaries[2]["Type"] = types.Name("FontDescriptor")
	dictionaries[2]["FontName"] = types.Name(name)
	traits := f.Traits()
	flags := 4
	if traits.FixedPitch {
		flags |= 1
	}
	if traits.ItalicOrOblique {
		flags |= 64
	}
	dictionaries[2]["Flags"] = types.Integer(flags)
	dictionaries[2]["FontBBox"] = types.NewNumberArray(float64(box[0])*perEm, float64(box[1])*perEm, float64(box[2])*perEm, float64(box[3])*perEm)
	dictionaries[2]["ItalicAngle"] = types.Float(traits.ItalicAngle)
	dictionaries[2]["Ascent"] = types.Float(f.Ascent() * perEm)
	dictionaries[2]["Descent"] = types.Float(f.Descent() * perEm)
	dictionaries[2]["CapHeight"] = types.Float(f.CapHeight() * perEm)
	dictionaries[2]["StemV"] = types.Integer(80)
	dictionaries[2]["FontFile2"] = refs[3]
	for _, stream := range []struct {
		index int
		data  []byte
		extra types.Dict
	}{{3, font.Program, types.Dict{"Length1": types.Integer(len(font.Program))}}, {4, toUnicodeCMap(f, font.Glyphs), nil}} {
		sd, err := s.NewStreamDictForBuf(stream.data)
		if err != nil {
			return nil, err
		}
		for key, value := range stream.extra {
			sd.Dict[key] = value
		}
		if err = sd.Encode(); err != nil {
			return nil, err
		}
		s.Table[refs[stream.index].ObjectNumber.Value()].Object = *sd
	}
	return &refs[0], nil
}

func (d *document) appendContent(page compose.Page) {
	if page.Background != nil {
		d.appendColor(*page.Background)
		d.content = fmt.Appendf(d.content, "0 0 %s %s re\nf\n", number(page.Width), number(page.Height))
	}
	for _, block := range page.Blocks {
		if block.Text == nil || len(block.Text.Lines) == 0 {
			continue
		}
		font := d.fonts.index[block.Text.Font.Identity()]
		d.appendColor(block.Color)
		d.appendText(block.Text, font)
	}
}

func (d *document) appendColor(color packet.Color) {
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
	key := fmt.Sprintf("F%d", fontIndex+1)
	if len(d.fontKeys) > fontIndex {
		key = d.fontKeys[fontIndex]
	}
	d.content = fmt.Appendf(d.content, "BT\n/%s ", key)
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
