// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func defaultsStyle() assembly.BlankStyle {
	origin := defaultsOrigin()

	return assembly.BlankStyle{
		Size:       assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: 100, Height: 200}}, origin),
		Background: assembly.Set(assembly.FillColor(assembly.Color{R: 1}), origin),
		Text: assembly.TextStyle{
			Value:    assembly.Set("default text", origin),
			Font:     assembly.Set(assembly.Font{File: "d.ttf", Base: "/b"}, origin),
			Size:     assembly.Set[assembly.Length](20, origin),
			Color:    assembly.Set(assembly.Color{G: 2}, origin),
			Anchor:   assembly.Set(assembly.AnchorTop, origin),
			X:        assembly.Set[assembly.Length](1, origin),
			Y:        assembly.Set[assembly.Length](2, origin),
			Width:    assembly.Set[assembly.Length](50, origin),
			Align:    assembly.Set(assembly.AlignLeft, origin),
			Leading:  assembly.Set(2.0, origin),
			Overflow: assembly.Set(assembly.OverflowAllow, origin),
		},
	}
}

// fieldOrigins lists the origin of every field of a style by name.
func fieldOrigins(style *assembly.BlankStyle) map[string]assembly.Origin {
	text := &style.Text

	return map[string]assembly.Origin{
		"size": style.Size.Origin, "background": style.Background.Origin, "value": text.Value.Origin, "font": text.Font.Origin,
		"fontsize": text.Size.Origin, "color": text.Color.Origin, "anchor": text.Anchor.Origin, "x": text.X.Origin, "y": text.Y.Origin,
		"width": text.Width.Origin, "align": text.Align.Origin,
		leadingName: text.Leading.Origin, "overflow": text.Overflow.Origin,
	}
}

// singleOverrides returns, for each field name, a style that sets only that field.
func singleOverrides() map[string]assembly.BlankStyle {
	origin := itemOrigin()

	return map[string]assembly.BlankStyle{
		"size":       {Size: assembly.Set(assembly.PageSize{Inherit: true}, origin)},
		"background": {Background: assembly.Set(assembly.Fill{}, origin)},
		"value":      {Text: assembly.TextStyle{Value: assembly.Set("", origin)}},
		"font":       {Text: assembly.TextStyle{Font: assembly.Set(assembly.Font{}, origin)}},
		"fontsize":   {Text: assembly.TextStyle{Size: assembly.Set[assembly.Length](9, origin)}},
		"color":      {Text: assembly.TextStyle{Color: assembly.Set(assembly.Color{B: 3}, origin)}},
		"anchor":     {Text: assembly.TextStyle{Anchor: assembly.Set(assembly.AnchorBottom, origin)}},
		"x":          {Text: assembly.TextStyle{X: assembly.Set[assembly.Length](0, origin)}},
		"y":          {Text: assembly.TextStyle{Y: assembly.Set[assembly.Length](0, origin)}},
		"width":      {Text: assembly.TextStyle{Width: assembly.Set[assembly.Length](7, origin)}},
		"align":      {Text: assembly.TextStyle{Align: assembly.Set(assembly.AlignRight, origin)}},
		leadingName:  {Text: assembly.TextStyle{Leading: assembly.Set(1.0, origin)}},
		"overflow":   {Text: assembly.TextStyle{Overflow: assembly.Set(assembly.OverflowError, origin)}},
	}
}

func TestOverLayersPerField(t *testing.T) {
	t.Parallel()

	defaults := defaultsStyle()

	// Every field of the item is set to something different, one at a time, and must win alone: it takes
	// the item's origin, and every other field keeps the defaults' origin.
	for name, override := range singleOverrides() {
		layered := override.Over(&defaults)

		for field, origin := range fieldOrigins(&layered) {
			want := defaultsOrigin()
			if field == name {
				want = itemOrigin()
			}

			if origin != want {
				t.Errorf("override %s: field %s has origin %+v, want %+v", name, field, origin, want)
			}
		}
	}
}

