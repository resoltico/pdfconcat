// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package model

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ExactInlineImageTail returns the content after EI for supported unfiltered
// device samples or a PDF 2.0 explicit Length. Once the length is known,
// a misplaced delimiter is a corrupt image.
func ExactInlineImageTail(c context.Context, header string, relaxed, pdf20 bool) (tail string, exact bool, err error) {
	rest := header
	var width, height, bits, components int
	explicit := -1
	samples := true
	var firstFilter string
	seen := map[types.Name]bool{}
	for {
		if err := c.Err(); err != nil {
			return "", false, err
		}
		rest, _ = trimLeftSpace(rest, false)
		if len(rest) > 2 && strings.HasPrefix(rest, "ID") && whitespaceOrEOL(rune(rest[2])) {
			break
		}
		keyObject, err := ParseObjectWithPolicy(c, &rest, 0, ValidationStrict, 256)
		key, ok := keyObject.Object.(types.Name)
		if err != nil || keyObject.StrictFailure != nil || !ok {
			return "", false, c.Err()
		}
		value, err := ParseObjectWithPolicy(c, &rest, 0, ValidationStrict, 256)
		if err != nil || value.StrictFailure != nil {
			return "", false, c.Err()
		}
		n, _ := value.Object.(types.Integer)
		switch key {
		case "W", "Width":
			key, width = "Width", n.Value()
		case "H", "Height":
			key, height = "Height", n.Value()
		case "BPC", "BitsPerComponent":
			key, bits = "BitsPerComponent", n.Value()
		case "CS", "ColorSpace":
			key = "ColorSpace"
			switch value.Object {
			case types.Name("G"), types.Name("DeviceGray"):
				components = 1
			case types.Name("RGB"), types.Name("DeviceRGB"):
				components = 3
			case types.Name("CMYK"), types.Name("DeviceCMYK"):
				components = 4
			default:
				samples = false
			}
		case "L", "Length":
			key = "Length"
			if !pdf20 {
				samples = false
				break
			}
			if length, ok := value.Object.(types.Integer); ok && length.Value() >= 0 {
				explicit = length.Value()
			} else {
				return "", true, fmt.Errorf("%w: invalid inline image Length", errBIExpressionCorrupt)
			}
		case "F", "Filter":
			key = "Filter"
			filter := value.Object
			if filters, ok := filter.(types.Array); ok {
				if len(filters) == 0 {
					break
				}
				filter = filters[0]
			}
			name, ok := filter.(types.Name)
			if !ok {
				return "", false, nil
			}
			firstFilter, samples = string(name), false
		case "IM", "ImageMask":
			key = "ImageMask"
			if value.Object != types.Boolean(false) {
				samples = false
			}
		case "D", "Decode":
			key = "Decode"
		case "I", "Interpolate":
			key = "Interpolate"
		default:
			samples = false
		}
		if seen[key] {
			return "", false, nil
		}
		seen[key] = true
	}
	length := explicit
	if samples && width > 0 && height > 0 && components > 0 &&
		(bits == 1 || bits == 2 || bits == 4 || bits == 8 || bits == 16) {
		if width > (math.MaxInt-7)/(components*bits) {
			return "", true, fmt.Errorf("%w: inline image sample row length overflows", errBIExpressionCorrupt)
		}
		rowBytes := (width*components*bits + 7) / 8
		if height > math.MaxInt/rowBytes {
			return "", true, fmt.Errorf("%w: inline image sample length overflows", errBIExpressionCorrupt)
		}
		length = rowBytes * height
		if explicit >= 0 && length != explicit {
			return "", true, fmt.Errorf("%w: inline image Length disagrees with its sample size", errBIExpressionCorrupt)
		}
	}
	// Preserve the backend's strict PDF 2.0 requirement for a Length
	// entry even when the sample size can be calculated independently.
	if length < 0 || pdf20 && !relaxed && explicit < 0 {
		return "", false, nil
	}
	start, err := inlineImageDataStart(rest, firstFilter)
	if err != nil {
		return "", true, err
	}
	if length >= len(rest)-start {
		return "", true, fmt.Errorf("%w: truncated inline image samples", errBIExpressionCorrupt)
	}
	at := start + length
	if !whitespaceOrEOL(rune(rest[at])) {
		// Preserve the backend's documented relaxed delimiter exception,
		// and only at the sample boundary proved above.
		if relaxed && strings.HasPrefix(rest[at:], "EI") && relaxedEIEnd(rest[at+2:]) {
			return rest[at+2:], true, nil
		}
		return "", true, fmt.Errorf("%w: inline image lacks whitespace before EI at its declared sample boundary", errBIExpressionCorrupt)
	}
	end, err := lookupLengthEI(rest, length, firstFilter)
	if err != nil {
		return "", true, fmt.Errorf("%w: inline image lacks EI at its declared sample boundary", errBIExpressionCorrupt)
	}
	return rest[end+2:], true, nil
}
