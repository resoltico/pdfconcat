// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type placementError string

func (e placementError) Error() string { return string(e) }

// PageNumberLayout owns the one digit measurement used for page-number placement.
type PageNumberLayout struct {
	digits  *typeset.Digits
	numbers packet.PageNumbers
}

// NewPageNumberLayout measures the page-number font once.
func NewPageNumberLayout(font *typeset.Font, numbers packet.PageNumbers) (*PageNumberLayout, error) {
	digits, err := typeset.NewDigits(font, numbers.Size)
	if err != nil {
		return nil, err
	}
	return &PageNumberLayout{digits: digits, numbers: numbers}, nil
}

// Place measures a footer on a physical page.
func (f *PageNumberLayout) Place(size packet.Size, number int64) (*typeset.Placed, error) {
	position := string(f.numbers.Position)
	if position == "" || position == "bottom-center" {
		return bottomNumberPlacement(f.digits, size, number, f.numbers.Margin)
	}
	text := strconv.FormatInt(number, 10)
	width := float64(len(text)) * f.digits.CellWidth
	placed, err := f.digits.Place(text, width, 0)
	if err != nil {
		return nil, err
	}
	if placed.InkBounds == nil {
		return nil, placementError("The page number has no painted glyphs.")
	}
	ink := placed.InkBounds
	dx := (size.Width - width) / 2
	dy := f.numbers.Margin - ink.Y
	if strings.HasSuffix(position, "-left") {
		dx = f.numbers.Margin - ink.X
	} else if strings.HasSuffix(position, "-right") {
		dx = size.Width - f.numbers.Margin - ink.X - ink.Width
	}
	if strings.HasPrefix(position, "top-") {
		dy = size.Height - f.numbers.Margin - ink.Y - ink.Height
	}
	placed, err = f.digits.Place(text, width+dx, dy)
	if err != nil {
		return nil, err
	}
	if !numberFits(placed, size) {
		return nil, placementError(fmt.Sprintf("Page number %d at %s cannot fit its complete logical and ink bounds; reduce page_numbers.size or margin, or use a larger page.", number, position))
	}
	return placed, nil
}

// Check verifies that the page can contain its footer.
func (f *PageNumberLayout) Check(size packet.Size, number int64) error {
	_, err := f.Place(size, number)
	return err
}

// Font returns the font used by every footer.
func (f *PageNumberLayout) Font() *typeset.Font { return f.digits.Font }

// Union measures all footer glyphs for font resource preparation.
func (f *PageNumberLayout) Union() (*typeset.Placed, error) {
	return f.digits.Place("0123456789", 10*f.digits.CellWidth, 0)
}

// ExclusionTop returns the physical top of the footer's logical and ink envelope.
func (f *PageNumberLayout) ExclusionTop() float64 {
	return f.numbers.Margin - f.digits.InkBottom + f.digits.Top
}

func bottomNumberPlacement(digits *typeset.Digits, size packet.Size, number int64, margin float64) (*typeset.Placed, error) {
	if number < 1 || math.IsNaN(margin) || math.IsInf(margin, 0) || margin < 0 {
		return nil, placementError("Invalid physical footer placement.")
	}
	text := strconv.FormatInt(number, 10)
	width := float64(len(text)) * digits.CellWidth
	placed, err := digits.Place(text, (size.Width+width)/2, 0)
	if err != nil {
		return nil, err
	}
	if placed.InkBounds == nil {
		return nil, placementError("The physical page number has no painted glyphs.")
	}
	unshifted := placed
	placed, err = digits.Place(text, (size.Width+width)/2, margin-placed.InkBounds.Y)
	if err != nil {
		return nil, err
	}
	if !numberFits(placed, size) {
		return nil, bottomNumberOverflow(digits, size, number, margin, unshifted, placed)
	}
	return placed, nil
}

func numberFits(placed *typeset.Placed, size packet.Size) bool {
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

// bottomNumberOverflow describes all failed axes after the original logical/ink guard
// refuses placement. Unshifted measurements estimate margin limits; actual
// placement validates each numerical hint.
func bottomNumberOverflow(digits *typeset.Digits, size packet.Size, number int64, margin float64, unshifted, placed *typeset.Placed) error {
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
			if err == nil && numberFits(candidate, size) {
				advice = append(advice, fmt.Sprintf("%s page_numbers.margin to %s %g pt.", direction, limit, proposed))
			} else {
				advice = append(advice, direction+" page_numbers.margin. If it still does not fit vertically, reduce page_numbers.size or increase sheet height.")
			}
		}
	}
	if len(advice) == 0 {
		advice = append(advice, "The measured footer bounds or physical viewport are invalid.")
	}
	return placementError(fmt.Sprintf("Physical page %d cannot contain its page number at size %g pt and margin %g pt. %s", number, placed.Size, margin, strings.Join(advice, " ")))
}

// Available returns the generated canvas interval left by the selected physical
// overlay, inverse transformed through proportional fitting before pagination.
func (f *PageNumberLayout) Available(canvas packet.Size, fit *packet.Size) (float64, float64) {
	physical := canvas
	if fit != nil {
		physical = *fit
	}
	scale, offset := 1.0, 0.0
	if fit != nil {
		scale = math.Min(fit.Width/canvas.Width, fit.Height/canvas.Height)
		offset = (fit.Height - canvas.Height*scale) / 2
	}
	bottom, top := 0.0, canvas.Height
	if strings.HasPrefix(string(f.numbers.Position), "top-") {
		edge := physical.Height - f.numbers.Margin - f.digits.InkTop + f.digits.Bottom
		top = math.Min(top, (edge-6-offset)/scale)
	} else {
		bottom = math.Max(bottom, (f.ExclusionTop()+6-offset)/scale)
	}
	return bottom, top
}
