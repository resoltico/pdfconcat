// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	variableAppearanceLine struct {
		data  []byte
		width float64 // Encoded glyph advance at font size one, before DA spacing and scaling.
	}
	variableAppearancePlan struct {
		resources                                        types.Dict
		da, kind, borderStyle                            string
		background, borderColor, dash                    []float64
		lines, glyphs                                    []variableAppearanceLine
		selected                                         []bool
		width, height, border, fontSize                  float64
		rotation, flags, justification, maxLen, topIndex int
	}
)

const (
	variableQuarterTurn       = 90
	variableHalfTurn          = 180
	variableThreeQuarterTurn  = 270
	variableRGBComponents     = 3
	variableCMYKComponents    = 4
	variableMultilineTopInset = 3
	variableListLeading       = 1.1
	variableReliefBlend       = .5
	variableFillColor         = "fill"
	variableAppearanceFailure = "variable appearance: %w"
	formXObjectSubtype        = "Form"
)

func (p *variableAppearancePlan) stream(ctx context.Context, pdf *model.Context) (*types.IndirectRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf(variableAppearanceFailure, err)
	}

	if err := p.validateDrawing(); err != nil {
		return nil, err
	}

	var content strings.Builder
	content.WriteString("q\n")
	p.drawBackground(&content)
	p.drawBorder(&content)
	content.WriteString("q\n")
	p.rotate(&content)

	switch {
	case p.kind == "list":
		p.drawList(&content)
	case p.flags&variableCombFlag != 0 && p.maxLen > 0:
		p.drawComb(&content)
	default:
		p.drawText(&content)
	}

	content.WriteString("Q\n")
	content.WriteString("Q\n")

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf(variableAppearanceFailure, err)
	}

	stream, err := pdf.NewStreamDictForBuf([]byte(content.String()))
	if err != nil {
		return nil, fmt.Errorf("variable appearance stream: %w", err)
	}

	stream.Dict[keyType] = types.Name(keyXObject)
	stream.Dict[keySubtype] = types.Name(formXObjectSubtype)
	stream.Dict["FormType"] = types.Integer(1)
	stream.Dict["BBox"] = types.NewNumberArray(0, 0, p.width, p.height)

	stream.Dict[keyResources] = p.resources
	if err = stream.Encode(); err != nil {
		return nil, fmt.Errorf("encode variable appearance: %w", err)
	}

	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf(variableAppearanceFailure, err)
	}

	ref, err := pdf.IndRefForNewObject(*stream)
	if err != nil {
		return nil, fmt.Errorf("store variable appearance: %w", err)
	}

	return ref, nil
}

func (p *variableAppearancePlan) logicalSize() (float64, float64) {
	if p.rotation == variableQuarterTurn || p.rotation == variableThreeQuarterTurn {
		return p.height, p.width
	}

	return p.width, p.height
}

func (p *variableAppearancePlan) rotate(out *strings.Builder) {
	switch p.rotation {
	case variableQuarterTurn:
		fmt.Fprintf(out, "0 1 -1 0 %s 0 cm\n", appearancePDFNumber(p.width))
	case variableHalfTurn:
		fmt.Fprintf(out, "-1 0 0 -1 %s %s cm\n", appearancePDFNumber(p.width), appearancePDFNumber(p.height))
	case variableThreeQuarterTurn:
		fmt.Fprintf(out, "0 -1 1 0 0 %s cm\n", appearancePDFNumber(p.height))
	default:
	}
}

func (p *variableAppearancePlan) drawBackground(out *strings.Builder) {
	if len(p.background) == 0 {
		return
	}

	variableDrawColor(out, p.background, variableFillColor)
	fmt.Fprintf(out, "0 0 %s %s re f\n", appearancePDFNumber(p.width), appearancePDFNumber(p.height))
}