func TestOverEmptyLayers(t *testing.T) {
	t.Parallel()

	var empty assembly.BlankStyle

	defaults := defaultsStyle()

	if !empty.IsZero() || defaults.IsZero() {
		t.Error("IsZero must be true only for an all-unset style")
	}

	if got := empty.Over(&empty); !got.IsZero() {
		t.Errorf("unset over unset = %+v", got)
	}

	if got := empty.Over(&defaults); got != defaults {
		t.Errorf("an empty item inherits the defaults entirely: %+v", got)
	}
}

func TestFieldApplyTo(t *testing.T) {
	t.Parallel()

	target := 7

	var unset assembly.Field[int]

	unset.ApplyTo(&target)

	if unset.IsSet() || target != 7 {
		t.Error("an unset field leaves the target alone")
	}

	assembly.Set(0, itemOrigin()).ApplyTo(&target)

	if target != 0 {
		t.Error("an explicit zero is a value, not an absence")
	}
}

func TestResolveAppliesBuiltIns(t *testing.T) {
	t.Parallel()

	var style assembly.BlankStyle

	spec, err := style.Resolve(a4Dim())
	if err != nil {
		t.Fatal(err)
	}

	want := assembly.BlankSpec{
		Dim: a4Dim(),
		Text: assembly.TextSpec{
			Font: assembly.Font{}, Size: 12, Anchor: assembly.AnchorCenter, Width: a4Dim().Width - 72, Align: assembly.AlignCenter,
			Leading: 1.2, Overflow: assembly.OverflowError,
		},
	}
	if spec != want {
		t.Errorf("built-ins:\n got %+v\nwant %+v", spec, want)
	}

	if spec.Background.Painted {
		t.Error("the built-in background paints nothing")
	}
}

func TestResolveExplicitValues(t *testing.T) {
	t.Parallel()

	origin := itemOrigin()
	style := assembly.BlankStyle{
		Size:       assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: 300, Height: 400}}, origin),
		Background: assembly.Set(assembly.FillColor(assembly.Color{R: 9}), origin),
		Text: assembly.TextStyle{
			Value: assembly.Set("x\ny", origin), Font: assembly.Set(assembly.Font{File: fontFile, Base: "/b"}, origin),
			Size: assembly.Set[assembly.Length](30, origin), Color: assembly.Set(assembly.Color{R: 1, G: 2, B: 3}, origin),
			Anchor: assembly.Set(assembly.AnchorBottomRight, origin), X: assembly.Set[assembly.Length](-5, origin),
			Y: assembly.Set[assembly.Length](6, origin), Width: assembly.Set[assembly.Length](120, origin),
			Align: assembly.Set(assembly.AlignJustify, origin), Leading: assembly.Set(10.0, origin),
			Overflow: assembly.Set(assembly.OverflowAllow, origin),
		},
	}

	spec, err := style.Resolve(a4Dim())
	if err != nil {
		t.Fatal(err)
	}

	want := assembly.BlankSpec{
		Dim: assembly.PageDim{Width: 300, Height: 400}, Background: assembly.FillColor(assembly.Color{R: 9}),
		Text: assembly.TextSpec{
			Value:    "x\ny",
			Font:     assembly.Font{File: fontFile, Base: "/b"},
			Size:     30,
			Color:    assembly.Color{R: 1, G: 2, B: 3},
			Anchor:   assembly.AnchorBottomRight,
			X:        -5,
			Y:        6,
			Width:    120,
			Align:    assembly.AlignJustify,
			Leading:  10,
			Overflow: assembly.OverflowAllow,
		},
	}
	if spec != want {
		t.Errorf("explicit:\n got %+v\nwant %+v", spec, want)
	}

	// An explicit size ignores the neighbor, even when there is none.
	_, err = style.Resolve(assembly.PageDim{})
	if err != nil {
		t.Errorf("explicit size without a neighbor: %v", err)
	}
}

