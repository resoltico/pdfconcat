// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/go-text/typesetting/language"
)

type (
	// Anchor is one of nine points of the page to which the text block is attached.
	Anchor int

	// Align places lines inside the block.
	Align int

	// OverflowPolicy decides what a finding means.
	OverflowPolicy int

	// Params describes one text block on one page. Lengths are points; +x is right and +y is up.
	Params struct {
		Text string
		// Language is an optional BCP-47 tag selecting language-specific glyph forms (for example "lv").
		Language string
		// Size is the font size, MinFontSize through MaxFontSize.
		Size float64
		// PageWidth and PageHeight are the page sides, MinPageSide through MaxPageSide.
		PageWidth, PageHeight float64
		// OffsetX and OffsetY move the anchored block; each is within +-MaxOffset.
		OffsetX, OffsetY float64
		// WrapWidth is the block width; 0 means no wrapping (explicit line breaks only).
		WrapWidth float64
		// LineSpacing is the baseline-to-baseline distance as a multiple of Size; it is greater than 0.
		LineSpacing float64
		Anchor      Anchor
		Align       Align
		Overflow    OverflowPolicy
	}

	// InvalidParamError names a Params field outside its domain.
	InvalidParamError struct {
		Field   string
		Message string
	}

	// Rect is a rectangle in page coordinates: its bottom-left corner and size.
	Rect struct {
		X, Y, Width, Height float64
	}

	// Line is one positioned line. X and Baseline are the page coordinates of the first glyph's pen.
	Line struct {
		Clusters []Cluster
		X        float64
		Baseline float64
		// Width is the advance width without trailing spaces, justification included.
		Width float64
		// ExtraSpace is the justification added to each interior U+0020, in points.
		ExtraSpace float64
	}

	// FindingKind classifies an overflow finding.
	FindingKind string

	// Finding is one overflow condition.
	Finding struct {
		Kind   FindingKind
		Detail string
		// Line is the zero-based line of a FindingWordTooWide, otherwise -1.
		Line int
	}

	// OverflowError reports findings under OverflowReject policy.
	OverflowError struct {
		Findings []Finding
	}

	// Placed is a laid-out text block positioned on a page.
	Placed struct {
		Font      *Font
		InkBounds *Rect
		Lines     []Line
		Findings  []Finding
		Bounds    Rect
		Size      float64
	}

	wrappedLine struct {
		clusters []Cluster
		last     bool
	}

	// paramCheck is one domain check of validate: the field, what it must be, and whether it is.
	paramCheck struct {
		field string
		want  string
		ok    bool
	}
)

const (
	// MinFontSize is the smallest font size in points that survives three-decimal output.
	MinFontSize = 0.01
	// MaxFontSize equals the largest generated page side.
	MaxFontSize = 14400.0
	// MinPageSide bounds a generated page side from below, in points.
	MinPageSide = 1.0
	// MaxPageSide bounds a generated page side from above, in points.
	MaxPageSide = 14400.0
	// MaxOffset bounds the absolute x and y offsets and the wrap width in points.
	MaxOffset = 1e6
	// MaxLineSpacing bounds the line spacing multiplier.
	MaxLineSpacing = 100.0

	maxLanguageLength = 35

	// anchorColumns is the number of anchor columns; anchors count row by row.
	anchorColumns = 3

	// AnchorTopLeft places the block's top-left corner on the page's. The nine anchors run row by row from
	// here to AnchorBottomRight; the block's matching point is placed on the page's.
	AnchorTopLeft Anchor = 0
	// AnchorTopCenter anchors the middle of the block's top edge to the middle of the page's.
	AnchorTopCenter Anchor = 1
	// AnchorTopRight anchors the block's top-right corner to the page's.
	AnchorTopRight Anchor = 2
	// AnchorMiddleLeft anchors the middle of the block's left edge to the middle of the page's.
	AnchorMiddleLeft Anchor = 3
	// AnchorCenter anchors the block's center to the page's.
	AnchorCenter Anchor = 4
	// AnchorMiddleRight anchors the middle of the block's right edge to the middle of the page's.
	AnchorMiddleRight Anchor = 5
	// AnchorBottomLeft anchors the block's bottom-left corner to the page's.
	AnchorBottomLeft Anchor = 6
	// AnchorBottomCenter anchors the middle of the block's bottom edge to the middle of the page's.
	AnchorBottomCenter Anchor = 7
	// AnchorBottomRight anchors the block's bottom-right corner to the page's.
	AnchorBottomRight Anchor = 8

	// AlignLeft aligns lines to the block's left edge. The alignments act inside the block.
	AlignLeft Align = 0
	// AlignCenter centers lines in the block.
	AlignCenter Align = 1
	// AlignRight aligns lines to the block's right edge.
	AlignRight Align = 2
	// AlignJustify spreads interior U+0020 spaces so a wrapped line fills the block; the last line of
	// each paragraph stays left-aligned.
	AlignJustify Align = 3

	// OverflowReject turns findings into an error (the plan value "error").
	OverflowReject OverflowPolicy = 0
	// OverflowAllow reports findings and succeeds.
	OverflowAllow OverflowPolicy = 1

	// FindingWordTooWide means a line, necessarily a single unbreakable word, is wider than the wrap width.
	FindingWordTooWide FindingKind = "word-wider-than-box"
	// FindingOutsidePageHorizontal means the block leaves the page on the left or right.
	FindingOutsidePageHorizontal FindingKind = "outside-page-horizontal"
	// FindingOutsidePageVertical means the block leaves the page on the top or bottom (vertical overflow).
	FindingOutsidePageVertical FindingKind = "outside-page-vertical"

	// boundsTolerance absorbs rounding when the block sits exactly on a page edge.
	boundsTolerance = 1e-6
)

