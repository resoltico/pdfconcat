// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type (
	// rasterOracle checks a rendering against the page specification: background color at the corners,
	// text color among the ink, and the ink inside the reported text bounds and roughly filling them
	// horizontally.
	rasterOracle struct {
		page compose.Page
		dpi  int
	}

	cancelAfter struct {
		cancel   context.CancelFunc
		n, limit int
	}

	failingWriter struct {
		left, written, calls int
		injected             bool
	}
	phaseFailureWriter struct {
		marker, suffix []byte
		written, calls int
		injected       bool
	}
)

var (
	errDisk = errors.New("disk full")

	// errRaster reports a rendering that does not match its page specification.
	errRaster = errors.New("rendering does not match the page")
)

func TestWriteHasExpectedPageAndFontStructure(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)
	pages := []compose.Page{
		textPage(t, f, "Rīgas ļoti"),
		{Width: 100, Height: 50, Background: &packet.Color{R: 255, G: 128, B: 0}},
		{Width: 100, Height: 50},
		{Width: 100, Height: 50, Blocks: []compose.TextBlock{{Text: &typeset.Placed{Font: f, Size: 10}}}},
	}

	path, first := writePDF(t, pages)
	if !bytes.HasPrefix(first, []byte("%PDF-1.7\n%")) || !bytes.HasSuffix(first, []byte("%%EOF\n")) {
		t.Error("header or trailer missing")
	}

	actualPages := 0
	for _, dict := range qpdfDictionaries(t, path) {
		if dict["/Type"] != "/Page" {
			continue
		}
		actualPages++
		box, ok := dict["/MediaBox"].([]any)
		if !ok || len(box) != 4 || box[0] != 0.0 || box[1] != 0.0 || dict["/UserUnit"] != nil {
			t.Fatalf("generated viewport: %v", dict)
		}
	}
	if actualPages != len(pages) {
		t.Fatalf("page dictionaries: %d", actualPages)
	}
	if count := embeddedPrograms(t, path); count != 1 {
		t.Fatalf("embedded fonts: %d", count)
	}

}

func embeddedPrograms(t *testing.T, path string) int {
	t.Helper()
	count := 0
	for _, dict := range qpdfDictionaries(t, path) {
		if dict["/FontFile2"] != nil {
			count++
		}
	}
	return count
}

func TestIndependentValidationAndExtraction(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)
	texts := []string{
		"ā vs ā", "ļ vs ļ", "á vs á", "Ελληνικά", "Привет",
		"Rīgas ļoti Ēdīgas ķirbis", "x́ q̄ z̧ t̄́ Ẃ",
		"no break", "wrapped paragraph\nsecond paragraph",
	}

	pages := make([]compose.Page, 0, len(texts))
	for _, text := range texts {
		pages = append(pages, textPage(t, f, text))
	}

	path, _ := writePDF(t, pages)

	if got := qpdfCheck(t, path); got != len(texts) {
		t.Fatalf("qpdf counts %d pages, want %d", got, len(texts))
	}

	got := extractRaw(t, path)
	if len(got) != len(texts) {
		t.Fatalf("pdftotext returned %d pages", len(got))
	}

	for i, want := range texts {
		// pdftotext -raw ends each line with LF and may place NBSP as a plain space; compare the
		// rest exactly, decomposed marks included.
		want = strings.ReplaceAll(want, " ", " ")
		if g := strings.TrimRight(got[i], "\n"); g != want {
			t.Errorf("page %d: extracted %q, want %q", i+1, g, want)
		}
	}
}

func TestJustifiedAndWrappedTextExtractsExactly(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)
	p := params("aaa bbb ccc ddd eee fff ggg hhh iii jjj")
	p.WrapWidth, p.Align = 90, typeset.AlignJustify
	block := placed(t, f, p)

	path, _ := writePDF(t, []compose.Page{{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}})

	want := lineTexts(block)

	if got := strings.TrimRight(extractRaw(t, path)[0], "\n"); got != strings.Join(want, "\n") || len(want) < 3 {
		t.Errorf("extracted %q, want lines %q", got, want)
	}
}

