// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	variableMultilineFlag   = 4096
	variablePasswordFlag    = 8192
	variableComboFlag       = 131072
	variableEditFlag        = 262144
	variableMultiSelectFlag = 2097152
	variableCombFlag        = 16777216
	keyMaxLen               = "MaxLen"
)

// compileVariableAppearance captures source semantics without changing its private graph.
// The resulting normal appearance has independent resources; alternate modes remain source-owned.
func compileVariableAppearance(
	ctx context.Context,
	pdf *model.Context,
	dict types.Dict,
	defaults formDefaults,
	resources map[string]types.Dict,
) (*variableAppearancePlan, error) {
	plan := &variableAppearancePlan{justification: defaults.justification, kind: "text"}
	if err := plan.readVariableProperties(ctx, pdf, dict); err != nil {
		return nil, err
	}

	appearance := defaults.appearance
	if appearance == nil || !appearance.hasFont {
		return nil, fmt.Errorf("%w: variable regeneration needs an effective font", errFormState)
	}

	font, err := compileVariableFont(ctx, pdf, resources[keyFont][appearance.fontName])
	if err != nil {
		return nil, err
	}

	if contentErr := plan.readVariableContent(ctx, pdf, dict, defaults, font, resources); contentErr != nil {
		return nil, contentErr
	}

	return plan, nil
}

func inheritedVariableInteger(ctx context.Context, pdf *model.Context, dict types.Dict, key string, fallback int) (int, error) {
	property, err := buttonInherited(ctx, pdf, dict, key)
	if err != nil {
		return 0, err
	}

	if property.value == nil {
		return fallback, nil
	}

	integer, ok := property.value.(types.Integer)
	if !ok || integer < 0 {
		return 0, fmt.Errorf("%w: variable field /%s must be a nonnegative integer", errFormState, key)
	}

	return integer.Value(), nil
}

func inheritedVariableStrings(ctx context.Context, pdf *model.Context, dict types.Dict, key string) ([]string, error) {
	property, err := buttonInherited(ctx, pdf, dict, key)
	if err != nil {
		return nil, err
	}

	if property.value == nil {
		return nil, nil
	}

	objects := types.Array{property.value}
	if array, ok := property.value.(types.Array); ok {
		objects = array
	}

	values := make([]string, 0, len(objects))
	for _, object := range objects {
		value, readErr := pdf.Dereference(object)
		if readErr != nil {
			return nil, fmt.Errorf("%w: variable /%s: %w", errFormState, key, readErr)
		}

		text, readErr := model.Text(value)
		if readErr != nil {
			return nil, fmt.Errorf("%w: variable /%s: %w", errFormState, key, readErr)
		}

		values = append(values, text)
	}

	return values, nil
}

func (p *variableAppearancePlan) readTextValue(ctx context.Context, pdf *model.Context, dict types.Dict, font *variableFont) error {
	values, err := inheritedVariableStrings(ctx, pdf, dict, "V")
	if err != nil {
		return err
	}

	if len(values) > 1 {
		return fmt.Errorf("%w: text regeneration needs a scalar value", errFormState)
	}

	value := ""
	if len(values) == 1 {
		value = values[0]
	}

	if p.flags&variablePasswordFlag != 0 {
		value = strings.Repeat("*", utf8.RuneCountInString(value))
	}

	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	if p.flags&variableMultilineFlag == 0 {
		value = strings.ReplaceAll(value, "\n", " ")
	}

	for part := range strings.SplitSeq(value, "\n") {
		line, encodeErr := font.encode(ctx, part)
		if encodeErr != nil {
			return encodeErr
		}

		p.lines = append(p.lines, line)
	}

	if p.flags&variableCombFlag != 0 {
		return p.readCombGlyphs(ctx, font, value)
	}

	return nil
}

func (p *variableAppearancePlan) sizeVariableText(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("size variable text: %w", err)
	}

	width, height := p.width, p.height
	if p.rotation == 90 || p.rotation == 270 {
		width, height = height, width
	}

	if p.fontSize == 0 {
		longest := 0.0
		for _, line := range p.lines {
			longest = max(longest, line.width)
		}

		p.fontSize = math.Floor(height - 2*p.border)
		if longest > 0 {
			p.fontSize = min(p.fontSize, math.Floor((width-4-2*p.border)/longest))
		}

		if p.flags&variableCombFlag != 0 {
			p.fontSize = math.Floor(min(height-2*p.border, (width-2*p.border)/float64(p.maxLen)))
		}
	}

	if p.fontSize <= 0 || !finiteAppearanceNumber(p.fontSize) {
		return fmt.Errorf("%w: variable field has no finite positive drawing size", errFormState)
	}

	for _, line := range p.lines {
		if !finiteAppearanceNumber(line.width * p.fontSize) {
			return fmt.Errorf("%w: variable text advance exceeds finite drawing geometry", errFormState)
		}
	}

	return nil
}

func (p *variableAppearancePlan) readCombGlyphs(ctx context.Context, font *variableFont, value string) error {
	if p.maxLen <= 0 || p.flags&variableMultilineFlag != 0 {
		return fmt.Errorf("%w: comb regeneration needs positive MaxLen and single-line content", errFormState)
	}

	for _, character := range value {
		glyph, encodeErr := font.encode(ctx, string(character))
		if encodeErr != nil {
			return encodeErr
		}

		p.glyphs = append(p.glyphs, glyph)
	}

	return nil
}

func (p *variableAppearancePlan) readVariableProperties(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	if err := p.readVariableGeometry(pdf, dict); err != nil {
		return err
	}

	if err := p.readVariableCharacteristics(pdf, dict); err != nil {
		return err
	}

	if err := p.readVariableBorder(pdf, dict); err != nil {
		return err
	}

	flags, err := inheritedVariableInteger(ctx, pdf, dict, "Ff", 0)
	if err != nil {
		return err
	}

	p.flags = flags

	p.maxLen, err = inheritedVariableInteger(ctx, pdf, dict, keyMaxLen, 0)
	if err != nil {
		return err
	}

	return nil
}

func (p *variableAppearancePlan) readVariableContent(
	ctx context.Context,
	pdf *model.Context,
	dict types.Dict,
	defaults formDefaults,
	font *variableFont,
	resources map[string]types.Dict,
) error {
	appearance := defaults.appearance

	var err error

	p.resources = types.Dict{}
	for category, bindings := range resources {
		p.resources[category] = bindings.Clone()
	}

	p.fontSize = appearance.fontSize
	if p.fontSize < 0 {
		return fmt.Errorf("%w: negative variable regeneration font size is unsupported", errFormState)
	}

	if defaults.kind == "Ch" {
		err = p.readChoiceValue(ctx, pdf, dict, font)
	} else {
		err = p.readTextValue(ctx, pdf, dict, font)
	}

	if err != nil {
		return err
	}

	if p.kind == "text" && p.flags&variableMultilineFlag != 0 {
		if wrapErr := p.wrapMultiline(ctx, font); wrapErr != nil {
			return wrapErr
		}
	}

	if sizeErr := p.sizeVariableText(ctx); sizeErr != nil {
		return sizeErr
	}

	p.da = appearance.sized(p.fontSize)

	return p.validateDrawing()
}
