// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet

// Setting retains an effective value and its authored declaration.
type Setting[T any] struct {
	Value T
	From  Ref
}

// Font identifies a file, or the bundled font when Path is empty.
type Font struct{ Path string }

// String returns the font's result representation.
func (f Font) String() string {
	if f.Path == "" {
		return "default"
	}
	return f.Path
}

// TextStyle is the effective generated text declaration in points.
type TextStyle struct {
	Value                Setting[string]
	Font                 Setting[Font]
	Size                 Setting[float64]
	Color                Setting[Color]
	Anchor               Setting[Anchor]
	X, Y, Width, Leading Setting[float64]
	Align                Setting[Align]
	Overflow             Setting[Overflow]
}

// PageStyle combines an authored canvas, background and text.
type PageStyle struct {
	Size       Setting[Size]
	Background Setting[*Color]
	Text       TextStyle
}

// Defaults returns the built-in page style.
func Defaults() PageStyle {
	return PageStyle{Text: TextStyle{
		Size: Setting[float64]{Value: 12}, Anchor: Setting[Anchor]{Value: AnchorCenter},
		Align: Setting[Align]{Value: AlignCenter}, Leading: Setting[float64]{Value: 1.2},
		Overflow: Setting[Overflow]{Value: OverflowError},
	}}
}

func over[T any](value, base Setting[T]) Setting[T] {
	if value.From == "" {
		return base
	}
	return value
}

// Over layers declared fields over base, including explicit zero and empty values.
func (s PageStyle) Over(base PageStyle) PageStyle {
	s.Size = over(s.Size, base.Size)
	s.Background = over(s.Background, base.Background)
	s.Text.Value = over(s.Text.Value, base.Text.Value)
	s.Text.Font = over(s.Text.Font, base.Text.Font)
	s.Text.Size = over(s.Text.Size, base.Text.Size)
	s.Text.Color = over(s.Text.Color, base.Text.Color)
	s.Text.Anchor = over(s.Text.Anchor, base.Text.Anchor)
	s.Text.X = over(s.Text.X, base.Text.X)
	s.Text.Y = over(s.Text.Y, base.Text.Y)
	s.Text.Width = over(s.Text.Width, base.Text.Width)
	s.Text.Leading = over(s.Text.Leading, base.Text.Leading)
	s.Text.Align = over(s.Text.Align, base.Text.Align)
	s.Text.Overflow = over(s.Text.Overflow, base.Text.Overflow)
	return s
}