// Error implements error.
func (e *InvalidParamError) Error() string {
	return fmt.Sprintf("invalid text parameter %s: %s", e.Field, e.Message)
}

// Text returns the source text of the line.
func (l Line) Text() string {
	var b strings.Builder
	for _, c := range l.Clusters {
		b.WriteString(c.Text)
	}

	return b.String()
}

// Error implements error.
func (e *OverflowError) Error() string {
	parts := make([]string, len(e.Findings))
	for i, f := range e.Findings {
		parts[i] = string(f.Kind) + ": " + f.Detail
	}

	return "text overflows: " + strings.Join(parts, "; ") + `; shorten or wrap the text, move it, or set overflow "allow"`
}

// Place shapes, wraps and positions params.Text with the font. Empty text yields no lines, zero bounds
// and no findings. A rejected text returns a *TextError and invalid numbers an *InvalidParamError,
// both with a nil result. When findings exist under OverflowReject policy, Place returns the
// complete result together with an *OverflowError, so callers can report computed bounds.
func (s *Shaper) Place(font *Font, params Params) (*Placed, error) {
	if font == nil {
		return nil, &InvalidParamError{Field: "font", Message: "must not be nil"}
	}

	err := validate(params)
	if err != nil {
		return nil, err
	}

	text := normalizeNewlines(params.Text)

	problems := checkSupported(text)
	if len(problems) > 0 {
		return nil, &TextError{Problems: problems}
	}

	var lang language.Language
	if params.Language != "" {
		lang = language.NewLanguage(params.Language)
	}

	lines, err := s.wrapParagraphs(font, text, lang, params.WrapWidth*float64(font.upem)/params.Size)
	if err != nil {
		return nil, err
	}

	placed := &Placed{Font: font, Size: params.Size}
	if len(lines) == 0 {
		return placed, nil
	}

	placed.layOut(font, params, lines)

	if params.Overflow == OverflowReject && len(placed.Findings) > 0 {
		return placed, &OverflowError{Findings: placed.Findings}
	}

	return placed, nil
}

// inRange reports whether value lies between low and high, both included. NaN is outside every range.
func inRange(value, low, high float64) bool {
	return value >= low && value <= high
}

// pointsRange describes the interval of inRange for an error message.
func pointsRange(low, high float64) string {
	return fmt.Sprintf("a number from %v to %v points", low, high)
}