// Marks must sit where the font's mark attachment puts them. Without a precomposed glyph, only GPOS
// places the mark: the shaped ink stays over its base and rises above it, while the same glyphs
// without their offsets spill to the right of the base.
func TestRasterMarkPositioning(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)

	render := func(block *typeset.Placed) bitmap {
		path, _ := writePDF(t, []compose.Page{{Width: 120, Height: 80, Blocks: []compose.TextBlock{{Text: block}}}})

		return renderGray(t, path)
	}
	single := func(text string) *typeset.Placed {
		p := params(text)
		p.Size, p.PageWidth, p.PageHeight, p.OffsetX, p.OffsetY = 48, 120, 80, 20, -10

		return placed(t, f, p)
	}
	inkBox := func(page bitmap) (int, int) {
		box, found := page.ink(128)
		if !found {
			t.Fatal("no ink")
		}

		return box.right - box.left + 1, box.top
	}

	for _, base := range []string{"x", "q", "t"} {
		for _, mark := range []string{"\u0301", "\u0304"} {
			baseW, baseTop := inkBox(render(single(base)))
			shaped := render(single(base + mark))
			shapedW, shapedTop := inkBox(shaped)
			naive := render(withoutMarkOffsets(single(base + mark)))

			if abs(shapedW-baseW) > baseW/5 || shapedTop >= baseTop {
				t.Errorf(
					"%q+%U: shaped ink width %d top %d, base alone %d top %d",
					base,
					firstRune(mark),
					shapedW,
					shapedTop,
					baseW,
					baseTop,
				)
			}

			// Displacing the mark moves at least about 60 full-ink pixels.
			if diff := shaped.sumAbsDiff(naive); diff < 255*60 {
				t.Errorf("%q+%U: naive placement differs from shaped by only %d", base, firstRune(mark), diff)
			}
		}
	}

	// A precomposed glyph and its composed decomposition render alike.
	pre, dec := render(single("ā")), render(single("a\u0304"))
	if diff := pre.sumAbsDiff(dec); diff > 255*pre.w*pre.h/200 {
		t.Errorf("precomposed and decomposed rendering differ by %d", diff)
	}
}

func withoutMarkOffsets(p *typeset.Placed) *typeset.Placed {
	out := *p
	out.Lines = make([]typeset.Line, len(p.Lines))

	for i, line := range p.Lines {
		line.Clusters = append([]typeset.Cluster(nil), line.Clusters...)

		for j, c := range line.Clusters {
			c.Glyphs = append([]typeset.Glyph(nil), c.Glyphs...)
			for k := range c.Glyphs {
				c.Glyphs[k].XOffset, c.Glyphs[k].YOffset = 0, 0
			}

			line.Clusters[j] = c
		}

		out.Lines[i] = line
	}

	return &out
}

func TestRasterColorAndPlacement(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)

	p := params("HHHH")
	p.Size, p.PageWidth, p.PageHeight, p.OffsetX, p.OffsetY = 60, 300, 200, 40, -30
	block := placed(t, f, p)
	page := compose.Page{
		Width: 300, Height: 200, Background: &packet.Color{R: 250, G: 240, B: 200}, Blocks: []compose.TextBlock{{Text: block, Color: packet.Color{R: 200, G: 0, B: 40}}},
	}
	path, _ := writePDF(t, []compose.Page{page})

	const dpi = 144

	oracle := rasterOracle{dpi: dpi, page: page}
	if err := oracle.check(t, path); err != nil {
		t.Fatalf("correct rendering rejected: %v", err)
	}

	// Negative controls: the oracle must reject wrong color and wrong placement.
	wrongColor := page
	wrongColor.Blocks = append([]compose.TextBlock(nil), page.Blocks...)
	wrongColor.Blocks[0].Color = packet.Color{R: 0, G: 0, B: 200}

	if err := (rasterOracle{dpi: dpi, page: wrongColor}).check(t, path); err == nil {
		t.Error("oracle accepted the wrong text color")
	}

	wrongBackground := page
	wrongBackground.Background = &packet.Color{R: 10, G: 10, B: 10}

	if err := (rasterOracle{dpi: dpi, page: wrongBackground}).check(t, path); err == nil {
		t.Error("oracle accepted the wrong background")
	}

	moved := page
	moved.Blocks = append([]compose.TextBlock(nil), page.Blocks...)
	movedBlock := *block
	movedBlock.Bounds.X += 60
	moved.Blocks[0].Text = &movedBlock

	if err := (rasterOracle{dpi: dpi, page: moved}).check(t, path); err == nil {
		t.Error("oracle accepted the wrong placement")
	}
}

