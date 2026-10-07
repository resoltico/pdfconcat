// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package layout_test

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// resolveTextDeclaration exercises the real declaration layer, deduplication and resolution.
func resolveTextDeclaration(t *testing.T, text *assembly.TextStyle, font *typeset.Font) *assembly.Layout {
	t.Helper()

	style := assembly.BlankStyle{
		Text: *text,
		Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: 100, Height: 100}}, assembly.Origin{Ref: 1}),
	}

	item, err := assembly.NewBlankItem(assembly.Origin{Ref: 9}, &style, 1)
	if err != nil {
		t.Fatal(err)
	}

	flat, err := assembly.Flatten(&assembly.Job{Source: assembly.ArgumentSource{}, Base: t.TempDir(), Items: []assembly.Item{item}})
	if err != nil {
		t.Fatal(err)
	}

	table, err := flat.Resolve(nil, func(string) (assembly.FontDigest, bool) { return assembly.FontDigest(font.Identity()), true })
	if err != nil {
		t.Fatal(err)
	}

	return table
}

func TestPlacementFaultsLocateGeometryDeclarationsAndDeduplicateConsumers(t *testing.T) {
	t.Parallel()
	font := loadFont(t)
	origin := assembly.Origin{Ref: 7}

	cases := []struct {
		name string
		text assembly.TextStyle
	}{
		{
			"vertical offset",
			assembly.TextStyle{Value: assembly.Set("A", assembly.Origin{Ref: 2}), Y: assembly.Set(assembly.Length(1000), origin)},
		},
		{
			"anchor only",
			assembly.TextStyle{
				Value:  assembly.Set(strings.Repeat("A\n", 30), assembly.Origin{Ref: 2}),
				Anchor: assembly.Set(assembly.AnchorTop, origin),
			},
		},
		{
			"oversized type",
			assembly.TextStyle{Value: assembly.Set("A", assembly.Origin{Ref: 2}), Size: assembly.Set(assembly.Length(200), origin)},
		},
		{
			"one declaration implicated twice",
			assembly.TextStyle{
				Value: assembly.Set("WWWWWWWWWWWWWWWWWWWW", origin),
				Width: assembly.Set(assembly.Length(150), origin),
				X:     assembly.Set(assembly.Length(1000), origin),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			table := resolveTextDeclaration(t, &tc.text, font)

			var shaper typeset.Shaper

			placed, err := layout.PlaceSpec(&shaper, table, 0, builtIn(font))

			found := assembly.Diagnostics(err)
			if placed == nil || len(found) != 1 || found[0].Location.Pointer != unsupportedTextDeclarationPointer ||
				len(found[0].Consumers) != 1 ||
				found[0].Affected != 1 {
				t.Fatalf("geometry declaration/consumer truth: placed=%v diagnostics=%+v", placed, found)
			}
		})
	}
}

// damagedOutlineFont changes a real font's used glyph, while retaining its valid table directory.
func damagedOutlineFont(t *testing.T) *typeset.Font {
	t.Helper()
	font := loadFont(t)

	var shaper typeset.Shaper

	placed, err := shaper.Place(
		font,
		layout.Params(
			&assembly.BlankSpec{
				Dim: assembly.PageDim{Width: 100, Height: 100},
				Text: assembly.TextSpec{
					Value:   "A",
					Size:    12,
					Width:   100,
					Leading: 1.2,
					Anchor:  assembly.AnchorCenter,
					Align:   assembly.AlignLeft,
				},
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	glyph := placed.Lines[0].Clusters[0].Glyphs[0].ID
	data := slices.Clone(font.Bytes())
	tables := map[string][]byte{}

	const header, record, offsetField, lengthField, locaFormat = 12, 16, 8, 12, 50
	for i := range int(binary.BigEndian.Uint16(data[4:])) {
		r := data[header+i*record:]
		offset := binary.BigEndian.Uint32(r[offsetField:])
		length := binary.BigEndian.Uint32(r[lengthField:])
		tables[string(r[:4])] = data[offset : offset+length]
	}

	if binary.BigEndian.Uint16(tables["head"][locaFormat:]) != 1 {
		t.Fatal("fixture requires long loca")
	}

	start := binary.BigEndian.Uint32(tables["loca"][4*int(glyph):])
	binary.BigEndian.PutUint16(tables["glyf"][start:], 0x7fff)

	damaged, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}

	return damaged
}

func TestStaticAndLateOutlineFailuresLocateTheFontDeclaration(t *testing.T) {
	t.Parallel()
	font := damagedOutlineFont(t)
	declaration := assembly.TextStyle{
		Value: assembly.Set("A", assembly.Origin{Ref: 2}),
		Font:  assembly.Set(assembly.Font{}, assembly.Origin{Ref: 7}),
	}
	table := resolveTextDeclaration(t, &declaration, font)
	staticErr := layout.ValidateText(t.Context(), table.Flat, builtIn(font))

	var shaper typeset.Shaper

	_, err := layout.PlaceSpec(&shaper, table, 0, builtIn(font))

	faults := []error{staticErr, err}
	for _, fault := range faults {
		found := assembly.Diagnostics(fault)
		if len(found) != 1 || found[0].Code != layout.CodeFontInvalid || found[0].Location.Pointer != unsupportedTextDeclarationPointer {
			t.Fatalf("outline font declaration: %+v", found)
		}
	}
	// Standalone resolved-spec callers also retain their declaration metadata.
	table.Flat = nil

	_, err = layout.PlaceSpec(&shaper, table, 0, builtIn(font))
	if found := assembly.Diagnostics(err); len(found) != 1 || found[0].Location.Pointer != unsupportedTextDeclarationPointer {
		t.Fatalf("standalone font declaration: %+v", found)
	}
}
