// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	nameWinAnsiEncoding  = "WinAnsiEncoding"
	nameMacRomanEncoding = "MacRomanEncoding"
	keyBaseEncoding      = "BaseEncoding"
	nameSymbol           = "Symbol"
	nameZapfDingbats     = "ZapfDingbats"
	nameType1            = "Type1"
	keyDifferences       = "Differences"
	fontMetricScale      = 1000
)

func variableFontEncoding(ctx context.Context, pdf *model.Context, dict types.Dict, base string) (simpleencodings.Encoding, error) {
	object, found := dict.Find(keyEncoding)
	if !found {
		return variableBuiltinEncoding(ctx, pdf, dict, base)
	}

	value, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return simpleencodings.Encoding{}, fmt.Errorf("variable font encoding: %w", err)
	}

	switch value := value.(type) {
	case types.Name:
		return variableNamedEncoding(string(value))
	case types.Dict:
		name, _, readErr := pdf.DereferenceNameEntryContext(ctx, value, keyBaseEncoding)
		if readErr != nil {
			return simpleencodings.Encoding{}, fmt.Errorf("variable BaseEncoding: %w", readErr)
		}

		var encoding simpleencodings.Encoding
		if name == nil {
			encoding, err = variableBuiltinEncoding(ctx, pdf, dict, base)
		} else {
			encoding, err = variableNamedEncoding(string(*name))
		}

		if err != nil {
			return encoding, err
		}

		if differenceErr := variableEncodingDifferences(ctx, pdf, value, &encoding); differenceErr != nil {
			return encoding, differenceErr
		}

		return encoding, nil
	default:
		return simpleencodings.Encoding{}, fmt.Errorf("%w: variable font Encoding must be a name or dictionary", errFormState)
	}
}

func variableBuiltinEncoding(ctx context.Context, pdf *model.Context, dict types.Dict, base string) (simpleencodings.Encoding, error) {
	kind, _, err := pdf.DereferenceNameEntryContext(ctx, dict, keySubtype)
	if err != nil || kind == nil || *kind != nameType1 {
		return simpleencodings.Encoding{}, errors.Join(
			err,
			fmt.Errorf("%w: variable font requires an explicit supported Encoding", errFormState),
		)
	}

	descriptor, err := pdf.DereferenceDictContext(ctx, dict["FontDescriptor"])
	if err != nil {
		return simpleencodings.Encoding{}, fmt.Errorf("variable encoding descriptor: %w", err)
	}

	for _, key := range []string{"FontFile", "FontFile2", "FontFile3"} {
		if descriptor[key] != nil {
			return simpleencodings.Encoding{}, fmt.Errorf(
				"%w: font-program intrinsic encoding is unsupported for variable regeneration", errFormState,
			)
		}
	}

	if base == nameSymbol || base == nameZapfDingbats {
		return variableCoreEncoding(base), nil
	}

	return simpleencodings.AdobeStandard, nil
}

func variableNamedEncoding(name string) (simpleencodings.Encoding, error) {
	switch name {
	case nameWinAnsiEncoding:
		return simpleencodings.WinAnsi, nil
	case nameMacRomanEncoding:
		return simpleencodings.MacRoman, nil
	case "MacExpertEncoding":
		return simpleencodings.MacExpert, nil
	case "StandardEncoding":
		return simpleencodings.AdobeStandard, nil
	default:
		return simpleencodings.Encoding{}, fmt.Errorf("%w: unsupported variable font Encoding /%s", errFormState, name)
	}
}

func variableEncodingDifferences(ctx context.Context, pdf *model.Context, dict types.Dict, encoding *simpleencodings.Encoding) error {
	values, err := pdf.DereferenceArrayContext(ctx, dict[keyDifferences])
	if err != nil {
		return fmt.Errorf("variable font Differences: %w", err)
	}

	code := -1

	for _, object := range values {
		value, readErr := pdf.DereferenceContext(ctx, object)
		if readErr != nil {
			return fmt.Errorf("variable glyph name: %w", readErr)
		}

		switch value := value.(type) {
		case types.Integer:
			code = int(value)
			if code < 0 || code > 255 {
				return fmt.Errorf("%w: variable Differences code must be in 0..255", errFormState)
			}
		case types.Name:
			if code < 0 || code >= len(encoding) {
				return fmt.Errorf("%w: variable Differences requires a valid preceding code", errFormState)
			}

			encoding[code] = string(value)
			code++
		default:
			return fmt.Errorf("%w: variable Differences requires integer/name entries", errFormState)
		}
	}

	return nil
}