func (o rasterOracle) check(t *testing.T, path string) error {
	t.Helper()

	img := renderRGB(t, path, o.dpi)
	scale := float64(o.dpi) / 72

	if want := int(math.Round(o.page.Width * scale)); img.w != want {
		return fmt.Errorf("%w: page rendered %d px wide, want %d", errRaster, img.w, want)
	}

	if background := o.page.Background; background != nil && !closeTo(img.rgb(1, 1), *background) {
		return fmt.Errorf("%w: background %v, want %v", errRaster, img.rgb(1, 1), *background)
	}

	box, found := inkAgainst(img, o.page.Background)
	if !found {
		return fmt.Errorf("%w: no ink", errRaster)
	}

	err := o.checkInkPlacement(box, scale)
	if err != nil {
		return err
	}

	// A fully covered pixel of the text color exists.
	for y := box.top; y <= box.bottom; y++ {
		for x := box.left; x <= box.right; x++ {
			if closeTo(img.rgb(x, y), o.page.Blocks[0].Color) {
				return nil
			}
		}
	}

	return fmt.Errorf("%w: no pixel of text color %v", errRaster, o.page.Blocks[0].Color)
}

// checkInkPlacement checks that the ink lies inside the text bounds of the page specification and starts
// near their left edge.
func (o rasterOracle) checkInkPlacement(ink extent, scale float64) error {
	bounds := o.page.Blocks[0].Text.Bounds
	left, right := int(bounds.X*scale)-2, int((bounds.X+bounds.Width)*scale)+2
	top, bottom := int((o.page.Height-bounds.Y-bounds.Height)*scale)-2, int((o.page.Height-bounds.Y)*scale)+2

	if ink.left < left || ink.right > right || ink.top < top || ink.bottom > bottom {
		return fmt.Errorf("%w: ink %+v outside expected bounds [%d,%d]-[%d,%d]", errRaster, ink, left, top, right, bottom)
	}

	if ink.left > left+int(0.15*scale*o.page.Blocks[0].Text.Size) {
		return fmt.Errorf("%w: ink starts at %d, expected near %d", errRaster, ink.left, left)
	}

	return nil
}

// rgb returns the pixel at x, y of an RGB bitmap.
func (b bitmap) rgb(x, y int) [3]int {
	index := 3 * (y*b.w + x)

	return [3]int{int(b.pix[index]), int(b.pix[index+1]), int(b.pix[index+2])}
}

// closeTo reports whether a pixel is within rounding of a color.
func closeTo(pixel [3]int, color packet.Color) bool {
	return abs(pixel[0]-int(color.R)) <= 3 && abs(pixel[1]-int(color.G)) <= 3 && abs(pixel[2]-int(color.B)) <= 3
}

func abs(value int) int { return max(value, -value) }

// firstRune returns the first character of text.
func firstRune(text string) rune {
	char, _ := utf8.DecodeRuneInString(text)

	return char
}

// lineTexts returns the text of every line of a block without trailing spaces, which extraction drops.
func lineTexts(block *typeset.Placed) []string {
	texts := make([]string, 0, len(block.Lines))
	for _, line := range block.Lines {
		texts = append(texts, strings.TrimRight(line.Text(), " "))
	}

	return texts
}

