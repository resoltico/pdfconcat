// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	variableFullTurn = 360
	keyRect          = "Rect"
)

func (p *variableAppearancePlan) readVariableGeometry(pdf *model.Context, dict types.Dict) error {
	rect, err := pdf.DereferenceArray(dict[keyRect])
	if err != nil || len(rect) != 4 {
		return fmt.Errorf("%w: variable regeneration needs a four-number rectangle", errFormState)
	}

	values := [4]float64{}

	for index, object := range rect {
		value, readErr := pdf.DereferenceNumber(object)
		if readErr != nil || !finiteAppearanceNumber(value) {
			return fmt.Errorf("%w: variable rectangle must be finite", errFormState)
		}

		values[index] = value
	}

	p.width, p.height = values[2]-values[0], values[3]-values[1]
	if !finiteAppearanceNumber(p.width) || !finiteAppearanceNumber(p.height) || p.width <= 0 || p.height <= 0 {
		return fmt.Errorf("%w: variable rectangle must have finite positive dimensions", errFormState)
	}

	return nil
}

func (p *variableAppearancePlan) readVariableCharacteristics(pdf *model.Context, dict types.Dict) error {
	characteristics, err := pdf.DereferenceDict(dict["MK"])
	if err != nil {
		return fmt.Errorf("%w: variable characteristics: %w", errFormState, err)
	}

	if rotation, found := characteristics.Find("R"); found {
		value, readErr := pdf.DereferenceInteger(rotation)
		if readErr != nil || value == nil || value.Value()%variableQuarterTurn != 0 {
			return fmt.Errorf("%w: variable rotation must be a quarter-turn integer", errFormState)
		}

		p.rotation = (value.Value()%variableFullTurn + variableFullTurn) % variableFullTurn
	}

	p.background, err = readVariableColor(pdf, characteristics["BG"])
	if err != nil {
		return err
	}

	p.borderColor, err = readVariableColor(pdf, characteristics["BC"])
	if err != nil {
		return err
	}

	return nil
}

func readVariableColor(pdf *model.Context, object types.Object) ([]float64, error) {
	array, err := pdf.DereferenceArray(object)
	if err != nil {
		return nil, fmt.Errorf("%w: variable color: %w", errFormState, err)
	}

	if len(array) == 0 {
		return nil, nil
	}

	if len(array) != 1 && len(array) != 3 && len(array) != 4 {
		return nil, fmt.Errorf("%w: variable color must be gray, RGB or CMYK", errFormState)
	}

	color := make([]float64, len(array))
	for index, object := range array {
		value, readErr := pdf.DereferenceNumber(object)
		if readErr != nil || !finiteAppearanceNumber(value) || value < 0 || value > 1 {
			return nil, fmt.Errorf("%w: variable color components must be finite in 0..1", errFormState)
		}

		color[index] = value
	}

	return color, nil
}

func (p *variableAppearancePlan) readVariableBorder(pdf *model.Context, dict types.Dict) error {
	p.borderStyle = "S"

	var err error
	if object, found := dict.Find("BS"); found {
		err = p.readVariableBorderStyle(pdf, object)
	} else {
		err = p.readVariableBorderArray(pdf, dict["Border"])
	}

	if err != nil {
		return err
	}

	if p.border < 0 || !finiteAppearanceNumber(p.border) {
		return fmt.Errorf("%w: variable border width must be finite and nonnegative", errFormState)
	}

	switch p.borderStyle {
	case "S", "D", "U", "B", "I":
		return nil
	default:
		return fmt.Errorf("%w: unsupported variable border style %q", errFormState, p.borderStyle)
	}
}

func (p *variableAppearancePlan) readVariableBorderStyle(pdf *model.Context, object types.Object) error {
	border, err := pdf.DereferenceDict(object)
	if err != nil {
		return fmt.Errorf("%w: variable BS: %w", errFormState, err)
	}

	if border == nil {
		return nil
	}

	p.border = 1
	if width, found := border.Find("W"); found {
		p.border, err = pdf.DereferenceNumber(width)
		if err != nil {
			return fmt.Errorf("%w: variable border width: %w", errFormState, err)
		}
	}

	style, _, err := pdf.DereferenceNameEntry(border, "S")
	if err != nil {
		return fmt.Errorf("%w: variable border style: %w", errFormState, err)
	}

	if style != nil {
		p.borderStyle = style.Value()
	}

	if p.borderStyle == "D" {
		p.dash, err = readVariableDash(pdf, border["D"])
	}

	return err
}

func (p *variableAppearancePlan) readVariableBorderArray(pdf *model.Context, object types.Object) error {
	border, err := pdf.DereferenceArray(object)
	if err != nil {
		return fmt.Errorf("%w: variable border: %w", errFormState, err)
	}

	if len(border) == 0 {
		return nil
	}

	if len(border) < variableRGBComponents || len(border) > variableCMYKComponents {
		return fmt.Errorf("%w: variable Border needs three or four entries", errFormState)
	}

	p.border, err = pdf.DereferenceNumber(border[2])
	if err != nil {
		return fmt.Errorf("%w: variable Border width: %w", errFormState, err)
	}

	if len(border) == variableCMYKComponents {
		p.dash, err = readVariableDash(pdf, border[3])
		p.borderStyle = "D"
	}

	return err
}

func readVariableDash(pdf *model.Context, object types.Object) ([]float64, error) {
	array, err := pdf.DereferenceArray(object)
	if err != nil {
		return nil, fmt.Errorf("%w: variable dash: %w", errFormState, err)
	}

	if len(array) == 0 {
		return []float64{3}, nil
	}

	values := make([]float64, len(array))
	positive := false

	for index, object := range array {
		value, readErr := pdf.DereferenceNumber(object)
		if readErr != nil || !finiteAppearanceNumber(value) || value < 0 {
			return nil, fmt.Errorf("%w: variable dash must have finite nonnegative lengths", errFormState)
		}

		values[index] = value
		positive = positive || value > 0
	}

	if !positive {
		return nil, fmt.Errorf("%w: variable dash cannot have all zero lengths", errFormState)
	}

	return values, nil
}
