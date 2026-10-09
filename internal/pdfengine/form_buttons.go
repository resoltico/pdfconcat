// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	pdffont "github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	buttonAppearancePlan struct {
		on                                            types.Name
		background                                    [3]float64
		borderColor                                   [3]float64
		width, height, border, fontSize, textX, textY float64
		caption                                       byte
	}

	buttonProperty struct{ value types.Object }
)

const (
	buttonFontUnits       = 1000
	buttonCoordinateScale = 100
	checkboxRadioFlag     = 32768
	buttonPushFlag        = 65536
	buttonOffState        = "Off"
)

// compileButtonAppearance captures the supported source-driven button regeneration contract.
// Only the proven solid RGB, unrotated square checkbox/radio dingbat shapes are materialized.
func compileButtonAppearance(ctx context.Context, pdf *model.Context, dict types.Dict) (*buttonAppearancePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("button appearance: %w", err)
	}

	plan := &buttonAppearancePlan{}
	if err := plan.readGeometry(ctx, pdf, dict); err != nil {
		return nil, err
	}

	if err := plan.readCharacteristics(ctx, pdf, dict); err != nil {
		return nil, err
	}

	if err := plan.readState(ctx, pdf, dict); err != nil {
		return nil, err
	}

	width, err := pdffont.CharWidth(ctx, "ZapfDingbats", rune(plan.caption))
	if err != nil {
		return nil, fmt.Errorf("button caption metrics: %w", err)
	}

	advance := float64(width) / buttonFontUnits

	plan.fontSize = math.Floor(min(plan.height-2*plan.border, (plan.width-4-2*plan.border)/advance))
	if plan.fontSize <= 0 {
		return nil, fmt.Errorf("%w: regenerated button rectangle is too small for its border and caption", errFormState)
	}

	plan.textX = math.Round((plan.width-advance*plan.fontSize)/2*buttonCoordinateScale) / buttonCoordinateScale

	plan.textY = math.Round((plan.height/2-.4*plan.fontSize)*buttonCoordinateScale) / buttonCoordinateScale
	if !finiteAppearanceNumber(plan.fontSize) || !finiteAppearanceNumber(plan.textX) || !finiteAppearanceNumber(plan.textY) {
		return nil, fmt.Errorf("%w: regenerated button geometry exceeds finite drawing coordinates", errFormState)
	}

	return plan, nil
}

func (p *buttonAppearancePlan) readGeometry(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	rect, err := pdf.DereferenceArrayContext(ctx, dict["Rect"])
	if err != nil || len(rect) != 4 {
		return errors.Join(err, fmt.Errorf("%w: regenerated button needs a four-number rectangle", errFormState))
	}

	values := make([]float64, len(rect))
	for index, object := range rect {
		value, readErr := pdf.DereferenceNumberContext(ctx, object)
		if readErr != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.Join(readErr, fmt.Errorf("%w: regenerated button rectangle must be finite", errFormState))
		}

		values[index] = value
	}

	p.width, p.height = values[2]-values[0], values[3]-values[1]
	if !finiteAppearanceNumber(p.width) || !finiteAppearanceNumber(p.height) || p.width <= 0 || p.width != p.height {
		return fmt.Errorf("%w: regenerated buttons require a positive square rectangle", errFormState)
	}

	return p.readBorder(ctx, pdf, dict)
}

func (p *buttonAppearancePlan) readBorder(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	border, err := pdf.DereferenceDictContext(ctx, dict["BS"])
	if err != nil {
		return fmt.Errorf("%w: button border: %w", errFormState, err)
	}

	style, _, err := pdf.DereferenceNameEntryContext(ctx, border, "S")
	if err != nil || style == nil || *style != "S" {
		return errors.Join(err, fmt.Errorf("%w: regenerated buttons require an explicit solid border", errFormState))
	}

	p.border, err = pdf.DereferenceNumberContext(ctx, border["W"])
	if err != nil || p.border <= 0 || math.IsNaN(p.border) || math.IsInf(p.border, 0) {
		return errors.Join(err, fmt.Errorf("%w: regenerated button border must have finite positive width", errFormState))
	}

	return nil
}

func (p *buttonAppearancePlan) readCharacteristics(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	characteristics, err := pdf.DereferenceDictContext(ctx, dict["MK"])
	if err != nil {
		return fmt.Errorf("%w: button characteristics: %w", errFormState, err)
	}

	if rotation, found := characteristics.Find("R"); found {
		value, readErr := pdf.DereferenceNumberContext(ctx, rotation)
		if readErr != nil || value != 0 {
			return errors.Join(readErr, fmt.Errorf("%w: rotated regenerated buttons are unsupported", errFormState))
		}
	}

	if colorErr := p.readColor(ctx, pdf, characteristics["BG"], &p.background); colorErr != nil {
		return colorErr
	}

	if colorErr := p.readColor(ctx, pdf, characteristics["BC"], &p.borderColor); colorErr != nil {
		return colorErr
	}

	value, err := pdf.DereferenceContext(ctx, characteristics["CA"])
	if err != nil {
		return fmt.Errorf("%w: button caption: %w", errFormState, err)
	}

	caption, ok := value.(types.StringLiteral)
	if !ok || len(caption.Value()) != 1 {
		return fmt.Errorf("%w: regenerated button requires a supported single-byte caption", errFormState)
	}

	p.caption = caption.Value()[0]

	return nil
}

func (*buttonAppearancePlan) readColor(ctx context.Context, pdf *model.Context, object types.Object, color *[3]float64) error {
	components, err := pdf.DereferenceArrayContext(ctx, object)
	if err != nil || len(components) != len(color) {
		return errors.Join(err, fmt.Errorf("%w: regenerated button requires explicit RGB background and border colors", errFormState))
	}

	for index, object := range components {
		value, readErr := pdf.DereferenceNumberContext(ctx, object)
		if readErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return errors.Join(readErr, fmt.Errorf("%w: regenerated button color components must be finite in 0..1", errFormState))
		}

		color[index] = value
	}

	return nil
}