// inkAgainst returns the box of pixels of an RGB bitmap that differ from the background.
func inkAgainst(img bitmap, background *packet.Color) (extent, bool) {
	base := [3]int{255, 255, 255}

	if background != nil {
		base = [3]int{int(background.R), int(background.G), int(background.B)}
	}

	box := extent{left: img.w, top: img.h, right: -1, bottom: -1}
	found := false

	for y := range img.h {
		for x := range img.w {
			pixel := img.rgb(x, y)
			if abs(pixel[0]-base[0])+abs(pixel[1]-base[1])+abs(pixel[2]-base[2]) > 60 {
				box = extent{left: min(box.left, x), top: min(box.top, y), right: max(box.right, x), bottom: max(box.bottom, y)}
				found = true
			}
		}
	}

	return box, found
}

func distinctPages(tb testing.TB, font *typeset.Font, count int) []compose.Page {
	tb.Helper()

	var shaper typeset.Shaper

	pages := make([]compose.Page, count)
	for index := range pages {
		text := fmt.Sprintf("Lappuse %d no %d\nĀčēģī ļoti Ελληνικά Привет %d\nx́ q̄ z̧", index+1, count, index*7919)
		spec := params(text)
		spec.WrapWidth = 270

		block, err := shaper.Place(font, spec)
		if err != nil {
			tb.Fatal(err)
		}

		pages[index] = compose.Page{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block, Color: packet.Color{R: 1, G: 2, B: 3}}}}
	}

	return pages
}

func TestThousandPagesShareOneFont(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)
	pages := distinctPages(t, f, 1000)
	path, data := writePDF(t, pages)

	if n := embeddedPrograms(t, path); n != 1 {
		t.Errorf("FontFile2 references: %d", n)
	}

	// One shared font plus about 2 KB of structure and compressed text per page.
	if limit := len(f.Bytes()) + 1000*2048; len(data) > limit {
		t.Errorf("1,000-page resource is %d bytes, above %d", len(data), limit)
	}

	if got := qpdfCheck(t, path); got != 1000 {
		t.Fatalf("qpdf counts %d pages", got)
	}

	if fonts := strings.Count(string(run(t, qpdfTool, "--qdf", "--object-streams=disable", path, "-")), "/Subtype /Type0"); fonts != 1 {
		t.Errorf("Type0 fonts: %d", fonts)
	}

	extracted := extractRaw(t, path)

	for _, i := range []int{0, 1, 499, 998, 999} {
		want := lineTexts(pages[i].Blocks[0].Text)

		if got := strings.TrimRight(extracted[i], "\n"); got != strings.Join(want, "\n") {
			t.Errorf("page %d: %q vs %q", i+1, got, want)
		}
	}
}

func TestDistinctFontsAreEmbeddedOnceEach(t *testing.T) {
	t.Parallel()

	first := defaultFont(t)
	sameBytes := defaultFont(t) // another pointer with the same identity

	other, err := typeset.LoadFont(altered(first))
	if err != nil {
		t.Fatal(err)
	}

	pages := []compose.Page{
		textPage(t, first, firstPageText),
		textPage(t, other, secondPageText),
		textPage(t, sameBytes, "three"),
		textPage(t, other, "four"),
	}
	path, _ := writePDF(t, pages)

	if n := embeddedPrograms(t, path); n != 2 {
		t.Errorf("FontFile2 references: %d, want 2", n)
	}

	qpdfCheck(t, path)

	got := extractRaw(t, path)
	for i, want := range []string{firstPageText, secondPageText, "three", "four"} {
		if strings.TrimSpace(got[i]) != want {
			t.Errorf("page %d: %q", i+1, got[i])
		}
	}
}

// altered returns the font with a harmless header change, hence another identity.
func altered(f *typeset.Font) []byte {
	data := append([]byte(nil), f.Bytes()...)
	data[len(data)-1] ^= 0xFF // the last byte lies in a table padding or data not read by validation

	return data
}

func TestWriteRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	f := defaultFont(t)
	good := textPage(t, f, "x")

	badSize := func(w, h float64) []compose.Page { return []compose.Page{good, {Width: w, Height: h}} }
	noFont := compose.Page{Width: 10, Height: 10, Blocks: []compose.TextBlock{{Text: &typeset.Placed{Size: 12, Lines: good.Blocks[0].Text.Lines}}}}
	noSize := compose.Page{Width: 10, Height: 10, Blocks: []compose.TextBlock{{Text: &typeset.Placed{Font: f, Lines: good.Blocks[0].Text.Lines}}}}

	cases := map[string][]compose.Page{
		"no pages":      nil,
		"NaN width":     badSize(math.NaN(), 10),
		"infinite":      badSize(10, math.Inf(1)),
		"too small":     badSize(0.5, 10),
		"too large":     badSize(10, 14401),
		"text no font":  {noFont},
		"text no size":  {noSize},
		"negative size": {{Width: 10, Height: 10, Blocks: []compose.TextBlock{{Text: &typeset.Placed{Font: f, Size: -1, Lines: good.Blocks[0].Text.Lines}}}}},
	}

	for name, pages := range cases {
		var buf bytes.Buffer
		if _, err := pdf.Write(context.Background(), &buf, pages); err == nil || buf.Len() != 0 {
			t.Errorf("%s: err=%v bytes=%d", name, err, buf.Len())
		}
	}

	tooMany := make([]compose.Page, pdf.MaxPages+1)
	if _, err := pdf.Write(context.Background(), io.Discard, tooMany); !errors.Is(err, pdf.ErrTooManyPages) {
		t.Errorf("too many pages: %v", err)
	}
}

func TestPageSizeBoundariesAreAccepted(t *testing.T) {
	t.Parallel()
	for _, side := range []float64{typeset.MinPageSide, 595.276, typeset.MaxPageSide} {
		path, _ := writePDF(t, []compose.Page{{Width: side, Height: side}})
		assertPageDimensions(t, path, side, side)
		qpdfCheck(t, path)
	}
}

func assertPageDimensions(t *testing.T, path string, width, height float64) {
	t.Helper()
	found := false
	for _, dict := range qpdfDictionaries(t, path) {
		if dict["/Type"] != "/Page" {
			continue
		}
		found = true
		box, ok := dict["/MediaBox"].([]any)
		if !ok || len(box) != 4 || box[0] != 0.0 || box[1] != 0.0 || box[2] != width || box[3] != height {
			t.Fatalf("page dimensions: %v", dict["/MediaBox"])
		}
	}
	if !found {
		t.Fatal("missing final page dictionary")
	}
}

func TestCancellation(t *testing.T) {
	t.Parallel()

	pages := distinctPages(t, defaultFont(t), 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	if _, err := pdf.Write(ctx, &buf, pages); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled before start: %v", err)
	}

	// Cancel during output after a fixed transport byte threshold is crossed.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	trigger := &cancelAfter{limit: 60_000, cancel: cancel}
	if _, err := pdf.Write(ctx, trigger, distinctPages(t, defaultFont(t), 200)); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled while writing: %v", err)
	}

	if trigger.n > 100_000 {
		t.Errorf("kept writing after cancellation: %d bytes", trigger.n)
	}
}

func (c *cancelAfter) Write(p []byte) (int, error) {
	c.n += len(p)
	if c.n > c.limit {
		c.cancel()
	}

	return len(p), nil
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.injected {
		return 0, errDisk
	}
	if len(p) > w.left {
		n := w.left
		w.written += n
		w.left = 0
		w.injected = true
		return n, errDisk
	}
	w.left -= len(p)
	w.written += len(p)
	return len(p), nil
}

