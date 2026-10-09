// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestVariableFontEncodingAndMetricBoundaries(t *testing.T) {
	t.Parallel()

	cases := map[string]func(types.Dict){
		"encoding decode":   func(d types.Dict) { d[keyEncoding] = *types.NewIndirectRef(9, 0) },
		"base encoding":     func(d types.Dict) { d[keyEncoding] = types.Dict{keyBaseEncoding: types.Integer(1)} },
		"unknown base":      func(d types.Dict) { d[keyEncoding] = types.Dict{keyBaseEncoding: types.Name("OtherEncoding")} },
		"differences array": func(d types.Dict) { d[keyEncoding] = types.Dict{keyDifferences: types.Integer(1)} },
		"glyph decode": func(d types.Dict) {
			d[keyEncoding] = types.Dict{keyDifferences: types.Array{types.Integer(65), *types.NewIndirectRef(9, 0)}}
		},
		"builtin TrueType":   func(d types.Dict) { d.Delete(keyEncoding); d[keySubtype] = types.Name("TrueType") },
		"builtin descriptor": func(d types.Dict) { d.Delete(keyEncoding); d["FontDescriptor"] = types.Integer(1) },
		"builtin program": func(d types.Dict) {
			d.Delete(keyEncoding)
			d["FontDescriptor"] = types.Dict{"FontFile": types.StreamDict{}}
		},
		"metric descriptor": func(d types.Dict) { d["FontDescriptor"] = types.Integer(1) },
		"metric program":    func(d types.Dict) { d["FontDescriptor"] = types.Dict{"FontFile2": types.StreamDict{}} },
		"missing descriptor": func(d types.Dict) {
			d["FirstChar"], d["Widths"], d["FontDescriptor"] = types.Integer(65), types.NewNumberArray(100), types.Integer(1)
		},
		"invalid missing": func(d types.Dict) {
			d["FirstChar"], d["Widths"] = types.Integer(65), types.NewNumberArray(100)
			d["FontDescriptor"] = types.Dict{"MissingWidth": types.Float(math.NaN())}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := formTestContext()
			stream := &types.ObjectStreamDict{Dict: types.Dict{}, Content: []byte("x"), MaxDecodeBytes: 1024}
			lazy := types.NewLazyObjectStreamObject(stream, 2, 3, nil)
			fixture.pdf.Table[9] = model.NewXRefTableEntryGen0(lazy)
			dict := variableFontDictionary()
			change(dict)

			if _, err := compileVariableFont(t.Context(), fixture.pdf, dict); err == nil {
				t.Fatal("unsupported source font metadata accepted")
			}
		})
	}
}

func TestVariableFontPredefinedAndUnknownGlyphCases(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"MacExpertEncoding", "StandardEncoding"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dict := variableFontDictionary()

			dict[keyEncoding] = types.Name(name)
			if _, err := compileVariableFont(t.Context(), formTestContext().pdf, dict); err != nil {
				t.Fatal(err)
			}
		})
	}

	for _, base := range []string{nameSymbol, nameZapfDingbats, "ABCDEF+Helvetica"} {
		dict := variableFontDictionary()
		dict.Delete(keyEncoding)

		dict[keyBaseFont] = types.Name(base)
		if _, err := compileVariableFont(t.Context(), formTestContext().pdf, dict); err != nil {
			t.Fatal(err)
		}
	}

	dict := variableFontDictionary()

	dict[keyEncoding] = types.Dict{keyDifferences: types.Array{types.Integer(65), types.Name("UndefinedGlyph")}}
	if _, err := compileVariableFont(t.Context(), formTestContext().pdf, dict); err != nil {
		t.Fatal(err)
	}

	dict[keyEncoding] = types.Name(nameMacRomanEncoding)

	font, err := compileVariableFont(t.Context(), formTestContext().pdf, dict)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = font.encode(t.Context(), ""); !errors.Is(err, errFormState) {
		t.Fatalf("undeclared core metric guessed: %v", err)
	}
}

func TestVariableFontRejectsUnknownDictionaryAndAdvanceOverflow(t *testing.T) {
	t.Parallel()

	for _, object := range []types.Object{types.Integer(1), types.Dict{}} {
		if _, err := compileVariableFont(t.Context(), formTestContext().pdf, object); !errors.Is(err, errFormState) {
			t.Fatal(err)
		}
	}

	dict := variableFontDictionary()
	dict["FirstChar"], dict["Widths"] = types.Integer(65), types.NewNumberArray(1e308)

	font, err := compileVariableFont(t.Context(), formTestContext().pdf, dict)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = font.encode(t.Context(), strings.Repeat("A", 10000)); !errors.Is(err, errFormState) {
		t.Fatalf("unbounded advance accepted: %v", err)
	}

	ctx := newFormCheckpointContext(t.Context(), t)
	ctx.remaining.Store(1)

	if _, err = compileVariableFont(ctx, formTestContext().pdf, variableFontDictionary()); !errors.Is(err, context.Canceled) {
		t.Fatalf("metric loading ignored cancellation: %v", err)
	}
}
