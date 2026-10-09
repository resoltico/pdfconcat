// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Ervins Strauhmanis

package parser

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/benoitkugler/pdf/model"
	"github.com/benoitkugler/pdf/reader/parser/filters"
	tkn "github.com/benoitkugler/pstokenizer"
)

func (pr *Parser) parseBoundedImageData(img *cs.OpBeginImage, res model.ResourcesColorSpace) error {
	switch c := img.ColorSpace.(type) {
	case cs.ImageColorSpaceName:
		c.ColorSpaceName = inlineCSName(c.ColorSpaceName)
		img.ColorSpace = c
	case cs.ImageColorSpaceIndexed:
		c.Base = inlineCSName(c.Base)
		img.ColorSpace = c
	}
	l := pr.bounded.limits
	w, h := img.Image.Width, img.Image.Height
	if w <= 0 || h <= 0 || w > l.ImageDimension || h > l.ImageDimension || w > l.ImagePixels/h {
		return errors.New("inline image dimension/pixel limit exceeded")
	}
	comps, bits := 1, 1
	if !img.Image.ImageMask {
		var err error
		if c, ok := img.ColorSpace.(cs.ImageColorSpaceName); ok && pr.bounded.components != nil {
			comps, err = pr.bounded.components(string(c.ColorSpaceName))
			bits = int(img.Image.BitsPerComponent)
		} else {
			comps, bits, err = img.Metrics(res)
		}
		if err != nil {
			return fmt.Errorf("inline image color space: %w", err)
		}
	}
	if len(img.Image.Decode) > 0 && len(img.Image.Decode) != 2*comps {
		return errors.New("inline Decode component count mismatch")
	}
	if comps <= 0 || comps > 32 || (bits != 1 && bits != 2 && bits != 4 && bits != 8 && bits != 16) {
		return errors.New("invalid inline image components or bits")
	}
	if w > (math.MaxInt-7)/comps/bits {
		return errors.New("inline row byte arithmetic overflow")
	}
	row := (w*comps*bits + 7) / 8
	if row > l.InlineDecodedBytes || h > l.InlineDecodedBytes/row {
		return errors.New("inline image decoded byte limit exceeded")
	}
	separator, err := pr.tokens.SkipRawBytes(1)
	if err != nil {
		return err
	}
	if len(separator) != 1 || !tkn.IsAsciiWhitespace(separator[0]) {
		return errors.New("inline image ID requires whitespace")
	}
	if separator[0] == '\r' && len(pr.tokens.Bytes()) > 0 && pr.tokens.Bytes()[0] == '\n' {
		if _, err = pr.tokens.SkipRawBytes(1); err != nil {
			return err
		}
	}
	input := pr.tokens.Bytes()
	n := row * h
	discarded := 0
	remainingWork := l.InlineWorkBytes - pr.bounded.inlineWork
	if remainingWork <= 0 {
		return errors.New("inline aggregate work byte limit exceeded")
	}
	if len(img.Image.Filter) > 0 {
		fi := img.Image.Filter[0]
		n, discarded, err = filters.SkipBounded(pr.bounded.ctx, string(fi.Name), fi.DecodeParms, bytes.NewReader(input), inlineRemainingLimit(l.InlineEncodedBytes, remainingWork), inlineRemainingLimit(l.InlineDecodedBytes, remainingWork), l.ImageDimension)
		if err != nil {
			return fmt.Errorf("inline image filter boundary: %w", err)
		}
	}
	if n > remainingWork || discarded > remainingWork-n {
		return errors.New("inline aggregate work byte limit exceeded")
	}
	pr.bounded.inlineWork += n + discarded
	if n > l.InlineEncodedBytes || n > len(input) {
		return errors.New("inline image encoded byte limit or input length exceeded")
	}
	if n >= len(input) || !tkn.IsAsciiWhitespace(input[n]) {
		return errors.New("inline image EI requires preceding whitespace")
	}
	img.Image.Content = input[:n]
	if _, err = pr.tokens.SkipRawBytes(n); err != nil {
		return err
	}
	// The actual filter EOD (or checked unfiltered length), never a raw EI search, owns this boundary.
	pr.tokens.SetPosition(pr.tokens.CurrentPosition())
	o, err := pr.ParseObject()
	if err != nil {
		return err
	}
	if o != Command("EI") {
		return errors.New("inline image EOD is not followed by EI")
	}
	return nil
}

func inlineCSName(n model.ColorSpaceName) model.ColorSpaceName {
	switch n {
	case "G":
		return model.ColorSpaceGray
	case "RGB":
		return model.ColorSpaceRGB
	case "CMYK":
		return model.ColorSpaceCMYK
	}
	return n
}

func inlineRemainingLimit(limit, remaining int) int {
	if remaining < limit {
		return remaining
	}
	return limit
}