func validate(params Params) error {
	checks := []paramCheck{
		{"size", pointsRange(MinFontSize, MaxFontSize), inRange(params.Size, MinFontSize, MaxFontSize)},
		{"page width", pointsRange(MinPageSide, MaxPageSide), inRange(params.PageWidth, MinPageSide, MaxPageSide)},
		{"page height", pointsRange(MinPageSide, MaxPageSide), inRange(params.PageHeight, MinPageSide, MaxPageSide)},
		{"anchor", "one of the nine anchors", params.Anchor >= AnchorTopLeft && params.Anchor <= AnchorBottomRight},
		{"offset x", fmt.Sprintf("a number within +-%v points", MaxOffset), math.Abs(params.OffsetX) <= MaxOffset},
		{"offset y", fmt.Sprintf("a number within +-%v points", MaxOffset), math.Abs(params.OffsetY) <= MaxOffset},
		{
			"wrap width", fmt.Sprintf("0 (no wrapping) or a number up to %v points", MaxOffset),
			inRange(params.WrapWidth, 0, MaxOffset),
		},
		{"align", "left, center, right or justify", params.Align >= AlignLeft && params.Align <= AlignJustify},
		{
			"line spacing", fmt.Sprintf("a number above 0 and up to %v", MaxLineSpacing),
			params.LineSpacing > 0 && params.LineSpacing <= MaxLineSpacing,
		},
		{"overflow", "error or allow", params.Overflow == OverflowReject || params.Overflow == OverflowAllow},
		{"language", "empty or a BCP-47 tag of letters, digits and hyphens", validLanguage(params.Language)},
		{"text", fmt.Sprintf("at most %d characters", MaxTextRunes), utf8.RuneCountInString(params.Text) <= MaxTextRunes},
	}

	for _, check := range checks {
		if !check.ok {
			return &InvalidParamError{Field: check.field, Message: "must be " + check.want}
		}
	}

	return nil
}

func validLanguage(tag string) bool {
	if len(tag) > maxLanguageLength {
		return false
	}

	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}

	return true
}

// wrapParagraphs splits text at LF (a final LF adds no paragraph), shapes each paragraph and wraps it.
func (s *Shaper) wrapParagraphs(font *Font, text string, lang language.Language, limit float64) ([]wrappedLine, error) {
	if text == "" {
		return nil, nil
	}

	paragraphs := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	var lines []wrappedLine

	offset := 0

	for _, paragraph := range paragraphs {
		clusters, err := s.shapeParagraph(font, paragraph, offset, lang)
		if err != nil {
			return nil, err
		}

		offset += utf8.RuneCountInString(paragraph) + 1

		for _, line := range breakLines(clusters, limit) {
			lines = append(lines, wrappedLine{clusters: line})
		}

		lines[len(lines)-1].last = true
	}

	return lines, nil
}

// lineWidth returns the advance width in font units without trailing spaces and the number of
// interior spaces (after the first non-space cluster).
func lineWidth(clusters []Cluster) (float64, int) {
	end := len(clusters)
	for end > 0 && clusters[end-1].IsSpace() {
		end--
	}

	var (
		width          float64
		interiorSpaces int
		seenWord       bool
	)

	for _, cluster := range clusters[:end] {
		switch {
		case !cluster.IsSpace():
			seenWord = true
		case seenWord:
			interiorSpaces++
		default:
		}

		width += float64(cluster.advance())
	}

	return width, interiorSpaces
}

// layOut aligns the lines in the block, anchors the block on the page and records findings.
func (placed *Placed) layOut(font *Font, params Params, lines []wrappedLine) {
	scale := params.Size / float64(font.upem)
	ascent, descent := font.ascent*scale, -font.descent*scale
	advance := params.Size * params.LineSpacing

	widths := make([]float64, len(lines))
	blockWidth := params.WrapWidth

	for index, line := range lines {
		width, _ := lineWidth(line.clusters)
		widths[index] = width * scale
		blockWidth = math.Max(blockWidth, widths[index])
	}

	height := ascent + descent + float64(len(lines)-1)*advance
	left, top := anchorOrigin(params, blockWidth, height)

	for index, line := range lines {
		width := widths[index]

		out := alignLine(params, line, width, blockWidth)
		out.X += left
		out.Baseline = top - ascent - float64(index)*advance

		if params.WrapWidth > 0 && width > params.WrapWidth*(1+fitTolerance) {
			placed.Findings = append(placed.Findings, Finding{
				Kind: FindingWordTooWide, Line: index,
				Detail: fmt.Sprintf("line %d is %.2f pt wide, the box is %.2f pt", index+1, width, params.WrapWidth),
			})
		}

		placed.Lines = append(placed.Lines, out)
	}

	placed.Bounds = Rect{X: left, Y: top - height, Width: blockWidth, Height: height}
	placed.measureInk()
	placed.recordPageFindings(params)
}

