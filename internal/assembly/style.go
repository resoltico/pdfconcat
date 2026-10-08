// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

type (
	// TextStyle is the partial, layerable description of the text printed on a blank.
	// An empty Value means no text.
	TextStyle struct {
		Font     Field[Font]
		Value    Field[string]
		Size     Field[Length]
		Color    Field[Color]
		Anchor   Field[Anchor]
		X        Field[Length]
		Y        Field[Length]
		Width    Field[Length]
		Align    Field[TextAlign]
		Leading  Field[float64]
		Overflow Field[Overflow]
	}

	// BlankStyle is the partial, layerable description of a generated blank page. Layers combine with Over:
	// per-item over plan defaults over the built-ins that Resolve applies.
	BlankStyle struct {
		Text       TextStyle
		Size       Field[PageSize]
		Background Field[Fill]
	}

	// TextSpec is the fully resolved text of a blank. An empty Value means no text.
	TextSpec struct {
		Value    string
		Font     Font
		Size     Length
		X, Y     Length
		Width    Length
		Leading  float64
		Color    Color
		Anchor   Anchor
		Align    TextAlign
		Overflow Overflow
	}

	// BlankSpec is a fully resolved, directly renderable blank page. It is comparable and carries no
	// provenance, so identical blanks can be rendered once and reused.
	BlankSpec struct {
		Text       TextSpec
		Dim        PageDim
		Background Fill
	}
)

// Built-in appearance defaults for generated blanks, and the limits of the values.
const (
	defaultFontSize   Length  = 12
	defaultLeading    float64 = 1.2
	defaultTextMargin Length  = 36

	// MaxLeading is the largest line height, as a multiple of the font size.
	MaxLeading float64 = 10
	// MinLeading is the smallest line height, as a multiple of the font size.
	MinLeading float64 = 1
	// MinFontSize is the smallest font size.
	MinFontSize Length = 1
	// MaxTextRunes is the longest text of one generated page, in Unicode scalar values.
	MaxTextRunes = 10000
)

// Over returns a style in which fields set on s win and the remaining fields come from base.
func (s *BlankStyle) Over(base *BlankStyle) BlankStyle {
	return BlankStyle{
		Size:       s.Size.Over(base.Size),
		Background: s.Background.Over(base.Background),
		Text: TextStyle{
			Value:    s.Text.Value.Over(base.Text.Value),
			Font:     s.Text.Font.Over(base.Text.Font),
			Size:     s.Text.Size.Over(base.Text.Size),
			Color:    s.Text.Color.Over(base.Text.Color),
			Anchor:   s.Text.Anchor.Over(base.Text.Anchor),
			X:        s.Text.X.Over(base.Text.X),
			Y:        s.Text.Y.Over(base.Text.Y),
			Width:    s.Text.Width.Over(base.Text.Width),
			Align:    s.Text.Align.Over(base.Text.Align),
			Leading:  s.Text.Leading.Over(base.Text.Leading),
			Overflow: s.Text.Overflow.Over(base.Text.Overflow),
		},
	}
}

// IsZero reports whether no field is set.
func (s *BlankStyle) IsZero() bool {
	return *s == BlankStyle{}
}

// Resolve applies built-in defaults and the inherited page size, validating every value. inherited is
// the size of the neighboring source page; the zero PageDim means there is none.
func (s *BlankStyle) Resolve(inherited PageDim) (BlankSpec, error) {
	size := PageSize{Inherit: true}
	s.Size.ApplyTo(&size)

	dim := size.Dim
	if size.Inherit {
		if inherited == (PageDim{}) {
			return BlankSpec{}, ErrUnresolvedSize
		}

		dim = inherited
	}

	spec := BlankSpec{
		Dim: dim,
		Text: TextSpec{
			Size:     defaultFontSize,
			Width:    max(dim.Width-2*defaultTextMargin, MinPageSide),
			Leading:  defaultLeading,
			Anchor:   AnchorCenter,
			Align:    AlignCenter,
			Overflow: OverflowError,
		},
	}

	s.Background.ApplyTo(&spec.Background)
	s.Text.Value.ApplyTo(&spec.Text.Value)
	s.Text.Font.ApplyTo(&spec.Text.Font)
	s.Text.Size.ApplyTo(&spec.Text.Size)
	s.Text.Color.ApplyTo(&spec.Text.Color)
	s.Text.Anchor.ApplyTo(&spec.Text.Anchor)
	s.Text.X.ApplyTo(&spec.Text.X)
	s.Text.Y.ApplyTo(&spec.Text.Y)
	s.Text.Width.ApplyTo(&spec.Text.Width)
	s.Text.Align.ApplyTo(&spec.Text.Align)
	s.Text.Leading.ApplyTo(&spec.Text.Leading)
	s.Text.Overflow.ApplyTo(&spec.Text.Overflow)

	err := spec.Validate()
	if err != nil {
		return BlankSpec{}, err
	}

	return spec, nil
}

// Validate checks every value of the spec, so that a directly constructed spec is as safe as a parsed one.
func (b BlankSpec) Validate() error {
	return errors.Join(b.Dim.Validate(), b.Text.Validate(), b.Text.Width.Validate())
}

// Validate checks the text values.
func (t TextSpec) Validate() error {
	var problems []error

	if t.Size.Validate() != nil || t.Size < MinFontSize {
		problems = append(
			problems,
			fmt.Errorf("%w: text size must be between %.0f and %.0f points", ErrOutOfRange, float64(MinFontSize), float64(MaxLength)),
		)
	}

	problems = append(problems, t.validateOffsets(), t.validateChoices(), ValidateTextValue(t.Value))

	if t.Width < MinPageSide {
		problems = append(problems, fmt.Errorf("%w: text width must be at least one point", ErrOutOfRange))
	}

	if math.IsNaN(t.Leading) || t.Leading < MinLeading || t.Leading > MaxLeading {
		problems = append(problems, fmt.Errorf("%w: text leading must be between %.0f and %.0f", ErrOutOfRange, MinLeading, MaxLeading))
	}

	return errors.Join(problems...)
}

func (t TextSpec) validateOffsets() error {
	var problems []error

	err := t.X.Validate()
	if err != nil {
		problems = append(problems, fmt.Errorf("text x: %w", err))
	}

	err = t.Y.Validate()
	if err != nil {
		problems = append(problems, fmt.Errorf("text y: %w", err))
	}

	return errors.Join(problems...)
}

func (t TextSpec) validateChoices() error {
	var problems []error

	if t.Anchor < AnchorTopLeft || t.Anchor > AnchorBottomRight {
		problems = append(problems, fmt.Errorf("%w: anchor %v", ErrUnknownName, t.Anchor))
	}

	if t.Align < AlignLeft || t.Align > AlignJustify {
		problems = append(problems, fmt.Errorf("%w: alignment %v", ErrUnknownName, t.Align))
	}

	if t.Overflow != OverflowError && t.Overflow != OverflowAllow {
		problems = append(problems, fmt.Errorf("%w: overflow policy %v", ErrUnknownName, t.Overflow))
	}

	return errors.Join(problems...)
}

// ValidateTextValue checks that text is valid UTF-8 of at most MaxTextRunes characters.
func ValidateTextValue(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("%w: text is not valid UTF-8", ErrInvalidText)
	}

	if utf8.RuneCountInString(text) > MaxTextRunes {
		return fmt.Errorf("%w: text exceeds %d characters", ErrInvalidText, MaxTextRunes)
	}

	return nil
}
