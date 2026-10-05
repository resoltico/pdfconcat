// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"context"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/font"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	glyphUnitsPerEm = 1000.0
	byteValues      = 256
)

// verticalMetrics holds the typographic ascender and descender of a font, in glyph units.
type verticalMetrics struct {
	ascent, descent float64
}

// Typographic ascender and descender of the standard font families, in glyph units.
const (
	timesAscent, timesDescent         = 683, 217
	courierAscent, courierDescent     = 629, 157
	helveticaAscent, helveticaDescent = 718, 207
)

// half converts a total amount of slack into the share on one side.
const half = 0.5

func fontVerticalMetrics(name assembly.Font) verticalMetrics {
	switch {
	case strings.HasPrefix(string(name), "Times"):
		return verticalMetrics{ascent: timesAscent, descent: timesDescent}
	case strings.HasPrefix(string(name), "Courier"):
		return verticalMetrics{ascent: courierAscent, descent: courierDescent}
	default:
		return verticalMetrics{ascent: helveticaAscent, descent: helveticaDescent}
	}
}

// textLine is one laid-out line of text with its horizontal placement.
type textLine struct {
	bytes       []byte
	x           float64
	baseline    float64
	wordSpacing float64
}

// glyphWidths returns the advance width of every byte of the font, in glyph units.
func glyphWidths(ctx context.Context, name assembly.Font) ([byteValues]float64, error) {
	var widths [byteValues]float64
	for code := range widths {
		width, err := font.CharWidth(ctx, string(name), rune(code))
		if err != nil {
			return widths, fmt.Errorf("measure font %s: %w", name, err)
		}

		widths[code] = float64(width)
	}

	return widths, nil
}

type measuredLine struct {
	bytes   []byte
	width   float64
	spaces  int
	endsPar bool
}

// wrapText breaks the text into lines no wider than boxWidth where possible.
// Newlines start new paragraphs; runs of spaces collapse to one space.
func wrapText(value string, widths *[byteValues]float64, size, boxWidth float64) ([]measuredLine, error) {
	var lines []measuredLine

	spaceWidth := widths[' '] * size / glyphUnitsPerEm

	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	for paragraph := range strings.SplitSeq(normalized, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, measuredLine{endsPar: true})
			continue
		}

		current := measuredLine{}

		for _, word := range words {
			encoded, err := encodeWinAnsi(word)
			if err != nil {
				return nil, err
			}

			wordWidth := measure(encoded, widths, size)

			if len(current.bytes) > 0 && current.width+spaceWidth+wordWidth > boxWidth {
				lines = append(lines, current)
				current = measuredLine{}
			}

			if len(current.bytes) > 0 {
				current.bytes = append(current.bytes, ' ')
				current.width += spaceWidth
				current.spaces++
			}

			current.bytes = append(current.bytes, encoded...)
			current.width += wordWidth
		}

		current.endsPar = true
		lines = append(lines, current)
	}

	return lines, nil
}

func measure(encoded []byte, widths *[byteValues]float64, size float64) float64 {
	total := 0.0
	for _, b := range encoded {
		total += widths[b]
	}

	return total * size / glyphUnitsPerEm
}

// textBlock is the geometry of the wrapped text before individual lines are placed.
type textBlock struct {
	left, top  float64 // Block box origin: left edge and top edge.
	width      float64 // Block box width.
	lineHeight float64
	firstBase  float64 // Baseline of the first line.
}

// layoutBlock computes where the block of lineCount lines sits on the page.
func layoutBlock(spec assembly.BlankSpec, lineCount int) textBlock {
	size := float64(spec.Text.Size)
	pageWidth, pageHeight := float64(spec.Dim.Width), float64(spec.Dim.Height)
	boxWidth := float64(spec.Text.Width)
	lineHeight := size * spec.Text.Leading
	blockHeight := lineHeight * float64(lineCount)

	left := float64(spec.Text.X)
	if spec.Text.Anchor.Horizontal() == assembly.PlaceCenter {
		left += (pageWidth - boxWidth) * half
	} else if spec.Text.Anchor.Horizontal() == assembly.PlaceRight {
		left += pageWidth - boxWidth
	}

	top := blockHeight + float64(spec.Text.Y)
	if spec.Text.Anchor.Vertical() == assembly.PlaceMiddle {
		top += (pageHeight - blockHeight) * half
	} else if spec.Text.Anchor.Vertical() == assembly.PlaceTop {
		top += pageHeight - blockHeight
	}

	metrics := fontVerticalMetrics(spec.Text.Font)
	glyphHeight := (metrics.ascent + metrics.descent) * size / glyphUnitsPerEm
	firstBase := top - (lineHeight-glyphHeight)*half - metrics.ascent*size/glyphUnitsPerEm

	return textBlock{left: left, top: top, width: boxWidth, lineHeight: lineHeight, firstBase: firstBase}
}

// placeLine positions one measured line within the block according to the text alignment.
func placeLine(block textBlock, align assembly.TextAlign, index int, line measuredLine) textLine {
	placed := textLine{
		bytes:    line.bytes,
		x:        block.left,
		baseline: block.firstBase - float64(index)*block.lineHeight,
	}
	slack := block.width - line.width

	switch {
	case align == assembly.AlignCenter:
		placed.x += slack * half
	case align == assembly.AlignRight:
		placed.x += slack
	case align == assembly.AlignJustify && !line.endsPar && line.spaces > 0 && slack > 0:
		placed.wordSpacing = slack / float64(line.spaces)
	default:
	}

	return placed
}

// placeText wraps and positions the text of spec on the page.
func placeText(ctx context.Context, spec assembly.BlankSpec) ([]textLine, error) {
	if spec.Text.Value == "" {
		return nil, nil
	}

	widths, err := glyphWidths(ctx, spec.Text.Font)
	if err != nil {
		return nil, err
	}

	measured, err := wrapText(spec.Text.Value, &widths, float64(spec.Text.Size), float64(spec.Text.Width))
	if err != nil {
		return nil, err
	}

	block := layoutBlock(spec, len(measured))
	lines := make([]textLine, 0, len(measured))

	for index := range measured {
		lines = append(lines, placeLine(block, spec.Text.Align, index, measured[index]))
	}

	return lines, nil
}