// alignLine positions a line of the given width inside a block of blockWidth; X is relative to the block.
func alignLine(params Params, line wrappedLine, width, blockWidth float64) Line {
	_, spaces := lineWidth(line.clusters)
	out := Line{Clusters: line.clusters, Width: width}

	switch {
	case params.Align == AlignCenter:
		out.X = (blockWidth - width) / 2
	case params.Align == AlignRight:
		out.X = blockWidth - width
	case params.Align == AlignJustify && !line.last && spaces > 0 && params.WrapWidth > 0 && width < blockWidth:
		out.ExtraSpace = (blockWidth - width) / float64(spaces)
		out.Width = blockWidth
	default:
	}

	return out
}

// anchorOrigin returns the block's left edge and top edge on the page.
func anchorOrigin(params Params, width, height float64) (float64, float64) {
	column, row := int(params.Anchor)%anchorColumns, int(params.Anchor)/anchorColumns

	var left, top float64

	switch column {
	case 0:
		left = 0
	case 1:
		left = (params.PageWidth - width) / 2
	default:
		left = params.PageWidth - width
	}

	switch row {
	case 0:
		top = params.PageHeight
	case 1:
		top = (params.PageHeight + height) / 2
	default:
		top = height
	}

	return left + params.OffsetX, top + params.OffsetY
}

func pageFindings(bounds Rect, params Params) []Finding {
	var findings []Finding

	if bounds.X < -boundsTolerance || bounds.X+bounds.Width > params.PageWidth+boundsTolerance {
		findings = append(findings, Finding{
			Kind: FindingOutsidePageHorizontal,
			Line: -1,
			Detail: fmt.Sprintf(
				"text spans x %.2f to %.2f pt, the page is %.2f pt wide",
				bounds.X,
				bounds.X+bounds.Width,
				params.PageWidth,
			),
		})
	}

	if bounds.Y < -boundsTolerance || bounds.Y+bounds.Height > params.PageHeight+boundsTolerance {
		findings = append(findings, Finding{
			Kind: FindingOutsidePageVertical,
			Line: -1,
			Detail: fmt.Sprintf(
				"text spans y %.2f to %.2f pt, the page is %.2f pt high",
				bounds.Y,
				bounds.Y+bounds.Height,
				params.PageHeight,
			),
		})
	}

	return findings
}

// measureInk uses font bearings independently of the block's advance-based anchoring geometry.
func (placed *Placed) measureInk() {
	scale := placed.Size / float64(placed.Font.upem)
	for _, line := range placed.Lines {
		placed.measureLineInk(line, scale)
	}
}

func (placed *Placed) measureLineInk(line Line, scale float64) {
	pen, seenWord := line.X, false
	for _, cluster := range line.Clusters {
		for _, glyph := range cluster.Glyphs {
			placed.includeGlyphInk(glyph, pen, line.Baseline, scale)

			pen += float64(glyph.Advance) * scale
			if cluster.IsSpace() && seenWord {
				pen += line.ExtraSpace
			}
		}

		seenWord = seenWord || !cluster.IsSpace()
	}
}

func (placed *Placed) includeGlyphInk(glyph Glyph, pen, baseline, scale float64) {
	extent := placed.Font.ink[glyph.ID]
	if extent.Width == 0 || extent.Height == 0 {
		return
	}

	x := pen + (float64(glyph.XOffset)+extent.X)*scale
	y := baseline + (float64(glyph.YOffset)+extent.Y)*scale

	right, top := x+extent.Width*scale, y+extent.Height*scale
	if placed.InkBounds == nil {
		placed.InkBounds = &Rect{X: x, Y: y, Width: right - x, Height: top - y}
		return
	}

	bounds := placed.InkBounds
	right = math.Max(right, bounds.X+bounds.Width)
	top = math.Max(top, bounds.Y+bounds.Height)
	bounds.X, bounds.Y = math.Min(x, bounds.X), math.Min(y, bounds.Y)
	bounds.Width, bounds.Height = right-bounds.X, top-bounds.Y
}

func (placed *Placed) recordPageFindings(params Params) {
	placed.Findings = append(placed.Findings, pageFindings(placed.Bounds, params)...)
	if placed.InkBounds == nil {
		return
	}

	for _, finding := range pageFindings(*placed.InkBounds, params) {
		known := false
		for _, existing := range placed.Findings {
			known = known || existing.Kind == finding.Kind
		}

		if !known {
			placed.Findings = append(placed.Findings, finding)
		}
	}
}
