// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func footerDigits(config packet.PageNumbers) (*typeset.Digits, error) {
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		return nil, err
	}
	return typeset.NewDigits(font, config.Size)
}

func footerPlacement(digits *typeset.Digits, size packet.Size, number int64, margin float64) (*typeset.Placed, error) {
	if number < 1 || math.IsNaN(margin) || math.IsInf(margin, 0) || margin < 0 {
		return nil, refuse(diag.TextOverflow, "Invalid physical footer placement.")
	}
	text := strconv.FormatInt(number, 10)
	width := float64(len(text)) * digits.CellWidth
	placed, err := digits.Place(text, (size.Width+width)/2, 0)
	if err != nil {
		return nil, err
	}
	if placed.InkBounds == nil {
		return nil, refuse(diag.TextOverflow, "The physical page number has no painted glyphs.")
	}
	unshifted := placed
	placed, err = digits.Place(text, (size.Width+width)/2, margin-placed.InkBounds.Y)
	if err != nil {
		return nil, err
	}
	if !footerFits(placed, size) {
		return nil, footerOverflow(digits, size, number, margin, unshifted, placed)
	}
	return placed, nil
}

func footerFits(placed *typeset.Placed, size packet.Size) bool {
	for _, bounds := range []*typeset.Rect{&placed.Bounds, placed.InkBounds} {
		if bounds == nil {
			continue
		}
		if !(bounds.X >= 0 && bounds.Y >= 0 && bounds.X+bounds.Width <= size.Width && bounds.Y+bounds.Height <= size.Height) {
			return false
		}
	}
	return true
}

// footerOverflow describes all failed axes after the original logical/ink guard
// refuses placement. Unshifted measurements estimate margin limits; actual
// placement validates each numerical hint.
func footerOverflow(digits *typeset.Digits, size packet.Size, number int64, margin float64, unshifted, placed *typeset.Placed) error {
	ink := placed.InkBounds
	left := math.Min(placed.Bounds.X, ink.X)
	right := math.Max(placed.Bounds.X+placed.Bounds.Width, ink.X+ink.Width)
	bottom := math.Min(placed.Bounds.Y, ink.Y)
	top := math.Max(placed.Bounds.Y+placed.Bounds.Height, ink.Y+ink.Height)
	below, above := bottom < 0, top > size.Height
	var advice []string
	if left < 0 || right > size.Width {
		advice = append(advice, "The page number exceeds the physical page width; reduce page_numbers.size or increase sheet width.")
	}
	if below || above {
		side := "below"
		if above {
			side = "above"
		}
		if below && above {
			side = "below and above"
		}
		advice = append(advice, fmt.Sprintf("The page number extends %s the physical page.", side))
		unshiftedBottom := math.Min(unshifted.Bounds.Y, unshifted.InkBounds.Y)
		unshiftedTop := math.Max(unshifted.Bounds.Y+unshifted.Bounds.Height, unshifted.InkBounds.Y+unshifted.InkBounds.Height)
		if height := unshiftedTop - unshiftedBottom; height > size.Height {
			advice = append(advice, fmt.Sprintf("Its complete logical and ink envelope is %g pt high, exceeding the %g pt sheet height; reduce page_numbers.size or increase sheet height. A margin change alone cannot fit it.", height, size.Height))
		} else {
			minimum := unshifted.InkBounds.Y - unshiftedBottom
			maximum := size.Height - (unshiftedTop - unshifted.InkBounds.Y)
			// Prefer readable point values, retaining precision for tight sheets.
			direction, limit, proposed := "Increase", "at least", minimum
			if below {
				if rounded := math.Ceil(minimum*1000) / 1000; rounded <= maximum {
					proposed = rounded
				}
			} else {
				direction, limit, proposed = "Decrease", "at most", maximum
				if rounded := math.Floor(maximum*1000) / 1000; rounded >= minimum {
					proposed = rounded
				}
			}
			text := strconv.FormatInt(number, 10)
			candidate, err := digits.Place(text, (size.Width+unshifted.Bounds.Width)/2, proposed-unshifted.InkBounds.Y)
			if err == nil && footerFits(candidate, size) {
				advice = append(advice, fmt.Sprintf("%s page_numbers.margin to %s %g pt.", direction, limit, proposed))
			} else {
				advice = append(advice, direction+" page_numbers.margin. If it still does not fit vertically, reduce page_numbers.size or increase sheet height.")
			}
		}
	}
	if len(advice) == 0 {
		advice = append(advice, "The measured footer bounds or physical viewport are invalid.")
	}
	return refuse(diag.TextOverflow, fmt.Sprintf("Physical page %d cannot contain its page number at size %g pt and margin %g pt. %s", number, placed.Size, margin, strings.Join(advice, " ")))
}

// CheckNumberPlacement predicts one footer using the packet's already measured
// digit cells, without imposing generated-canvas limits on source viewports.
func CheckNumberPlacement(digits *typeset.Digits, size packet.Size, number int64, margin float64) error {
	_, err := footerPlacement(digits, size, number, margin)
	return err
}

type physicalViewport struct {
	size   packet.Size
	matrix [6]float64
}

