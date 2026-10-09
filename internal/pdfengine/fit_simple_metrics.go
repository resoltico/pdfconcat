// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/benoitkugler/pdf/fonts/standardfonts"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type fitSimpleMetrics struct {
	widths [256]float64
	known  bool
}

// simpleMetrics follows source byte codes and declared widths, never Unicode regeneration widths.
func (i *fitProgramInspector) simpleMetrics(ctx context.Context, dict types.Dict, kind string) (fitSimpleMetrics, error) {
	if kind != "Type1" && kind != "TrueType" {
		return fitSimpleMetrics{}, nil
	}

	if _, found := dict["Widths"]; found {
		return i.declaredSimpleMetrics(ctx, dict)
	}

	base, err := i.name(ctx, dict["BaseFont"])
	if err != nil {
		return fitSimpleMetrics{}, fitResourceError(err)
	}

	metrics, known := standardfonts.Fonts[base]
	if !known {
		return fitSimpleMetrics{}, nil
	}

	encoding, err := i.simpleEncoding(ctx, dict["Encoding"], (*simpleencodings.Encoding)(&metrics.Builtin))
	if err != nil {
		return fitSimpleMetrics{}, fitResourceError(err)
	}

	result := fitSimpleMetrics{known: true}
	for code, name := range encoding {
		result.widths[code] = float64(metrics.CharsWidths[name]) / fontMetricScale
	}

	return result, nil
}

func (i *fitProgramInspector) declaredSimpleMetrics(ctx context.Context, dict types.Dict) (fitSimpleMetrics, error) {
	var result fitSimpleMetrics

	first, err := i.integer(ctx, dict["FirstChar"])
	if err != nil {
		return result, fitResourceError(err)
	}

	values, err := i.pdf.DereferenceArrayContext(ctx, dict["Widths"])
	if err != nil {
		return result, fitResourceError(err)
	}

	if first < 0 || first > 255 || len(values) > 256-first {
		return result, fmt.Errorf("%w: source simple-font width range is invalid", errFitUnsupported)
	}

	missing, err := i.simpleMissingWidth(ctx, dict)
	if err != nil {
		return result, fitResourceError(err)
	}

	for code := range result.widths {
		result.widths[code] = missing / fontMetricScale
	}

	for index, object := range values {
		width, readErr := i.pdf.DereferenceNumberContext(ctx, object)
		if readErr != nil {
			return result, fitResourceError(readErr)
		}

		if !finiteAppearanceNumber(width) {
			return result, fmt.Errorf("%w: source simple-font width must be finite", errFitUnsupported)
		}

		result.widths[first+index] = width / fontMetricScale
	}

	result.known = true

	return result, nil
}

func (i *fitProgramInspector) simpleMissingWidth(ctx context.Context, dict types.Dict) (float64, error) {
	descriptor, err := i.pdf.DereferenceDictContext(ctx, dict["FontDescriptor"])
	if err != nil {
		return 0, fitResourceError(err)
	}

	object, found := descriptor["MissingWidth"]
	if !found {
		return 0, nil
	}

	width, err := i.pdf.DereferenceNumberContext(ctx, object)
	if err != nil {
		return 0, fitResourceError(err)
	}

	if !finiteAppearanceNumber(width) {
		return 0, fmt.Errorf("%w: source MissingWidth must be finite", errFitUnsupported)
	}

	return width, nil
}

func (i *fitProgramInspector) simpleEncoding(
	ctx context.Context,
	object types.Object,
	fallback *simpleencodings.Encoding,
) (simpleencodings.Encoding, error) {
	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return *fallback, fitResourceError(err)
	}

	switch value := value.(type) {
	case nil:
		return *fallback, nil
	case types.Name:
		return variableNamedEncoding(string(value))
	case types.Dict:
		if raw, found := value[keyBaseEncoding]; found {
			name, readErr := i.name(ctx, raw)
			if readErr != nil {
				return *fallback, fitResourceError(readErr)
			}

			*fallback, err = variableNamedEncoding(name)
			if err != nil {
				return *fallback, fitResourceError(err)
			}
		}

		return i.fitEncodingDifferences(ctx, value, fallback)
	default:
		return *fallback, fmt.Errorf("%w: source byte encoding is not a name or dictionary", errFitUnsupported)
	}
}

func (i *fitProgramInspector) fitEncodingDifferences(
	ctx context.Context,
	dict types.Dict,
	encoding *simpleencodings.Encoding,
) (simpleencodings.Encoding, error) {
	values, err := i.pdf.DereferenceArrayContext(ctx, dict[keyDifferences])
	if err != nil {
		return *encoding, fitResourceError(err)
	}

	if len(values) > fitEncodingDifferenceLimit {
		return *encoding, fmt.Errorf("%w: source encoding Differences exceeds 1024 entries", errFitUnsupported)
	}

	code := -1

	for _, raw := range values {
		value, readErr := i.pdf.DereferenceContext(ctx, raw)
		if readErr != nil {
			return *encoding, fitResourceError(readErr)
		}

		code, err = fitEncodingEntry(value, code, encoding)
		if err != nil {
			return *encoding, fitResourceError(err)
		}
	}

	return *encoding, nil
}

func fitSimpleAdvance(state *fitGraphicsState, width float64, code byte) error {
	spacing := state.charSpace
	if code == fitSpaceCode {
		spacing += state.wordSpace
	}

	x := (width*state.fontSize + spacing) * state.horizontal

	return fitAdvanceText(state, x, 0)
}

func (i *fitProgramInspector) advanceSimpleText(
	ctx context.Context,
	dict types.Dict,
	kind string,
	text []byte,
	state *fitGraphicsState,
) error {
	metrics, found := i.simpleFonts[state.font.identity]
	if !found {
		var err error

		metrics, err = i.simpleMetrics(ctx, dict, kind)
		if err != nil {
			return fitResourceError(err)
		}

		i.simpleFonts[state.font.identity] = metrics
	}

	if !metrics.known {
		state.textPositionKnown = false
		return nil
	}

	for _, code := range text {
		if err := i.step(ctx); err != nil {
			return fitResourceError(err)
		}

		if err := fitSimpleAdvance(state, metrics.widths[code], code); err != nil {
			return fitResourceError(err)
		}
	}

	return nil
}

func fitEncodingEntry(value types.Object, code int, encoding *simpleencodings.Encoding) (int, error) {
	switch value := value.(type) {
	case types.Integer:
		code = int(value)
		if code < 0 || code >= len(encoding) {
			return code, fmt.Errorf("%w: source encoding code is outside 0..255", errFitUnsupported)
		}

		return code, nil
	case types.Name:
		if code < 0 || code >= len(encoding) {
			return code, fmt.Errorf("%w: source glyph name lacks valid preceding code", errFitUnsupported)
		}

		(*encoding)[code] = string(value)

		return code + 1, nil
	default:
		return code, fmt.Errorf("%w: source Differences member is not integer/name", errFitUnsupported)
	}
}
