// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	appearanceOperand struct {
		object     types.Object
		start, end int
	}
	appearanceBinding struct {
		category, name string
		start, end     int
	}
	formAppearance struct {
		text               string
		fontName           string
		bindings           []appearanceBinding
		fontSize           float64
		sizeStart, sizeEnd int
		hasFont            bool
	}
)

const (
	keyFont             = "Font"
	keyExtGState        = "ExtGState"
	rgbColorComponents  = 3
	cmykColorComponents = 4
)

// parseFormAppearance uses pdfcpu's object parser for PDF operands; only operator boundaries
// are scanned here. Default appearance programs contain supported text and color state, not painting.
func parseFormAppearance(ctx context.Context, text string, resources map[string]types.Dict) (formAppearance, error) {
	parsed := formAppearance{text: text}

	var operands []appearanceOperand

	remaining := text
	for remaining != "" {
		if err := ctx.Err(); err != nil {
			return parsed, fmt.Errorf("default appearance cancellation: %w", err)
		}

		remaining = appearanceSpace(remaining)
		if remaining == "" {
			break
		}

		start := len(text) - len(remaining)
		if appearanceObject(remaining[0]) {
			operand, err := appearanceValue(ctx, &remaining, start)
			if err != nil {
				return parsed, fmt.Errorf("default appearance operand: %w", err)
			}

			operands = append(operands, operand)

			continue
		}

		operator := appearanceOperator(remaining)
		if operator == "" {
			return parsed, fmt.Errorf("%w: invalid default appearance delimiter at byte %d", errFormState, start)
		}

		remaining = remaining[len(operator):]
		if err := parsed.operation(operator, operands, resources); err != nil {
			return parsed, err
		}

		operands = operands[:0]
	}

	if len(operands) != 0 {
		return parsed, fmt.Errorf("%w: default appearance ends with unused operands", errFormState)
	}

	return parsed, nil
}

// PDF content operands cannot be indirect references. Isolate number tokens so the object parser
// cannot mistake the color operator RG for the R suffix of an indirect object reference.

func appearanceValue(ctx context.Context, remaining *string, start int) (appearanceOperand, error) {
	input := *remaining
	if strings.ContainsRune("+-.0123456789", rune(input[0])) {
		token := appearanceOperator(input)

		number, err := parseAppearanceNumber(token)
		if err != nil {
			return appearanceOperand{}, err
		}

		*remaining = (*remaining)[len(token):]

		return appearanceOperand{object: number.object, start: start, end: start + len(token)}, nil
	}

	length := len(input)

	object, err := model.ParseObject(ctx, &input, 0)
	if err != nil {
		return appearanceOperand{}, fmt.Errorf("default appearance operand: %w", err)
	}

	consumed := length - len(input)
	*remaining = (*remaining)[consumed:]

	return appearanceOperand{object: object, start: start, end: start + consumed}, nil
}

func parseAppearanceNumber(input string) (appearanceOperand, error) {
	if !validAppearanceNumber(input) {
		return appearanceOperand{}, fmt.Errorf("%w: invalid PDF number %q", errFormState, input)
	}

	if strings.Contains(input, ".") {
		number, err := strconv.ParseFloat(input, 64)
		if err != nil || !finiteAppearanceNumber(number) {
			return appearanceOperand{}, errors.Join(err, fmt.Errorf("%w: default appearance real must be finite", errFormState))
		}

		return appearanceOperand{object: types.Float(number)}, nil
	}

	number, err := strconv.Atoi(input)
	if err != nil {
		return appearanceOperand{}, fmt.Errorf("default appearance integer: %w", err)
	}

	return appearanceOperand{object: types.Integer(number)}, nil
}

func validAppearanceNumber(token string) bool {
	if token == "" {
		return false
	}

	if token[0] == '+' || token[0] == '-' {
		token = token[1:]
	}

	digits, dots := 0, 0

	for _, character := range token {
		if character == '.' {
			dots++
			continue
		}

		if character < '0' || character > '9' {
			return false
		}

		digits++
	}

	return digits > 0 && dots <= 1
}