func TestResolveNeedsASizeSource(t *testing.T) {
	t.Parallel()

	var style assembly.BlankStyle

	_, err := style.Resolve(assembly.PageDim{})
	if !errors.Is(err, assembly.ErrUnresolvedSize) || !strings.Contains(err.Error(), "explicit size") {
		t.Fatalf("got %v", err)
	}

	inherit := assembly.BlankStyle{Size: assembly.Set(assembly.PageSize{Inherit: true}, itemOrigin())}

	_, err = inherit.Resolve(assembly.PageDim{})
	if !errors.Is(err, assembly.ErrUnresolvedSize) {
		t.Fatalf("explicit inherit: %v", err)
	}
}

func TestResolveNarrowPageKeepsOneBlockPoint(t *testing.T) {
	t.Parallel()

	var style assembly.BlankStyle

	spec, err := style.Resolve(assembly.PageDim{Width: 10, Height: 10})
	if err != nil || spec.Text.Width != 1 {
		t.Fatalf("width %v, %v; a page narrower than the margins keeps a one-point block", spec.Text.Width, err)
	}
}

func lengthField(value float64) assembly.Field[assembly.Length] {
	return assembly.Set(assembly.Length(value), itemOrigin())
}

func textOnly(set func(*assembly.TextStyle)) assembly.BlankStyle {
	var style assembly.BlankStyle

	set(&style.Text)

	return style
}

// invalidStyles are styles whose resolution must fail, by description.
func invalidStyles() map[string]assembly.BlankStyle {
	nan, inf, origin := math.NaN(), math.Inf(1), itemOrigin()

	return map[string]assembly.BlankStyle{
		"size 0":         textOnly(func(s *assembly.TextStyle) { s.Size = lengthField(0) }),
		"size 0.5":       textOnly(func(s *assembly.TextStyle) { s.Size = lengthField(0.5) }),
		"size NaN":       textOnly(func(s *assembly.TextStyle) { s.Size = lengthField(nan) }),
		"size Inf":       textOnly(func(s *assembly.TextStyle) { s.Size = lengthField(inf) }),
		"size huge":      textOnly(func(s *assembly.TextStyle) { s.Size = lengthField(maxLength + 1) }),
		"x NaN":          textOnly(func(s *assembly.TextStyle) { s.X = lengthField(nan) }),
		"x Inf":          textOnly(func(s *assembly.TextStyle) { s.X = lengthField(inf) }),
		"y -Inf":         textOnly(func(s *assembly.TextStyle) { s.Y = lengthField(-inf) }),
		"y huge":         textOnly(func(s *assembly.TextStyle) { s.Y = lengthField(-maxLength - 1) }),
		"width 0":        textOnly(func(s *assembly.TextStyle) { s.Width = lengthField(0) }),
		"width negative": textOnly(func(s *assembly.TextStyle) { s.Width = lengthField(-3) }),
		"width NaN":      textOnly(func(s *assembly.TextStyle) { s.Width = lengthField(nan) }),
		"width huge":     textOnly(func(s *assembly.TextStyle) { s.Width = lengthField(maxLength + 1) }),
		"leading 0.99":   textOnly(func(s *assembly.TextStyle) { s.Leading = assembly.Set(0.99, origin) }),
		"leading 10.01":  textOnly(func(s *assembly.TextStyle) { s.Leading = assembly.Set(10.01, origin) }),
		"leading NaN":    textOnly(func(s *assembly.TextStyle) { s.Leading = assembly.Set(nan, origin) }),
		"leading Inf":    textOnly(func(s *assembly.TextStyle) { s.Leading = assembly.Set(inf, origin) }),
		"text too long": textOnly(func(s *assembly.TextStyle) {
			s.Value = assembly.Set(strings.Repeat("a", assembly.MaxTextRunes+1), origin)
		}),
		"text invalid utf8": textOnly(func(s *assembly.TextStyle) { s.Value = assembly.Set("\xff", origin) }),
		"anchor 0":          textOnly(func(s *assembly.TextStyle) { s.Anchor = assembly.Set(assembly.Anchor(0), origin) }),
		"anchor 10":         textOnly(func(s *assembly.TextStyle) { s.Anchor = assembly.Set(assembly.Anchor(10), origin) }),
		"align 0":           textOnly(func(s *assembly.TextStyle) { s.Align = assembly.Set(assembly.TextAlign(0), origin) }),
		"align 5":           textOnly(func(s *assembly.TextStyle) { s.Align = assembly.Set(assembly.TextAlign(5), origin) }),
		"overflow 0":        textOnly(func(s *assembly.TextStyle) { s.Overflow = assembly.Set(assembly.Overflow(0), origin) }),
		"overflow 3":        textOnly(func(s *assembly.TextStyle) { s.Overflow = assembly.Set(assembly.Overflow(3), origin) }),
		"page NaN": {
			Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: assembly.Length(nan), Height: 10}}, origin),
		},
		"page zero": {Size: assembly.Set(assembly.PageSize{}, origin)},
		"page huge": {Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: maxLength + 1, Height: 10}}, origin)},
	}
}

func TestResolveRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for name, style := range invalidStyles() {
		spec, err := style.Resolve(a4Dim())
		if err == nil {
			t.Errorf("%s accepted: %+v", name, spec)
		}
	}
}

func TestResolveReportsEveryProblem(t *testing.T) {
	t.Parallel()

	origin := itemOrigin()
	style := assembly.BlankStyle{Text: assembly.TextStyle{
		Size: assembly.Set[assembly.Length](0, origin), X: assembly.Set(assembly.Length(math.NaN()), origin),
		Y: assembly.Set(assembly.Length(math.Inf(1)), origin), Leading: assembly.Set(99.0, origin),
	}}

	_, err := style.Resolve(a4Dim())
	if err == nil {
		t.Fatal("accepted")
	}

	for _, want := range []string{"text size", "text x", "text y", "leading"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestResolveAcceptsTheBoundsOfEveryValue(t *testing.T) {
	t.Parallel()

	origin := itemOrigin()

	for name, style := range map[string]assembly.BlankStyle{
		"smallest page": {Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: 1, Height: 1}}, origin)},
		"largest page":  {Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: maxLength, Height: maxLength}}, origin)},
		"extreme text": {Text: assembly.TextStyle{
			Size: assembly.Set[assembly.Length](maxLength, origin), X: assembly.Set[assembly.Length](-maxLength, origin),
			Y: assembly.Set[assembly.Length](maxLength, origin), Width: assembly.Set[assembly.Length](maxLength, origin),
			Leading: assembly.Set(10.0, origin), Value: assembly.Set(strings.Repeat("ā", assembly.MaxTextRunes), origin),
		}},
		"smallest text": {Text: assembly.TextStyle{
			Size: assembly.Set[assembly.Length](smallestPoint, origin), Width: assembly.Set[assembly.Length](smallestPoint, origin),
			Leading: assembly.Set(1.0, origin),
		}},
	} {
		_, err := style.Resolve(a4Dim())
		if err != nil {
			t.Errorf(namedFailureFormat, name, err)
		}
	}
}

func TestValidateTextValue(t *testing.T) {
	t.Parallel()

	if assembly.ValidateTextValue("") != nil || assembly.ValidateTextValue(strings.Repeat("ā", assembly.MaxTextRunes)) != nil {
		t.Error("empty and maximal text are valid")
	}

	for _, text := range []string{strings.Repeat("ā", assembly.MaxTextRunes+1), "a\xc0b"} {
		if !errors.Is(assembly.ValidateTextValue(text), assembly.ErrInvalidText) {
			t.Errorf("text %q accepted", text)
		}
	}
}
