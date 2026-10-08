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

const variableTextInset = 4

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

	var lines []variableAppearanceLine

	for start := 0; start < len(paragraph.data); {
		split, err := breakVariableLine(ctx, font, paragraph.data, start, size, available)
		if err != nil {
			return nil, err
		}

		lines = append(lines, variableAppearanceLine{data: paragraph.data[start:split.end], width: split.width})
		start = split.next
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
	split := variableLineBreak{end: start}
	wordEnd, wordWidth := -1, 0.0
	space, hasSpace := font.codes[' ']

	for split.end < len(data) {
		if err := ctx.Err(); err != nil {
			return split, fmt.Errorf("wrap variable text: %w", err)
		}

		code := data[split.end]

		advance := font.widths[code]
		if split.end > start && (split.width+advance)*size > available {
			break
		}

		if hasSpace && code == space {
			wordEnd, wordWidth = split.end, split.width
		}

		split.width += advance
		split.end++
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
		for size := 20.0; size > 1; size-- {
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
