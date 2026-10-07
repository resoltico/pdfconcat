// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestGeometryAdaptersDistinguishUnavailableFromKnownEmptyInk(t *testing.T) {
	t.Parallel()

	current := &pipeline{
		placed: []*typeset.Placed{
			nil,
			{Bounds: typeset.Rect{X: 2, Y: 3, Width: 4, Height: 5}},
			{Bounds: typeset.Rect{X: 6, Y: 7, Width: 8, Height: 9}, InkBounds: &typeset.Rect{X: -1, Y: -2, Width: 3, Height: 4}},
		},
	}
	for _, index := range []int{0, 3} {
		if current.boundsOf(index) != nil || current.inkBoundsOf(index) != nil {
			t.Fatalf("unknown geometry at %d became known", index)
		}
	}

	if got := current.boundsOf(1); got == nil || *got != (report.Rect{X: 2, Y: 3, Width: 4, Height: 5}) {
		t.Fatalf("logical whitespace block geometry: %+v", got)
	}

	if current.inkBoundsOf(1) != nil {
		t.Fatal("no outlined ink became known ink")
	}

	assertKnownGeometryCopied(t, current)
}

func assertKnownGeometryCopied(t *testing.T, current *pipeline) {
	t.Helper()

	bounds, ink := current.boundsOf(2), current.inkBoundsOf(2)
	if bounds == nil || *bounds != (report.Rect{X: 6, Y: 7, Width: 8, Height: 9}) || ink == nil ||
		*ink != (report.Rect{X: -1, Y: -2, Width: 3, Height: 4}) {
		t.Fatalf("computed geometry changed: bounds=%+v ink=%+v", bounds, ink)
	}

	bounds.X, ink.Width = 1000, 1000
	if current.placed[2].Bounds.X != 6 || current.placed[2].InkBounds.Width != 3 {
		t.Fatal("report geometry exposes mutable placement storage")
	}
}

func TestFontDeclarationOriginRetainsBuiltinInheritanceAndExplicitOverrides(t *testing.T) {
	t.Parallel()

	root, item := assembly.Origin{Ref: 7}, assembly.Origin{Ref: 9}

	cases := []struct {
		name          string
		defaults, own assembly.TextStyle
		want          assembly.Origin
	}{
		{name: "builtin inherited", want: item},
		{name: "explicit default builtin", defaults: assembly.TextStyle{Font: assembly.Set(assembly.Font{}, root)}, want: root},
		{
			name:     "explicit item builtin",
			defaults: assembly.TextStyle{Font: assembly.Set(assembly.Font{}, root)},
			own:      assembly.TextStyle{Font: assembly.Set(assembly.Font{}, item)},
			want:     item,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			style := assembly.BlankStyle{Text: tc.own}

			blank, err := assembly.NewBlankItem(item, &style, 1)
			if err != nil {
				t.Fatal(err)
			}

			flat, err := assembly.Flatten(
				&assembly.Job{
					Source:   assembly.ArgumentSource{},
					Base:     t.TempDir(),
					Defaults: assembly.BlankStyle{Text: tc.defaults},
					Items:    []assembly.Item{blank},
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			if len(flat.Fonts) != 0 {
				t.Fatal("builtin declaration unexpectedly captured a file font")
			}

			if got := fontDeclarationOrigin(flat, &flat.Contributions[0]); got != tc.want {
				t.Fatalf("font declaration origin=%+v want %+v", got, tc.want)
			}
		})
	}
}