func pageViewport(c context.Context, s *model.Context, page int) (types.Dict, physicalViewport, error) {
	d, _, attrs, err := s.PageDict(c, page, false)
	if err != nil {
		return nil, physicalViewport{}, err
	}
	box := *attrs.MediaBox
	if crop := attrs.CropBox; crop != nil {
		box.LL.X = math.Max(box.LL.X, crop.LL.X)
		box.LL.Y = math.Max(box.LL.Y, crop.LL.Y)
		box.UR.X = math.Min(box.UR.X, crop.UR.X)
		box.UR.Y = math.Min(box.UR.Y, crop.UR.Y)
	}
	unit := 1.0
	value, e := s.Dereference(d["UserUnit"])
	if e != nil {
		return nil, physicalViewport{}, e
	}
	switch v := value.(type) {
	case types.Integer:
		unit = float64(v)
	case types.Float:
		unit = float64(v)
	}
	size, err := displayedSize(c, s, page)
	if err != nil {
		return nil, physicalViewport{}, err
	}
	inverse := 1 / unit
	matrix := [6]float64{inverse, 0, 0, inverse, box.LL.X, box.LL.Y}
	switch (attrs.Rotate%360 + 360) % 360 {
	case 90:
		matrix = [6]float64{0, inverse, -inverse, 0, box.UR.X, box.LL.Y}
	case 180:
		matrix = [6]float64{-inverse, 0, 0, -inverse, box.UR.X, box.UR.Y}
	case 270:
		matrix = [6]float64{0, -inverse, inverse, 0, box.LL.X, box.UR.Y}
	}
	return d, physicalViewport{size: size, matrix: matrix}, nil
}

func copiedDict(original types.Dict) types.Dict {
	result := types.Dict{}
	for key, value := range original {
		result[key] = value
	}
	return result
}

// applyPageNumbers adds fresh page-local wrappers and font dictionaries after
// fitting. Original streams, annotation coordinates and page geometry stay intact.
func applyPageNumbers(c context.Context, s *model.Context, layout packet.Layout, prepared map[typeset.Digest]typeset.EmbeddedFont, names map[typeset.Digest]string) error {
	if layout.PageNumbers == nil {
		return nil
	}
	digits, err := footerDigits(*layout.PageNumbers)
	if err != nil {
		return err
	}
	font, ok := prepared[digits.Font.Identity()]
	if !ok {
		return ErrInvalidPage
	}
	name := names[digits.Font.Identity()]
	if name == "" {
		return ErrInvalidPage
	}
	fontRef, err := buildFontResource(c, s, font, name)
	if err != nil {
		return err
	}
	spanIndex := 0
	for page := 1; page <= s.PageCount; page++ {
		if err = c.Err(); err != nil {
			return err
		}
		for spanIndex < len(layout.Parts) && int64(page) > layout.Parts[spanIndex].LastPage {
			spanIndex++
		}
		if spanIndex >= len(layout.Parts) {
			return ErrInvalidPage
		}
		part := layout.Parts[spanIndex]
		if part.Kind == packet.PartPadding {
			continue
		}
		d, viewport, e := pageViewport(c, s, page)
		if e != nil {
			return e
		}
		placed, e := footerPlacement(digits, viewport.size, int64(page), layout.PageNumbers.Margin)
		if e != nil {
			diagnostic := Diagnostic(e)
			return &policyError{code: diagnostic.Code, err: e, file: part.Source, items: []string{part.Ref, "/page_numbers"}}
		}
		_, _, attrs, e := s.PageDict(c, page, false)
		if e != nil {
			return e
		}
		resources := copiedDict(attrs.Resources)
		fonts, e := s.DereferenceDict(resources["Font"])
		if e != nil {
			return e
		}
		fonts = copiedDict(fonts)
		key := "PCFooter"
		for index := 1; ; index++ {
			if _, exists := fonts[key]; !exists {
				break
			}
			key = "PCFooter" + strconv.Itoa(index)
		}
		fonts[key] = *fontRef
		resources["Font"] = fonts
		d["Resources"] = resources
		contents := types.Array{}
		prefix, e := addStream(s, []byte("q\n"), nil)
		if e != nil {
			return e
		}
		contents = append(contents, *prefix)
		if original := d["Contents"]; original != nil {
			value, e := s.Dereference(original)
			if e != nil {
				return e
			}
			if array, ok := value.(types.Array); ok {
				contents = append(contents, array...)
			} else {
				contents = append(contents, original)
			}
		}
		doc := &document{fonts: &fontSet{index: map[typeset.Digest]int{digits.Font.Identity(): 0}, list: []*typeset.Font{digits.Font}, used: make([][glyphSetWords]uint64, 1)}, fontKeys: []string{key}, content: []byte("Q\nq\n")}
		m := viewport.matrix
		doc.content = fmt.Appendf(doc.content, "%s %s %s %s %s %s cm\n", number(m[0]), number(m[1]), number(m[2]), number(m[3]), number(m[4]), number(m[5]))
		doc.appendColor(Color{})
		doc.appendText(placed, 0)
		doc.content = append(doc.content, "Q\n"...)
		overlay, e := addStream(s, doc.content, nil)
		if e != nil {
			return e
		}
		contents = append(contents, *overlay)
		d["Contents"] = contents
	}
	return nil
}
