// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func variableFontDictionary() types.Dict {
	return types.Dict{
		keySubtype: types.Name(type1FontSubtype), keyBaseFont: types.Name(fontHelvetica),
		keyEncoding: types.Name(nameWinAnsiEncoding),
	}
}

func TestVariableFontUsesActualEncodingAndDeclaredWidths(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()

	for _, encoding := range []string{nameWinAnsiEncoding, nameMacRomanEncoding} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			dict := variableFontDictionary()
			dict[keyEncoding] = types.Name(encoding)

			font, err := compileVariableFont(t.Context(), fixture.pdf, dict)
			if err != nil {
				t.Fatal(err)
			}

			line, err := font.encode(t.Context(), "é")
			if err != nil || line.width != .556 {
				t.Fatalf("actual glyph width %v: %v", line, err)
			}

			wanted := byte(233)
			if encoding == nameMacRomanEncoding {
				wanted = 142
			}

			if len(line.data) != 1 || line.data[0] != wanted {
				t.Fatalf("wrong encoded glyph %x; want %x", line.data, wanted)
			}
		})
	}
}

func TestVariableFontDeclaredAdvancesOverrideCoreMetrics(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	dict := variableFontDictionary()
	dict["Widths"], dict["FirstChar"] = types.NewNumberArray(1500), types.Integer(65)
	dict["FontDescriptor"] = types.Dict{"MissingWidth": types.Integer(700)}

	font, err := compileVariableFont(t.Context(), fixture.pdf, dict)
	if err != nil {
		t.Fatal(err)
	}

	line, err := font.encode(t.Context(), "AB")
	if err != nil || line.width != 2.2 || string(line.data) != "AB" {
		t.Fatalf("declared advances replaced: %+v, %v", line, err)
	}
}

func TestVariableFontDifferencesPreserveActualGlyphAvailability(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	dict := variableFontDictionary()
	dict[keyEncoding] = types.Dict{
		keyBaseEncoding: types.Name(nameWinAnsiEncoding), keyDifferences: types.Array{types.Integer(65), types.Name("W")},
	}

	font, err := compileVariableFont(t.Context(), fixture.pdf, dict)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = font.encode(t.Context(), "A"); !errors.Is(err, errFormState) {
		t.Fatalf("unavailable glyph silently substituted: %v", err)
	}

	line, err := font.encode(t.Context(), "W")
	if err != nil || string(line.data) != "A" || line.width != .944 {
		t.Fatalf("actual encoding binding lost: %+v, %v", line, err)
	}
}

func TestVariableFontDuplicateEncodingNamesUseCanonicalCoreMetrics(t *testing.T) {
	t.Parallel()

	font, err := compileVariableFont(t.Context(), formTestContext().pdf, variableFontDictionary())
	if err != nil {
		t.Fatal(err)
	}

	line, err := font.encode(t.Context(), " -\u00a0\u00ad•")
	if err != nil {
		t.Fatal(err)
	}

	if line.width != 1.572 {
		t.Fatalf("duplicate glyph names changed spacing: %v; want 1.572", line.width)
	}
}

func TestVariableFontRejectsMalformedMetadataAndUnavailableGlyphs(t *testing.T) {
	t.Parallel()

	cases := map[string]func(types.Dict){
		"kind":                func(d types.Dict) { d[keySubtype] = types.Name("Type0") },
		"base":                func(d types.Dict) { d.Delete(keyBaseFont) },
		"encoding":            func(d types.Dict) { d[keyEncoding] = types.Integer(2) },
		"unknown encoding":    func(d types.Dict) { d[keyEncoding] = types.Name("UnknownEncoding") },
		"bad difference code": func(d types.Dict) { d[keyEncoding] = types.Dict{keyDifferences: types.Array{types.Integer(256)}} },
		"unpositioned glyph":  func(d types.Dict) { d[keyEncoding] = types.Dict{keyDifferences: types.Array{types.Name("W")}} },
		"bad difference":      func(d types.Dict) { d[keyEncoding] = types.Dict{keyDifferences: types.Array{types.Boolean(true)}} },
		"no widths":           func(d types.Dict) { d[keyBaseFont] = types.Name("NonCoreFont") },
		"CMap":                func(d types.Dict) { d["ToUnicode"] = types.StreamDict{} },
		"empty widths":        func(d types.Dict) { d["Widths"] = types.Array{} },
		"range":               func(d types.Dict) { d["FirstChar"], d["Widths"] = types.Integer(math.MaxInt), types.NewNumberArray(1) },
		"negative width":      func(d types.Dict) { d["FirstChar"], d["Widths"] = types.Integer(65), types.NewNumberArray(-1) },
		"nonfinite width":     func(d types.Dict) { d["FirstChar"], d["Widths"] = types.Integer(65), types.NewNumberArray(math.Inf(1)) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dict := variableFontDictionary()
			change(dict)

			if _, err := compileVariableFont(t.Context(), formTestContext().pdf, dict); !errors.Is(err, errFormState) {
				t.Fatalf("unsupported metadata accepted: %v", err)
			}
		})
	}

	font, err := compileVariableFont(t.Context(), formTestContext().pdf, variableFontDictionary())
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"☃", string([]byte{255})} {
		if _, err = font.encode(t.Context(), text); !errors.Is(err, errFormState) {
			t.Fatalf("unsupported value %q: %v", text, err)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err = compileVariableFont(ctx, formTestContext().pdf, variableFontDictionary()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if _, err = font.encode(ctx, "A"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