func (p *variableAppearancePlan) drawBorder(out *strings.Builder) {
	color := p.borderColor
	if len(color) == 0 {
		color = p.background
	}

	if p.border <= 0 || len(color) == 0 {
		return
	}

	if p.borderStyle != "B" && p.borderStyle != "I" {
		variableDrawColor(out, color, "stroke")
		fmt.Fprintf(out, "%s w\n", appearancePDFNumber(p.border))
	}

	if p.borderStyle == "D" {
		out.WriteString("[")

		for _, length := range p.dash {
			fmt.Fprintf(out, " %s", appearancePDFNumber(length))
		}

		out.WriteString("] 0 d\n")
	}

	switch p.borderStyle {
	case "B", "I":
		p.drawRelief(out, color)
	case "U":
		fmt.Fprintf(out, "0 0 m %s 0 l s\n", appearancePDFNumber(p.width))
	default:
		fmt.Fprintf(
			out,
			"%s %s %s %s re s\n",
			appearancePDFNumber(p.border/2),
			appearancePDFNumber(p.border/2),
			appearancePDFNumber(p.width-p.border),
			appearancePDFNumber(p.height-p.border),
		)
	}

	fmt.Fprintf(
		out,
		"%s %s %s %s re W n\n",
		appearancePDFNumber(p.border),
		appearancePDFNumber(p.border),
		appearancePDFNumber(p.width-2*p.border),
		appearancePDFNumber(p.height-2*p.border),
	)
}

func variableDrawColor(out *strings.Builder, color []float64, operation string) {
	for _, value := range color {
		fmt.Fprintf(out, "%s ", appearancePDFNumber(value))
	}

	operator := "g"

	switch len(color) {
	case variableRGBComponents:
		operator = "rg"
	case variableCMYKComponents:
		operator = "k"
	default:
	}

	if operation == "stroke" {
		operator = strings.ToUpper(operator)
	}

	out.WriteString(operator + "\n")
}

func (p *variableAppearancePlan) textX(width, advance float64) float64 {
	// Source regeneration places text using base font advances; DA state still controls painting.
	switch p.justification {
	case 1:
		return (width - advance) / 2
	case 2:
		return width - p.border - 2 - advance
	default:
		return p.border + 2
	}
}

func (p *variableAppearancePlan) drawText(out *strings.Builder) {
	width, height := p.logicalSize()
	if p.flags&variableMultilineFlag != 0 {
		p.drawMultiline(out, width, height)
		return
	}

	if len(p.lines) == 0 {
		return
	}

	line := p.lines[0]
	out.WriteString("/Tx BMC\nBT\n" + p.da + "\n")
	fmt.Fprintf(
		out,
		"1 0 0 1 %.2f %.2f Tm\n<%s> Tj\nET\nEMC\n",
		p.textX(width, line.width*p.fontSize),
		height/2-.4*p.fontSize,
		hex.EncodeToString(line.data),
	)
}

func (p *variableAppearancePlan) drawMultiline(out *strings.Builder, width, height float64) {
	out.WriteString("/Tx BMC\nBT\n" + p.da + "\n")
	fmt.Fprintf(out, "1 0 0 1 0 %.2f Tm\n", height-variableMultilineTopInset)

	previousX := 0.

	for _, line := range p.lines {
		x := p.textX(width, line.width*p.fontSize)
		fmt.Fprintf(out, "%.2f %.2f Td\n<%s> Tj\n", x-previousX, -p.fontSize, hex.EncodeToString(line.data))
		previousX = x
	}

	out.WriteString("ET\nEMC\n")
}

func (p *variableAppearancePlan) drawComb(out *strings.Builder) {
	width, height := p.logicalSize()
	count := min(len(p.glyphs), p.maxLen)
	cell := (width - 2*p.border) / float64(p.maxLen)

	start := p.border
	switch p.justification {
	case 1:
		start += float64(p.maxLen-count) / 2 * cell
	case 2:
		start += float64(p.maxLen-count) * cell
	default:
	}

	out.WriteString("/Tx BMC\nBT\n" + p.da + "\n")
	fmt.Fprintf(out, "1 0 0 1 %.2f %.2f Tm\n", start, height/2-.4*p.fontSize)

	previous := cell
	for _, glyph := range p.glyphs[:count] {
		center := (cell - glyph.width*p.fontSize) / 2
		fmt.Fprintf(out, "%.2f 0 Td\n<%s> Tj\n", center-previous+cell, hex.EncodeToString(glyph.data))
		previous = center
	}

	out.WriteString("ET\nEMC\n")
}

