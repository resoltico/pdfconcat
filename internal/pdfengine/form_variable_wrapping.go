// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
)

type variableLineBreak struct {
	end, next int
	width     float64
}

const (
	variableTextInset        = 4
	maximumMultilineAutoSize = 20
)

func wrapVariableLines(
	ctx context.Context,
	font *variableFont,
	paragraphs []variableAppearanceLine,
	size, available float64,
) ([]variableAppearanceLine, error) {
	var lines []variableAppearanceLine

	for _, paragraph := range paragraphs {
		wrapped, err := wrapVariableParagraph(ctx, font, paragraph, size, available)
		if err != nil {
			return nil, err
		}

		lines = append(lines, wrapped...)
	}

	return lines, nil
}

func wrapVariableParagraph(
	ctx context.Context,
	font *variableFont,
	paragraph variableAppearanceLine,
	size, available float64,
) ([]variableAppearanceLine, error) {
	if len(paragraph.data) == 0 {
		return []variableAppearanceLine{paragraph}, nil
	}

	var (
		lines []variableAppearanceLine
		start int
	)

	// Each line consumes at least one byte, including an oversized first glyph.
	for range paragraph.data {
		split, err := breakVariableLine(ctx, font, paragraph.data, start, size, available)
		if err != nil {
			return nil, err
		}

		lines = append(lines, variableAppearanceLine{data: paragraph.data[start:split.end], width: split.width})
		start = split.next
		if start >= len(paragraph.data) {
			return lines, nil
		}
	}

	return lines, nil
}

func breakVariableLine(
	ctx context.Context,
	font *variableFont,
	data []byte,
	start int,
	size, available float64,
) (variableLineBreak, error) {
	if err := ctx.Err(); err != nil {
		return variableLineBreak{}, fmt.Errorf("wrap variable text: %w", err)
	}

	// Every line consumes its first glyph, even when that glyph alone exceeds
	// the available width. The caller supplies a nonempty remaining suffix.
	code := data[start]
	split := variableLineBreak{end: start + 1, width: font.widths[code]}
	wordEnd, wordWidth := -1, 0.0
	space, hasSpace := font.codes[' ']
	if hasSpace && code == space {
		wordEnd = start
	}

	for offset, code := range data[start+1:] {
		if err := ctx.Err(); err != nil {
			return split, fmt.Errorf("wrap variable text: %w", err)
		}

		position := start + 1 + offset

		advance := font.widths[code]
		if (split.width+advance)*size > available {
			break
		}

		if hasSpace && code == space {
			wordEnd, wordWidth = position, split.width
		}

		split.width += advance
		split.end = position + 1
	}

	split.next = split.end
	if split.end < len(data) && wordEnd >= start {
		split.end, split.width, split.next = wordEnd, wordWidth, wordEnd+1
		for split.next < len(data) && hasSpace && data[split.next] == space {
			split.next++
		}
	}

	return split, nil
}

func (p *variableAppearancePlan) wrapMultiline(ctx context.Context, font *variableFont) error {
	width, height := p.logicalSize()
	available := width - 2*p.border - variableTextInset

	paragraphs := p.lines
	if p.fontSize == 0 {
		// Enumerate descending integer candidates; the minimum fallback follows below.
		for reduction := range maximumMultilineAutoSize - 1 {
			size := float64(maximumMultilineAutoSize - reduction)
			lines, err := wrapVariableLines(ctx, font, paragraphs, size, available)
			if err != nil {
				return err
			}

			p.lines, p.fontSize = lines, size
			if height-3-size*float64(len(lines)) >= .33*size {
				return nil
			}
		}

		p.fontSize = 1
	}

	lines, err := wrapVariableLines(ctx, font, paragraphs, p.fontSize, available)
	p.lines = lines

	return err
}
