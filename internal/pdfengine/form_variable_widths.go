// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	pdffont "github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/text/encoding/charmap"
)

func (f *variableFont) readWidths(
	ctx context.Context, pdf *model.Context, dict types.Dict, base string, encoding *simpleencodings.Encoding,
) error {
	if _, found := dict.Find("Widths"); found {
		return f.readDeclaredWidths(ctx, pdf, dict)
	}

	kind, _, err := pdf.DereferenceNameEntryContext(ctx, dict, keySubtype)
	if err != nil || kind == nil || *kind != nameType1 || !pdffont.IsCoreFont(base) {
		return errors.Join(err, fmt.Errorf("%w: variable font %s needs declared glyph widths", errFormState, base))
	}

	descriptor, err := pdf.DereferenceDictContext(ctx, dict["FontDescriptor"])
	if err != nil {
		return fmt.Errorf("variable metric descriptor: %w", err)
	}

	for _, key := range []string{"FontFile", "FontFile2", "FontFile3"} {
		if descriptor[key] != nil {
			return fmt.Errorf("%w: embedded variable font requires declared glyph widths", errFormState)
		}
	}

	nominal := variableMetricCodes(base)
	for code, name := range encoding {
		metricCode, found := nominal[name]
		if !found {
			continue
		}

		width, metricErr := pdffont.CharWidth(ctx, base, rune(metricCode))
		if metricErr != nil {
			return fmt.Errorf("variable font metric: %w", metricErr)
		}

		f.widths[code] = float64(width) / fontMetricScale
		f.known[code] = true
	}

	return nil
}

func variableMetricCodes(base string) map[string]byte {
	codes := map[string]byte{}

	encoding := variableCoreEncoding(base)

	runes := encoding.NameToRune()
	for code, glyph := range encoding {
		if glyph == "" {
			continue
		}

		if base == nameSymbol || base == nameZapfDingbats {
			if _, found := codes[glyph]; !found {
				codes[glyph] = byte(code)
			}

			continue
		}

		canonical, found := charmap.Windows1252.EncodeRune(runes[glyph])
		if found && encoding[canonical] == glyph {
			codes[glyph] = canonical
		}
	}

	return codes
}

func (f *variableFont) readDeclaredWidths(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	values, first, err := variableWidthRange(ctx, pdf, dict)
	if err != nil {
		return err
	}

	missing, err := variableMissingWidth(ctx, pdf, dict)
	if err != nil {
		return err
	}

	for code := range f.widths {
		f.widths[code] = missing
		f.known[code] = true
	}

	for index, object := range values {
		width, readErr := pdf.DereferenceNumberContext(ctx, object)
		if readErr != nil || math.IsInf(width, 0) || math.IsNaN(width) || width < 0 {
			return errors.Join(readErr, fmt.Errorf("%w: variable glyph widths must be finite nonnegative numbers", errFormState))
		}

		f.widths[first+index] = width / fontMetricScale
	}

	return nil
}

func variableWidthRange(ctx context.Context, pdf *model.Context, dict types.Dict) (types.Array, int, error) {
	values, err := pdf.DereferenceArrayContext(ctx, dict["Widths"])
	if err != nil || len(values) == 0 {
		return nil, 0, errors.Join(err, fmt.Errorf("%w: variable font Widths must be a nonempty number array", errFormState))
	}

	first, err := pdf.DereferenceIntegerContext(ctx, dict["FirstChar"])
	if err != nil || first == nil || first.Value() < 0 || first.Value() > 255 || len(values) > 256-first.Value() {
		return nil, 0, errors.Join(err, fmt.Errorf("%w: variable font Widths range must fit character codes 0..255", errFormState))
	}

	return values, first.Value(), nil
}

func variableMissingWidth(ctx context.Context, pdf *model.Context, dict types.Dict) (float64, error) {
	descriptor, err := pdf.DereferenceDictContext(ctx, dict["FontDescriptor"])
	if err != nil {
		return 0, fmt.Errorf("variable font descriptor: %w", err)
	}

	object, found := descriptor.Find("MissingWidth")
	if !found {
		return 0, nil
	}

	width, err := pdf.DereferenceNumberContext(ctx, object)
	if err != nil || math.IsNaN(width) || math.IsInf(width, 0) || width < 0 {
		return 0, errors.Join(err, fmt.Errorf("%w: variable MissingWidth must be finite and nonnegative", errFormState))
	}

	return width / fontMetricScale, nil
}
