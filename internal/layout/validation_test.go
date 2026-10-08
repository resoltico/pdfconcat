// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package layout_test

import (
	"context"
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestStaticTextValidationAndSingleSpecLocateEffectiveDeclarations(t *testing.T) {
	t.Parallel()
	font := loadFont(t)
	origin := assembly.Origin{Ref: 7}
	style := assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Set("שלום", origin)}}
	flat := &assembly.Flattened{
		Source: assembly.ArgumentSource{},
		Contributions: []assembly.Contribution{
			{Kind: assembly.ItemBlank, Origin: assembly.Origin{Ref: 9}, Style: 0, TextOverrides: &style.Text},
			{Kind: assembly.ItemBlank, Origin: assembly.Origin{Ref: 10}, Style: 1, TextOverrides: &style.Text},
		},
		Styles: []assembly.LayeredStyle{
			{Style: style, FirstUse: assembly.Origin{Ref: 9}},
			{Style: style, FirstUse: assembly.Origin{Ref: 10}},
		},
	}
	err := layout.ValidateText(t.Context(), flat, builtIn(font))

	problems := assembly.Diagnostics(err)
	if len(problems) != 1 || problems[0].Location.Pointer != unsupportedTextDeclarationPointer {
		t.Fatalf("declaration and cached static failures: %v", err)
	}

	spec := blankSpec("שלום", 300, 200)
	usedSpec := used(&spec, 9)
	usedSpec.Declared = style.Text

	var shaper typeset.Shaper

	placed, err := layout.PlaceSpec(&shaper, layoutOf(usedSpec), 0, builtIn(font))

	diagnostics := assembly.Diagnostics(err)
	if placed != nil || len(diagnostics) != 1 {
		t.Fatalf("placement lost declaration: %v", err)
	}

	if diagnostics[0].Location.Pointer != unsupportedTextDeclarationPointer {
		t.Fatalf("placement lost declaration: %v", err)
	}
}

func TestStaticTextAndProducerRequireLoadedFontAndCancellation(t *testing.T) {
	t.Parallel()
	font := loadFont(t)

	flat := &assembly.Flattened{
		Source:        assembly.ArgumentSource{},
		Contributions: []assembly.Contribution{{Kind: assembly.ItemBlank, Origin: assembly.Origin{Ref: 1}, Style: 0}},
		Styles: []assembly.LayeredStyle{
			{
				FirstUse: assembly.Origin{Ref: 1},
				Style:    assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Set("supported", assembly.Origin{})}},
			},
		},
	}
	if err := layout.ValidateText(t.Context(), flat, builtIn(font)); err != nil {
		t.Fatal(err)
	}

	missing := func(string) (*typeset.Font, bool) { return nil, false }

	diagnostics := assembly.Diagnostics(layout.ValidateText(t.Context(), flat, missing))
	if len(diagnostics) != 1 {
		t.Fatalf("missing font validation: %+v", diagnostics)
	}

	if diagnostics[0].Code != assembly.CodeFontUnavailable {
		t.Fatalf("missing font validation: %+v", diagnostics)
	}

	spec := blankSpec("supported", 300, 200)

	var shaper typeset.Shaper
	if _, err := layout.PlaceSpec(&shaper, layoutOf(used(&spec, 1)), 0, missing); err == nil {
		t.Fatal("producer accepted missing font")
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	if err := layout.ValidateText(canceled, flat, builtIn(font)); !errors.Is(err, context.Canceled) {
		t.Fatalf("static validation cancellation: %v", err)
	}
}
