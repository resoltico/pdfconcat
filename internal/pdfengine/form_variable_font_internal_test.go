// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestVariableEncodingDifferencesCoverInclusiveByteRange(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	for _, differences := range []types.Array{
		{types.Integer(0), types.Name("A"), types.Name("B")},
		{types.Integer(255), types.Name("Z")},
	} {
		encoding := simpleencodings.Encoding{}
		if err := variableEncodingDifferences(t.Context(), pdf, types.Dict{keyDifferences: differences}, &encoding); err != nil {
			t.Fatal(err)
		}
		if differences[0] == types.Integer(0) {
			if encoding[0] != "A" || encoding[1] != "B" {
				t.Fatalf("consecutive encoding assignments changed: %v", encoding[:2])
			}
		} else if encoding[255] != "Z" {
			t.Fatal("last byte code was not assigned")
		}
	}
	for _, differences := range []types.Array{
		{types.Integer(-1)},
		{types.Integer(256)},
		{types.Integer(255), types.Name("Z"), types.Name("A")},
	} {
		encoding := simpleencodings.Encoding{}
		if err := variableEncodingDifferences(t.Context(), pdf, types.Dict{keyDifferences: differences}, &encoding); !errors.Is(err, errFormState) {
			t.Fatalf("invalid byte assignment accepted: %v", err)
		}
	}
}

func TestVariableFontIntrinsicSymbolAndNameBoundaries(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	dict := variableFontDictionary()
	dict["ToUnicode"] = nil
	if _, err := compileVariableFont(t.Context(), pdf, dict); err != nil {
		t.Fatalf("null ToUnicode entry treated as a remapping: %v", err)
	}
	for _, base := range []string{nameSymbol, nameZapfDingbats} {
		encoding, err := variableBuiltinEncoding(t.Context(), pdf, types.Dict{keySubtype: types.Name(nameType1)}, base)
		if err != nil || encoding != variableCoreEncoding(base) {
			t.Fatalf("intrinsic %s encoding changed: %v", base, err)
		}
	}
	for source, want := range map[string]string{"ABCDEF+Helvetica": "Helvetica", "ABCDEF+": "ABCDEF+"} {
		name, err := variableFontName(t.Context(), pdf, types.Dict{keySubtype: types.Name("TrueType"), keyBaseFont: types.Name(source)})
		if err != nil || name != want {
			t.Fatalf("BaseFont %q = %q, want %q: %v", source, name, want, err)
		}
	}
	encoding := simpleencodings.Encoding{0: ".notdef", 1: "invalid-glyph", 2: "A"}
	codes := variableGlyphCodes(&encoding)
	if codes['A'] != 2 || len(codes) != 1 {
		t.Fatalf("unmapped glyph entries hid later valid code: %v", codes)
	}
}

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
		"missing-kind":        func(d types.Dict) { d.Delete(keySubtype) },
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
