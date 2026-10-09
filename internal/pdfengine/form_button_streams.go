// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (p *buttonAppearancePlan) apply(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("materialize button: %w", err)
	}

	off, offErr := p.stream(pdf, types.Name(buttonOffState))
	if offErr != nil {
		return offErr
	}

	on, onErr := p.stream(pdf, p.on)
	if onErr != nil {
		return onErr
	}

	appearance, err := pdf.DereferenceDictContext(ctx, dict["AP"])
	if err != nil {
		return fmt.Errorf("button AP: %w", err)
	}

	local := maps.Clone(appearance)
	local["N"] = types.Dict{buttonOffState: *off, string(p.on): *on}
	dict["AP"] = local

	return nil
}

func (p *buttonAppearancePlan) stream(pdf *model.Context, state types.Name) (*types.IndirectRef, error) {
	bg, bc := p.background, p.borderColor
	content := fmt.Sprintf(
		"%s %s %s rg\n0 0 %s %s re f\n%s w\n%s %s %s RG\n%s %s %s %s re s\n",
		appearancePDFNumber(
			bg[0],
		),
		appearancePDFNumber(bg[1]),
		appearancePDFNumber(bg[2]),
		appearancePDFNumber(p.width),
		appearancePDFNumber(p.height),
		appearancePDFNumber(p.border),
		appearancePDFNumber(bc[0]),
		appearancePDFNumber(bc[1]),
		appearancePDFNumber(bc[2]),
		appearancePDFNumber(
			p.border/2,
		),
		appearancePDFNumber(p.border/2),
		appearancePDFNumber(p.width-p.border),
		appearancePDFNumber(p.height-p.border),
	)
	resources := types.Dict{}

	if state != buttonOffState {
		font, err := pdf.IndRefForNewObject(
			types.Dict{keyType: types.Name("Font"), keySubtype: types.Name("Type1"), keyBaseFont: types.Name("ZapfDingbats")},
		)
		if err != nil {
			return nil, fmt.Errorf("button appearance font: %w", err)
		}

		resources["Font"] = types.Dict{"ZaDb": *font}
		content += fmt.Sprintf("q\nBT\n/ZaDb %.2f Tf 0 g 1 0 0 1 %.2f %.2f Tm\n<%02x> Tj\nET\nQ\n", p.fontSize, p.textX, p.textY, p.caption)
	}

	stream, err := pdf.NewStreamDictForBuf([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("button appearance stream: %w", err)
	}

	stream.Dict[keyType] = types.Name(keyXObject)
	stream.Dict[keySubtype] = types.Name(formXObjectSubtype)
	stream.Dict["FormType"] = types.Integer(1)
	stream.Dict["BBox"] = types.NewNumberArray(0, 0, p.width, p.height)

	stream.Dict["Resources"] = resources
	if err = stream.Encode(); err != nil {
		return nil, fmt.Errorf("encode button appearance: %w", err)
	}

	reference, err := pdf.IndRefForNewObject(*stream)
	if err != nil {
		return nil, fmt.Errorf("store button appearance: %w", err)
	}

	return reference, nil
}

// appearancePDFNumber emits PDF real syntax without exponent notation or precision loss.
func appearancePDFNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
