// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

type (
	// Glyph is one positioned glyph. All distances are font units.
	Glyph struct {
		ID uint16
		// Advance is the pen advance after the glyph, with kerning applied.
		Advance int
		// XOffset and YOffset move the drawn glyph from the pen; GPOS mark attachment lands here.
		XOffset, YOffset int
	}

	// Cluster is the unit that maps glyphs back to source text: the glyphs that one run of source
	// characters shaped to (a base and its marks, a ligature, or a single character).
	Cluster struct {
		Text   string
		Glyphs []Glyph
	}

	// Shaper holds reusable shaping state. It is not safe for concurrent use; use one per goroutine.
	// A Font may be shared between Shapers.
	//
	// go-text's Face carries a mutable glyph cache, so a Face must not be shared between goroutines even
	// though the parsed font is immutable; each Shaper therefore owns a private Face per Font.
	Shaper struct {
		fonts    map[*Font]*shaperFont
		harfbuzz shaping.HarfbuzzShaper
		segment  shaping.Segmenter
	}

	// shaperFont is a Shaper's private state for one Font: its Face and the glyphs whose outlines
	// were already checked, so that each glyph is checked once per Shaper however often it is used.
	shaperFont struct {
		face    *gtfont.Face
		checked map[uint16]struct{}
	}

	singleFace struct{ face *gtfont.Face }
)

// unitsPerPixel converts shaper output: Size is one em of upem pixels, so a 26.6 value is 64 font units per unit.
const unitsPerPixel = 64

// IsSpace reports whether the cluster is exactly one U+0020, the only breaking and justification space.
func (c Cluster) IsSpace() bool { return c.Text == " " }

func (c Cluster) advance() int {
	total := 0
	for _, glyph := range c.Glyphs {
		total += glyph.Advance
	}

	return total
}

func (s *Shaper) fontState(font *Font) *shaperFont {
	state, ok := s.fonts[font]
	if ok {
		return state
	}

	if s.fonts == nil {
		s.fonts = map[*Font]*shaperFont{}
	}

	state = &shaperFont{face: gtfont.NewFace(font.face.Font), checked: map[uint16]struct{}{}}
	s.fonts[font] = state

	return state
}

func (o singleFace) ResolveFace(rune) *gtfont.Face { return o.face }

// protect runs run and turns a panic of the parsing library, which malformed font data can provoke, into an
// error wrapping sentinel.
func protect(sentinel error, run func() error) error {
	var err error

	func() {
		defer func() {
			recovered := recover()
			if recovered != nil {
				err = fmt.Errorf("%w: parser panic: %v", sentinel, recovered)
			}
		}()

		err = run()
	}()

	return err
}

// shapeParagraph shapes one line-break-free paragraph into clusters. offset is the paragraph's rune
// offset in the text, used only for error positions. It never substitutes or drops text.
func (s *Shaper) shapeParagraph(font *Font, paragraph string, offset int, lang language.Language) ([]Cluster, error) {
	var clusters []Cluster

	err := protect(ErrShapingFailed, func() error {
		var shapeErr error

		clusters, shapeErr = s.shapeChecked(font, paragraph, offset, lang)

		return shapeErr
	})
	if err != nil {
		return nil, err
	}

	return clusters, nil
}

func (s *Shaper) shapeChecked(font *Font, paragraph string, offset int, lang language.Language) ([]Cluster, error) {
	runes := []rune(paragraph)
	if len(runes) == 0 {
		return nil, nil
	}

	state := s.fontState(font)
	input := shaping.Input{
		Text: runes, RunStart: 0, RunEnd: len(runes),
		Direction: di.DirectionLTR, Face: state.face,
		Size: fixed.I(font.upem), Script: language.Latin, Language: lang,
	}

	var (
		clusters []Cluster
		problems []Problem
	)

	for _, run := range s.segment.Split(input, singleFace{state.face}) {
		if run.Direction != di.DirectionLTR || !allowedScript(run.Script) {
			return nil, &TextError{Problems: []Problem{{
				Offset: offset + run.RunStart, Rune: runes[run.RunStart], Reason: "right-to-left or unsupported-script run",
			}}}
		}

		runClusters, runProblems, err := shapeRun(s.harfbuzz.Shape(run), run, runes, offset)
		if err != nil {
			return nil, err
		}

		problems = append(problems, runProblems...)

		clusters = append(clusters, runClusters...)
	}

	if len(problems) > 0 {
		return nil, &TextError{Problems: problems[:min(len(problems), maxReportedProblems)]}
	}

	err := checkRebuilt(clusters, paragraph)
	if err != nil {
		return clusters, err
	}

	return clusters, state.checkOutlines(font, clusters)
}

// checkOutlines verifies that the font can draw every glyph of the clusters. The parsing library drops
// the whole outline table when one glyph of it is damaged, which would otherwise render the text blank
// without an error. Each glyph is checked once; the cache holds at most the font's glyph count.
func (state *shaperFont) checkOutlines(font *Font, clusters []Cluster) error {
	for _, cluster := range clusters {
		for _, glyph := range cluster.Glyphs {
			_, done := state.checked[glyph.ID]
			if done {
				continue
			}

			_, ok := state.face.GlyphDataOutline(gtfont.GID(glyph.ID))
			if !ok {
				first, _ := utf8.DecodeRuneInString(cluster.Text)

				return &OutlineError{Font: font.name, Rune: first, Glyph: glyph.ID}
			}

			state.checked[glyph.ID] = struct{}{}
		}
	}

	return nil
}

// checkRebuilt verifies that the clusters' source texts reassemble the paragraph: shaping must never
// drop, duplicate or reorder characters.
func checkRebuilt(clusters []Cluster, paragraph string) error {
	var rebuilt strings.Builder
	for _, cluster := range clusters {
		rebuilt.WriteString(cluster.Text)
	}

	if rebuilt.String() != paragraph {
		return fmt.Errorf("%w: dropped or reordered text", ErrShapingFailed)
	}

	return nil
}

// shapeRun groups one shaped run's glyphs by source cluster.
func shapeRun(out shaping.Output, run shaping.Input, runes []rune, offset int) ([]Cluster, []Problem, error) {
	var (
		clusters []Cluster
		problems []Problem
	)

	glyphs := out.Glyphs

	for first := 0; first < len(glyphs); {
		next := first + 1
		for next < len(glyphs) && glyphs[next].TextIndex() == glyphs[first].TextIndex() {
			next++
		}

		start, end := glyphs[first].TextIndex(), run.RunEnd
		if next < len(glyphs) {
			end = glyphs[next].TextIndex()
		}

		if start < run.RunStart || end > run.RunEnd || end <= start {
			return nil, nil, fmt.Errorf("%w: inconsistent cluster [%d,%d)", ErrShapingFailed, start, end)
		}

		cluster := Cluster{Text: string(runes[start:end])}

		for _, glyph := range glyphs[first:next] {
			if glyph.GlyphID == 0 || glyph.GlyphID > maxGlyphID {
				problems = append(problems, Problem{
					Offset: offset + start, Rune: runes[start], Reason: "the font has no glyph for this character",
				})

				continue
			}

			cluster.Glyphs = append(cluster.Glyphs, Glyph{
				ID:      uint16(glyph.GlyphID),
				Advance: int(glyph.Advance) / unitsPerPixel,
				XOffset: int(glyph.XOffset) / unitsPerPixel, YOffset: int(glyph.YOffset) / unitsPerPixel,
			})
		}

		clusters = append(clusters, cluster)
		first = next
	}

	return clusters, problems, nil
}