func (p *buttonAppearancePlan) readState(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	property, err := buttonInherited(ctx, pdf, dict, "Ff")
	object := property.value

	if err != nil {
		return err
	}

	flags := 0

	if object != nil {
		integer, readErr := pdf.DereferenceIntegerContext(ctx, object)
		if readErr != nil || integer == nil {
			return errors.Join(readErr, fmt.Errorf("%w: button field flags are not an integer", errFormState))
		}

		flags = integer.Value()
	}

	if flags&buttonPushFlag != 0 {
		return fmt.Errorf("%w: regenerated push buttons are unsupported", errFormState)
	}

	want := byte('4')
	if flags&checkboxRadioFlag != 0 {
		want = 'l'
	}

	if p.caption != want {
		return fmt.Errorf("%w: regenerated checkbox/radio caption must be 4/l respectively", errFormState)
	}

	if appearanceErr := buttonDefaultAppearance(ctx, pdf, dict); appearanceErr != nil {
		return appearanceErr
	}

	if statesErr := p.readNormalStates(ctx, pdf, dict); statesErr != nil {
		return statesErr
	}

	return p.checkSelection(ctx, pdf, dict, flags)
}

func (p *buttonAppearancePlan) readNormalStates(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	appearance, err := pdf.DereferenceDictContext(ctx, dict["AP"])
	if err != nil {
		return fmt.Errorf("%w: button appearance: %w", errFormState, err)
	}

	states, err := pdf.DereferenceDictContext(ctx, appearance["N"])
	if err != nil || len(states) != 2 {
		return errors.Join(err, fmt.Errorf("%w: regenerated button needs normal Off and one named on-state streams", errFormState))
	}

	for name, object := range states {
		value, readErr := pdf.DereferenceContext(ctx, object)
		if readErr != nil {
			return fmt.Errorf("%w: button state stream: %w", errFormState, readErr)
		}

		if _, ok := value.(types.StreamDict); !ok {
			return fmt.Errorf("%w: regenerated button normal states must be real streams", errFormState)
		}

		if name != buttonOffState {
			p.on = types.Name(name)
		}
	}

	if _, found := states[buttonOffState]; !found || p.on == "" {
		return fmt.Errorf("%w: regenerated button needs Off and a named on-state", errFormState)
	}

	return nil
}

func (p *buttonAppearancePlan) checkSelection(ctx context.Context, pdf *model.Context, dict types.Dict, flags int) error {
	state, _, err := pdf.DereferenceNameEntryContext(ctx, dict, "AS")
	if err != nil || state == nil || *state != buttonOffState && *state != p.on {
		return errors.Join(err, fmt.Errorf("%w: regenerated button AS must select a valid normal state", errFormState))
	}

	property, valueErr := buttonInherited(ctx, pdf, dict, "V")
	valueObject := property.value

	if valueErr != nil {
		return valueErr
	}

	value, readErr := pdf.DereferenceContext(ctx, valueObject)

	selection, valid := value.(types.Name)
	if readErr != nil || !valid {
		return errors.Join(readErr, fmt.Errorf("%w: regenerated button V must be a valid name", errFormState))
	}

	selected := types.Name(buttonOffState)
	if selection == p.on {
		selected = p.on
	} else if flags&checkboxRadioFlag == 0 && selection != buttonOffState {
		return fmt.Errorf("%w: regenerated checkbox V must match its normal state names", errFormState)
	}

	if *state != selected {
		return fmt.Errorf("%w: regenerated button AS conflicts with its field V", errFormState)
	}

	return nil
}

func buttonInherited(ctx context.Context, pdf *model.Context, dict types.Dict, key string) (buttonProperty, error) {
	for depth := 0; dict != nil; depth++ {
		if err := ctx.Err(); err != nil {
			return buttonProperty{}, fmt.Errorf("button inheritance: %w", err)
		}

		if depth > maxFieldDepth {
			return buttonProperty{}, fmt.Errorf("%w: cyclic/deep button inheritance", errFormState)
		}

		if value, found := dict.Find(key); found {
			resolved, readErr := pdf.DereferenceContext(ctx, value)
			if readErr != nil {
				return buttonProperty{}, fmt.Errorf("%w: button inherited %s: %w", errFormState, key, readErr)
			}

			if resolved != nil {
				return buttonProperty{value: resolved}, nil
			}
		}

		parent, err := pdf.DereferenceDictContext(ctx, dict[keyParent])
		if err != nil {
			return buttonProperty{}, fmt.Errorf("%w: button parent: %w", errFormState, err)
		}

		dict = parent
	}

	return buttonProperty{}, nil
}

func buttonDefaultAppearance(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	property, err := buttonInherited(ctx, pdf, dict, "DA")
	object := property.value

	if err != nil {
		return err
	}

	if object == nil {
		root, readErr := pdf.DereferenceDictContext(ctx, pdf.RootDict[keyAcroForm])
		if readErr != nil {
			return fmt.Errorf("%w: button form defaults: %w", errFormState, readErr)
		}

		object = root["DA"]
	}

	value, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fmt.Errorf("%w: button default appearance: %w", errFormState, err)
	}

	if value == nil {
		return nil
	}

	text, ok := value.(types.StringLiteral)
	if !ok || strings.TrimSpace(text.Value()) != "" {
		return fmt.Errorf("%w: regenerated buttons with custom default appearance are unsupported", errFormState)
	}

	return nil
}

func finiteAppearanceNumber(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
