// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"errors"
	"fmt"
	"math"
)

// Built-in appearance defaults for generated blanks.
const (
	defaultFont           Font    = "Helvetica"
	defaultFontSize       Length  = 12
	defaultLeading        float64 = 1.2
	defaultTextMargin     Length  = 36
	maxLeading            float64 = 10
	minFontSize           Length  = 1
	maxTextValueRuneCount         = 10000
)

// TextStyle is the partial, layerable description of the text printed on a blank.
// An empty Value means no text.
type TextStyle struct {
	Value   Option[string]
	Font    Option[Font]
	Size    Option[Length]
	Color   Option[Color]
	Anchor  Option[Anchor]
	X       Option[Length]
	Y       Option[Length]
	Width   Option[Length]
	Align   Option[TextAlign]
	Leading Option[float64]
}

// BlankStyle is the partial, layerable description of a generated blank page.
// Layers are combined with Over; Resolve applies built-in defaults.
type BlankStyle struct {
	Size       Option[PageSize]
	Background Option[Color]
	Text       TextStyle
}

// Over returns a style in which fields set on s win and the remaining fields come from base.
func (s BlankStyle) Over(base BlankStyle) BlankStyle {
	return BlankStyle{
		Size:       s.Size.Over(base.Size),
		Background: s.Background.Over(base.Background),
		Text: TextStyle{
			Value:   s.Text.Value.Over(base.Text.Value),
			Font:    s.Text.Font.Over(base.Text.Font),
			Size:    s.Text.Size.Over(base.Text.Size),
			Color:   s.Text.Color.Over(base.Text.Color),
			Anchor:  s.Text.Anchor.Over(base.Text.Anchor),
			X:       s.Text.X.Over(base.Text.X),
			Y:       s.Text.Y.Over(base.Text.Y),
			Width:   s.Text.Width.Over(base.Text.Width),
			Align:   s.Text.Align.Over(base.Text.Align),
			Leading: s.Text.Leading.Over(base.Text.Leading),
		},
	}
}

// IsZero reports whether no field is set.
func (s BlankStyle) IsZero() bool {
	return s == BlankStyle{}
}

// TextSpec is the fully resolved text of a blank. An empty Value means no text.
type TextSpec struct {
	Value   string
	Font    Font
	Size    Length
	Color   Color
	Anchor  Anchor
	X, Y    Length
	Width   Length
	Align   TextAlign
	Leading float64
}

// BlankSpec is a fully resolved, directly renderable blank page. It is comparable,
// so identical blanks can be rendered once and reused.
type BlankSpec struct {
	Dim        PageDim
	Background Option[Color]
	Text       TextSpec
}

// Resolve applies built-in defaults and the inherited page size, validating every value.
func (s BlankStyle) Resolve(inherited PageDim) (BlankSpec, error) {
	size := s.Size.OrElse(PageSize{Inherit: true})

	dim := size.Dim
	if size.Inherit {
		dim = inherited
	}

	text := TextSpec{
		Value:   s.Text.Value.OrElse(""),
		Font:    s.Text.Font.OrElse(defaultFont),
		Size:    s.Text.Size.OrElse(defaultFontSize),
		Color:   s.Text.Color.OrElse(Color{}),
		Anchor:  s.Text.Anchor.OrElse(AnchorCenter),
		X:       s.Text.X.OrElse(0),
		Y:       s.Text.Y.OrElse(0),
		Width:   s.Text.Width.OrElse(max(dim.Width-2*defaultTextMargin, minPageSideLength)),
		Align:   s.Text.Align.OrElse(AlignCenter),
		Leading: s.Text.Leading.OrElse(defaultLeading),
	}
	spec := BlankSpec{Dim: dim, Background: s.Background, Text: text}

	err := spec.validate()
	if err != nil {
		return BlankSpec{}, err
	}

	return spec, nil
}

func (b BlankSpec) validate() error {
	var problems []error
	if b.Dim.Width < minPageSideLength || b.Dim.Height < minPageSideLength {
		problems = append(problems, errors.New("page size is smaller than 1 point per side"))
	}

	if b.Text.Size < minFontSize || b.Text.Size > maxLength {
		problems = append(problems, fmt.Errorf("text size must be between %.0f and %.0f points", float64(minFontSize), float64(maxLength)))
	}

	if math.IsNaN(b.Text.Leading) || b.Text.Leading < 1 || b.Text.Leading > maxLeading {
		problems = append(problems, fmt.Errorf("text leading must be between 1 and %.0f", maxLeading))
	}

	if b.Text.Width < minPageSideLength {
		problems = append(problems, errors.New("text width must be positive"))
	}

	if len([]rune(b.Text.Value)) > maxTextValueRuneCount {
		problems = append(problems, fmt.Errorf("text exceeds %d characters", maxTextValueRuneCount))
	}

	return errors.Join(problems...)
}