// phaseFailureWriter refuses a marker from this serialization, including when
// its prefix was accepted in earlier writes. It never relies on another PDF's
// length or the backend's chunk boundaries.
func (w *phaseFailureWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.injected {
		return 0, errDisk
	}
	combined := append(bytes.Clone(w.suffix), p...)
	if index := bytes.Index(combined, w.marker); index >= 0 {
		n := max(0, index-len(w.suffix))
		w.written += n
		w.injected = true
		return n, errDisk
	}
	w.suffix = bytes.Clone(combined[max(0, len(combined)-len(w.marker)+1):])
	w.written += len(p)
	return len(p), nil
}

func TestWriteReportsWriterFailures(t *testing.T) {
	t.Parallel()
	pages := distinctPages(t, defaultFont(t), 300)
	// These fixed budgets are crossed by this full-font fixture. The explicit
	// injection assertion detects a fixture change that stops exercising failure.
	for _, budget := range []int{0, 1000, 70000, 130000} {
		writer := &failingWriter{left: budget}
		written, err := pdf.Write(t.Context(), writer, pages)
		if !writer.injected || writer.calls == 0 || writer.written != budget || written != int64(writer.written) || !errors.Is(err, errDisk) {
			t.Fatalf("budget %d: injected=%v accepted=%d reported=%d calls=%d error=%v", budget, writer.injected, writer.written, written, writer.calls, err)
		}
		t.Logf("budget %d injected after %d bytes across %d writes", budget, writer.written, writer.calls)
	}
	for _, phase := range []struct{ name, marker string }{{"trailer", "\nstartxref\n"}, {"EOF", "\n%%EOF\n"}} {
		writer := &phaseFailureWriter{marker: []byte(phase.marker)}
		written, err := pdf.Write(t.Context(), writer, pages)
		if !writer.injected || writer.calls == 0 || writer.written == 0 || written != int64(writer.written) || !errors.Is(err, errDisk) {
			t.Fatalf("%s: injected=%v accepted=%d reported=%d calls=%d error=%v", phase.name, writer.injected, writer.written, written, writer.calls, err)
		}
		t.Logf("%s injected after %d bytes across %d writes", phase.name, writer.written, writer.calls)
	}
}

func TestWriterFailureMarkerSpansTransportWrites(t *testing.T) {
	for _, marker := range []string{"\nstartxref\n", "\n%%EOF\n"} {
		// Exercise the real serializer through one-byte transport writes, ensuring
		// marker detection spans calls instead of assuming a particular buffer size.
		pages := []compose.Page{{Width: 10, Height: 10}}
		writer := &phaseFailureWriter{marker: []byte(marker)}
		split := singleByteWriter{writer: writer}
		written, err := pdf.Write(t.Context(), split, pages)
		if !writer.injected || writer.calls < len(marker) || written != int64(writer.written) || !errors.Is(err, errDisk) {
			t.Fatalf("split %q: injected=%v accepted=%d reported=%d calls=%d error=%v", marker, writer.injected, writer.written, written, writer.calls, err)
		}
	}
}

type singleByteWriter struct{ writer io.Writer }

func (w singleByteWriter) Write(p []byte) (int, error) {
	total := 0
	for index := range p {
		n, err := w.writer.Write(p[index : index+1])
		total += n
		if err != nil {
			return total, err
		}
		if n != 1 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func TestPageDimensionsRetainFractionalPoints(t *testing.T) {
	t.Parallel()
	for _, width := range []float64{1, 100.5, 595.276, 841.8894, 14400, 1.0004, 1.0006, 12.3000001} {
		path, _ := writePDF(t, []compose.Page{{Width: width, Height: 10}})
		assertPageDimensions(t, path, width, 10)
	}
}

func TestBlankLinesProduceNoOperators(t *testing.T) {
	t.Parallel()

	path, _ := writePDF(t, []compose.Page{textPage(t, defaultFont(t), "a\n\nb\n\n\nc")})
	qpdfCheck(t, path)

	got := strings.Join(strings.Fields(extractRaw(t, path)[0]), " ")
	if got != "a b c" {
		t.Errorf("extracted %q", got)
	}
}