func (p *variableAppearancePlan) drawList(out *strings.Builder) {
	width, height := p.logicalSize()

	baseline := height - variableListLeading*p.fontSize
	for index := p.topIndex; index < len(p.lines); index++ {
		line := p.lines[index]

		out.WriteString("q\n")

		selected := index < len(p.selected) && p.selected[index]
		if selected {
			fmt.Fprintf(
				out,
				"0 g\n%s %.2f %s %.2f re f\n",
				appearancePDFNumber(p.border),
				baseline-.2*p.fontSize,
				appearancePDFNumber(width-2*p.border),
				variableListLeading*p.fontSize,
			)
		}

		out.WriteString("BT\n" + p.da + "\n")
		fmt.Fprintf(out, "1 0 0 1 %.2f %.2f Tm\n", p.textX(width, line.width*p.fontSize), baseline)

		if selected {
			out.WriteString("1 g\n")
		}

		fmt.Fprintf(out, "<%s> Tj\nET\nQ\n", hex.EncodeToString(line.data))

		baseline -= variableListLeading * p.fontSize
	}
}

func (p *variableAppearancePlan) drawRelief(out *strings.Builder, color []float64) {
	light, dark := make([]float64, len(color)), make([]float64, len(color))
	for index, value := range color {
		light[index] = variableReliefBlend*value + variableReliefBlend
		dark[index] = variableReliefBlend * value
	}

	if p.borderStyle == "I" {
		light, dark = dark, light
	}

	if len(color) == variableCMYKComponents {
		light, dark = dark, light
	}

	variableDrawColor(out, light, variableFillColor)

	w, h, b := appearancePDFNumber(p.width), appearancePDFNumber(p.height), appearancePDFNumber(p.border)
	innerW, innerH := appearancePDFNumber(p.width-p.border), appearancePDFNumber(p.height-p.border)
	fmt.Fprintf(out, "0 0 m 0 %s l %s %s l %s %s l %s %s l %s %s l f\n", h, w, h, innerW, innerH, b, innerH, b, b)
	variableDrawColor(out, dark, variableFillColor)
	fmt.Fprintf(out, "0 0 m %s 0 l %s %s l %s %s l %s %s l %s %s l f\n", w, w, h, innerW, innerH, innerW, b, b, b)
}

func (p *variableAppearancePlan) validateDrawing() error {
	width, height := p.logicalSize()
	for _, value := range []float64{
		width, height, p.fontSize,
		width - 2*p.border, height - 2*p.border,
		height - variableListLeading*p.fontSize*float64(len(p.lines)),
		height - variableMultilineTopInset - p.fontSize*float64(len(p.lines)),
		height/2 - .4*p.fontSize,
	} {
		if !finiteAppearanceNumber(value) {
			return fmt.Errorf("%w: variable appearance geometry must be finite", errFormState)
		}
	}

	for _, line := range p.lines {
		advance := line.width * p.fontSize
		if !finiteAppearanceNumber(advance) || !finiteAppearanceNumber(p.textX(width, advance)) {
			return fmt.Errorf("%w: variable appearance text position exceeds finite geometry", errFormState)
		}
	}

	return p.validateCombPosition(width)
}

func (p *variableAppearancePlan) validateCombPosition(width float64) error {
	if p.maxLen > 0 {
		cell := (width - 2*p.border) / float64(p.maxLen)
		for _, glyph := range p.glyphs {
			if !finiteAppearanceNumber((cell - glyph.width*p.fontSize) / 2) {
				return fmt.Errorf("%w: variable appearance comb position must be finite", errFormState)
			}
		}
	}

	return nil
}
