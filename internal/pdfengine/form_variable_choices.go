// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type variableChoiceOption struct{ export, display string }

func (p *variableAppearancePlan) readChoiceValue(ctx context.Context, pdf *model.Context, dict types.Dict, font *variableFont) error {
	options, err := readVariableOptions(ctx, pdf, dict)
	if err != nil {
		return err
	}

	values, err := inheritedVariableStrings(ctx, pdf, dict, "V")
	if err != nil {
		return err
	}

	if p.flags&variableComboFlag != 0 {
		return p.readComboValue(ctx, font, options, values)
	}

	p.kind = "list"

	p.topIndex, err = inheritedVariableInteger(ctx, pdf, dict, "TI", 0)
	if err != nil {
		return err
	}

	indices, err := readVariableIndices(ctx, pdf, dict, len(options))
	if err != nil {
		return err
	}

	if indices == nil {
		indices = selectedVariableOptions(options, values)
	}

	p.selected = indices

	for _, option := range options {
		line, encodeErr := font.encode(ctx, option.display)
		if encodeErr != nil {
			return encodeErr
		}

		p.lines = append(p.lines, line)
	}

	if p.topIndex > len(options) {
		return fmt.Errorf("%w: choice top index exceeds options", errFormState)
	}

	return nil
}

func (p *variableAppearancePlan) readComboValue(
	ctx context.Context,
	font *variableFont,
	options []variableChoiceOption,
	values []string,
) error {
	if len(values) > 1 {
		return fmt.Errorf("%w: combo regeneration needs a scalar value", errFormState)
	}

	value := ""

	if len(values) == 1 {
		var found bool

		value, found = variableComboDisplay(options, values[0])
		if !found && value != "" && p.flags&variableEditFlag == 0 {
			return fmt.Errorf("%w: combo value does not match an option", errFormState)
		}
	}

	line, err := font.encode(ctx, value)
	p.lines = []variableAppearanceLine{line}

	return err
}

func variableComboDisplay(options []variableChoiceOption, value string) (string, bool) {
	for _, option := range options {
		if option.export == value {
			return option.display, true
		}
	}

	return value, false
}

func selectedVariableOptions(options []variableChoiceOption, values []string) []bool {
	selected := make([]bool, len(options))
	for _, value := range values {
		for index, option := range options {
			if option.export == value {
				selected[index] = true
				break
			}
		}
	}

	return selected
}

func readVariableOptions(ctx context.Context, pdf *model.Context, dict types.Dict) ([]variableChoiceOption, error) {
	property, err := buttonInherited(ctx, pdf, dict, "Opt")
	if err != nil {
		return nil, err
	}

	array, err := pdf.DereferenceArrayContext(ctx, property.value)
	if err != nil {
		return nil, fmt.Errorf("%w: choice options: %w", errFormState, err)
	}

	options := make([]variableChoiceOption, 0, len(array))
	for _, object := range array {
		if err = ctx.Err(); err != nil {
			return nil, fmt.Errorf("read choice options: %w", err)
		}

		option, readErr := readVariableOption(ctx, pdf, object)
		if readErr != nil {
			return nil, readErr
		}

		options = append(options, option)
	}

	return options, nil
}

func readVariableOption(ctx context.Context, pdf *model.Context, object types.Object) (variableChoiceOption, error) {
	option := variableChoiceOption{}

	value, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return option, fmt.Errorf("%w: choice option: %w", errFormState, err)
	}

	pair := types.Array{value, value}
	if decoded, ok := value.(types.Array); ok {
		pair = decoded
	}

	if len(pair) != 2 {
		return option, fmt.Errorf("%w: choice option must be a string or export/display pair", errFormState)
	}

	export, err := pdf.DereferenceContext(ctx, pair[0])
	if err != nil {
		return option, fmt.Errorf("%w: choice export: %w", errFormState, err)
	}

	display, err := pdf.DereferenceContext(ctx, pair[1])
	if err != nil {
		return option, fmt.Errorf("%w: choice display: %w", errFormState, err)
	}

	option.export, err = model.Text(export)
	if err != nil {
		return option, fmt.Errorf("%w: option export: %w", errFormState, err)
	}

	option.display, err = model.Text(display)
	if err != nil {
		return option, fmt.Errorf("%w: option display: %w", errFormState, err)
	}

	return option, nil
}

func readVariableIndices(ctx context.Context, pdf *model.Context, dict types.Dict, count int) ([]bool, error) {
	property, err := buttonInherited(ctx, pdf, dict, "I")
	if err != nil {
		return nil, err
	}

	if property.value == nil {
		return nil, nil
	}

	array, err := pdf.DereferenceArrayContext(ctx, property.value)
	if err != nil {
		return nil, fmt.Errorf("%w: choice indices: %w", errFormState, err)
	}

	indices := make([]bool, count)

	for _, object := range array {
		value, readErr := pdf.DereferenceIntegerContext(ctx, object)
		if readErr != nil || value == nil || value.Value() < 0 || value.Value() >= count {
			return nil, errors.Join(readErr, fmt.Errorf("%w: choice index is outside options", errFormState))
		}

		indices[value.Value()] = true
	}

	return indices, nil
}
