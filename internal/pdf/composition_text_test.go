// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestType3WidthIntervalDefaultAndMalformedMetrics(t *testing.T) {
	ctx, err := model.NewContext(bytes.NewReader(nil), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		code   int
		change func(types.Dict)
		want   float64
		known  bool
	}{
		{"below", 64, nil, 0, true}, {"above", 66, nil, 0, true}, {"inside", 65, nil, 600, true},
		{"rotated-horizontal-component", 65, func(d types.Dict) {
			d["FontMatrix"] = types.Array{types.Float(.5), types.Integer(1), types.Integer(-1), types.Float(.5), types.Integer(0), types.Integer(0)}
		}, 300, true},
		{"missing-first", 66, func(d types.Dict) { delete(d, "FirstChar") }, 0, false},
		{"missing-last", 66, func(d types.Dict) { delete(d, "LastChar") }, 0, false},
		{"bad-first", 66, func(d types.Dict) { d["FirstChar"] = types.Name("bad") }, 0, false},
		{"bad-last", 66, func(d types.Dict) { d["LastChar"] = types.Name("bad") }, 0, false},
		{"reversed", 66, func(d types.Dict) { d["LastChar"] = types.Integer(64) }, 0, false},
		{"missing-widths", 66, func(d types.Dict) { delete(d, "Widths") }, 0, false},
		{"bad-widths", 66, func(d types.Dict) { d["Widths"] = types.Name("bad") }, 0, false},
		{"missing-in-range", 65, func(d types.Dict) { d["LastChar"] = types.Integer(66) }, 0, false},
		{"mismatch-outside", 67, func(d types.Dict) { d["LastChar"] = types.Integer(66) }, 0, false},
		{"extra-width", 66, func(d types.Dict) { d["Widths"] = types.Array{types.Integer(600), types.Integer(0)} }, 0, false},
		{"null-width", 65, func(d types.Dict) { d["Widths"] = types.Array{nil} }, 0, false},
		{"invalid-width", 65, func(d types.Dict) { d["Widths"] = types.Array{types.Name("bad")} }, 0, false},
		{"invalid-unused-width", 66, func(d types.Dict) { d["Widths"] = types.Array{types.Name("bad")} }, 0, false},
		{"nonfinite-width", 65, func(d types.Dict) { d["Widths"] = types.Array{types.Float(math.Inf(1))} }, 0, false},
		{"nonfinite-unused-width", 66, func(d types.Dict) { d["Widths"] = types.Array{types.Float(math.NaN())} }, 0, false},
		{"missing-matrix", 66, func(d types.Dict) { delete(d, "FontMatrix") }, 0, false},
		{"nonfinite-matrix", 66, func(d types.Dict) {
			d["FontMatrix"] = types.Array{types.Float(math.Inf(1)), types.Integer(0), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(0)}
		}, 0, false},
		{"overflow", 65, func(d types.Dict) {
			d["Widths"] = types.Array{types.Float(math.MaxFloat64)}
			d["FontMatrix"] = types.Array{types.Integer(2), types.Integer(0), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(0)}
		}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			font := types.Dict{"FirstChar": types.Integer(65), "LastChar": types.Integer(65), "Widths": types.Array{types.Integer(600)}, "FontMatrix": types.Array{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(0)}}
			if tc.change != nil {
				tc.change(font)
			}
			a := compositionInspector{pdf: ctx, context: context.Background(), remaining: 10000}
			got, known, err := a.type3Advance(font, tc.code)
			if tc.known {
				if err != nil || !known || got != tc.want {
					t.Fatal(got, known, err)
				}
			} else if err == nil && known {
				t.Fatal("malformed metrics became known", got)
			}
		})
	}
}

func TestType3MetricsWorkRemainsBoundedAndCancellable(t *testing.T) {
	pdf, err := model.NewContext(bytes.NewReader(nil), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	font := types.Dict{"FirstChar": types.Integer(65), "LastChar": types.Integer(65), "Widths": types.Array{types.Integer(600)}, "FontMatrix": types.Array{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(1), types.Integer(0), types.Integer(0)}}
	a := compositionInspector{pdf: pdf, context: context.Background(), remaining: 0}
	_, known, err := a.type3Advance(font, 66)
	if known || err != nil || a.remaining >= 0 {
		t.Fatal("metrics escaped work budget", known, err, a.remaining)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a = compositionInspector{pdf: pdf, context: ctx, remaining: 100}
	_, known, err = a.type3Advance(font, 66)
	if known || !errors.Is(err, context.Canceled) {
		t.Fatal("metrics ignored cancellation", known, err)
	}
}