func appearanceSpace(text string) string {
	for text != "" {
		if pdfSpace(text[0]) {
			text = text[1:]
			continue
		}

		if text[0] == '%' {
			index := strings.IndexAny(text, "\r\n")
			if index < 0 {
				return ""
			}

			text = text[index:]

			continue
		}

		break
	}

	return text
}

func pdfSpace(value byte) bool {
	return value == 0 || value == 9 || value == 10 || value == 12 || value == 13 || value == 32
}

func appearanceObject(value byte) bool {
	return strings.ContainsRune("/([<+-.0123456789", rune(value))
}

func appearanceOperator(text string) string {
	for index := range len(text) {
		if pdfSpace(text[index]) || strings.ContainsRune("()<>[]{}/%", rune(text[index])) {
			return text[:index]
		}
	}

	return text
}

func (a *formAppearance) operation(operator string, operands []appearanceOperand, resources map[string]types.Dict) error {
	switch operator {
	case "Tf":
		if len(operands) != 2 || !appearanceNumber(operands[1].object) {
			return fmt.Errorf("%w: default appearance Tf requires a font name and finite size", errFormState)
		}

		if err := a.bind(keyFont, operands[0], resources); err != nil {
			return err
		}

		a.hasFont = true
		a.fontName = a.bindings[len(a.bindings)-1].name

		switch size := operands[1].object.(type) {
		case types.Integer:
			a.fontSize = float64(size)
		case types.Float:
			a.fontSize = float64(size)
		default:
			// Numeric operands were validated above.
		}

		a.sizeStart, a.sizeEnd = operands[1].start, operands[1].end
	case "gs":
		if len(operands) != 1 {
			return fmt.Errorf("%w: default appearance gs requires one graphics-state name", errFormState)
		}

		return a.bind(keyExtGState, operands[0], resources)
	default:
		return checkAppearanceNumbers(operator, operands)
	}

	return nil
}

func checkAppearanceNumbers(operator string, operands []appearanceOperand) error {
	arity := appearanceArity(operator)
	if arity < 0 {
		return fmt.Errorf("%w: unsupported default appearance operator %q", errFormState, operator)
	}

	if len(operands) != arity {
		return fmt.Errorf("%w: default appearance %s requires %d numeric operands, got %d", errFormState, operator, arity, len(operands))
	}

	for _, operand := range operands {
		if !appearanceNumber(operand.object) {
			return fmt.Errorf("%w: default appearance %s requires finite numeric operands", errFormState, operator)
		}
	}

	return nil
}

func appearanceArity(operator string) int {
	switch operator {
	case "Tc", "Tw", "Tz", "TL", "Tr", "Ts", "g", "G":
		return 1
	case "rg", "RG":
		return rgbColorComponents
	case "k", "K":
		return cmykColorComponents
	default:
		return -1
	}
}

func appearanceNumber(object types.Object) bool {
	switch value := object.(type) {
	case types.Integer:
		return true
	case types.Float:
		return !math.IsNaN(value.Value()) && !math.IsInf(value.Value(), 0)
	default:
		return false
	}
}

func (a *formAppearance) bind(category string, operand appearanceOperand, resources map[string]types.Dict) error {
	name, ok := operand.object.(types.Name)
	if !ok {
		return fmt.Errorf("%w: default appearance %s resource operand must be a name", errFormState, category)
	}

	if _, found := resources[category][name.Value()]; !found {
		return fmt.Errorf("%w: default appearance refers to missing /DR /%s /%s", errFormState, category, name.Value())
	}

	a.bindings = append(a.bindings, appearanceBinding{category: category, name: name.Value(), start: operand.start, end: operand.end})

	return nil
}

func (a *formAppearance) renamed(names map[string]map[string]string) string {
	var result strings.Builder

	position := 0
	for _, binding := range a.bindings {
		result.WriteString(a.text[position:binding.start])
		result.WriteString(types.Name(names[binding.category][binding.name]).PDFString())
		position = binding.end
	}

	result.WriteString(a.text[position:])

	return result.String()
}

// sized replaces the effective Tf operand, preserving all other source DA operators and bytes.
func (a *formAppearance) sized(size float64) string {
	return a.text[:a.sizeStart] + strconv.FormatFloat(size, 'f', -1, 64) + a.text[a.sizeEnd:]
}
