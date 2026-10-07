// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package layout_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

const unsupportedTextDeclarationPointer = "argv:7"

func loadFont(tb testing.TB) *typeset.Font {
	tb.Helper()

	font, err := typeset.LoadDefaultFont()
	if err != nil {
		tb.Fatal(err)
	}

	return font
}

func builtIn(font *typeset.Font) layout.FontSet {
	return func(path string) (*typeset.Font, bool) { return font, path == "" }
}

func blankSpec(text string, width, height assembly.Length) assembly.BlankSpec {
	return assembly.BlankSpec{
		Dim: assembly.PageDim{Width: width, Height: height},
		Text: assembly.TextSpec{
			Value: text, Size: 12, Width: width - 72, Leading: 1.2, Anchor: assembly.AnchorCenter,
			Align: assembly.AlignCenter, Overflow: assembly.OverflowError,
		},
	}
}

// layoutOf builds a Layout whose specs are used by the given origins (one contribution each).
func layoutOf(specs ...assembly.ResolvedSpec) *assembly.Layout {
	return &assembly.Layout{Source: assembly.ArgumentSource{}, Specs: specs}
}

func used(spec *assembly.BlankSpec, refs ...assembly.Ref) assembly.ResolvedSpec {
	origins := make([]assembly.Origin, len(refs))
	for index, ref := range refs {
		origins[index] = assembly.Origin{Ref: ref}
	}

	return assembly.ResolvedSpec{Spec: *spec, Origins: origins, Uses: int64(len(refs))}
}

func TestParamsMapsEveryField(t *testing.T) {
	t.Parallel()

	// Independent expectations: the nine anchors in grid order and the four alignments.
	anchors := map[assembly.Anchor]typeset.Anchor{
		assembly.AnchorTopLeft: typeset.AnchorTopLeft, assembly.AnchorTop: typeset.AnchorTopCenter,
		assembly.AnchorTopRight: typeset.AnchorTopRight, assembly.AnchorLeft: typeset.AnchorMiddleLeft,
		assembly.AnchorCenter: typeset.AnchorCenter, assembly.AnchorRight: typeset.AnchorMiddleRight,
		assembly.AnchorBottomLeft: typeset.AnchorBottomLeft, assembly.AnchorBottom: typeset.AnchorBottomCenter,
		assembly.AnchorBottomRight: typeset.AnchorBottomRight,
	}

	aligns := map[assembly.TextAlign]typeset.Align{
		assembly.AlignLeft: typeset.AlignLeft, assembly.AlignCenter: typeset.AlignCenter,
		assembly.AlignRight: typeset.AlignRight, assembly.AlignJustify: typeset.AlignJustify,
	}

	if len(anchors) != 9 || len(aligns) != 4 {
		t.Fatal("tables must cover every value")
	}

	for anchor, want := range anchors {
		spec := blankSpec("x", 300, 200)
		spec.Text.Anchor = anchor

		if got := layout.Params(&spec).Anchor; got != want {
			t.Errorf("anchor %v -> %v, want %v", anchor, got, want)
		}
	}

	for align, want := range aligns {
		spec := blankSpec("x", 300, 200)
		spec.Text.Align = align

		if got := layout.Params(&spec).Align; got != want {
			t.Errorf("align %v -> %v, want %v", align, got, want)
		}
	}

	spec := blankSpec("héllo", 300, 200)
	spec.Text.Size, spec.Text.X, spec.Text.Y, spec.Text.Width, spec.Text.Leading = 14, -5, 7, 120, 1.5
	spec.Text.Overflow = assembly.OverflowAllow

	want := typeset.Params{
		Text: "héllo", Size: 14, PageWidth: 300, PageHeight: 200, Anchor: typeset.AnchorCenter, OffsetX: -5, OffsetY: 7,
		WrapWidth: 120, Align: typeset.AlignCenter, LineSpacing: 1.5, Overflow: typeset.OverflowAllow,
	}

	if got := layout.Params(&spec); got != want {
		t.Errorf("Params = %+v, want %+v", got, want)
	}

	strict := blankSpec("", 100, 100)
	if strict.Text.Overflow != assembly.OverflowError || layout.Params(&strict).Overflow != typeset.OverflowReject {
		t.Error("overflow error must map to rejection")
	}
}

func TestPlaceEveryDistinctSpecOnceAndReportsAllProblems(t *testing.T) {
	t.Parallel()

	fine := blankSpec("Sveiki, pasaule", 300, 200)
	empty := blankSpec("", 300, 200)
	wide := blankSpec(strings.Repeat("W", 60), 300, 200) // one unbreakable word wider than the box
	allowed := wide
	allowed.Text.Overflow = assembly.OverflowAllow

	arabic := blankSpec("مرحبا", 300, 200)
	tall := blankSpec(strings.Repeat("line\n", 40), 300, 200)

	table := layoutOf(
		used(&fine, 1), used(&wide, 2, 3, 4), used(&empty, 5), used(&allowed, 6), used(&arabic, 7), used(&tall, 8),
	)

	placed, err := layout.Place(context.Background(), table, builtIn(loadFont(t)))

	list := assembly.Diagnostics(err)
	if len(list) != 3 {
		t.Fatalf("want every failing spec reported: %v", err)
	}

	checkReportedProblems(t, list)

	overflow, ok := errors.AsType[*typeset.OverflowError](err)
	if !ok || len(overflow.Findings) == 0 {
		t.Error("typeset cause must stay available")
	}

	checkPlacements(t, placed)
}

// checkPlacements checks the placements of the table of TestPlaceEveryDistinctSpecOnceAndReportsAllProblems.
func checkPlacements(t *testing.T, placed []*typeset.Placed) {
	t.Helper()

	if placed[0] == nil || len(placed[0].Lines) == 0 || placed[2] == nil || len(placed[2].Lines) != 0 {
		t.Errorf("successful specs have placements: %+v %+v", placed[0], placed[2])
	}

	if placed[3] == nil || len(placed[3].Findings) == 0 {
		t.Errorf("allowed overflow stays visible as findings: %+v", placed[3])
	}

	if placed[1] == nil || placed[4] != nil || placed[5] == nil {
		t.Error("overflow preserves geometry; unsupported text has no placement")
	}
}

// checkReportedProblems checks the three diagnostics of the table of TestPlaceEveryDistinctSpecOnceAndReportsAllProblems.
func checkReportedProblems(t *testing.T, list []*assembly.Error) {
	t.Helper()

	wantCodes := []assembly.Code{layout.CodeTextOverflow, layout.CodeTextUnsupported, layout.CodeTextOverflow}
	wantPointers := []string{"argv:2", unsupportedTextDeclarationPointer, "argv:8"}

	for index, found := range list {
		if found.Code != wantCodes[index] || found.Location.Pointer != wantPointers[index] || found.Stage != layout.StageLayout {
			t.Errorf("diagnostic %d = %+v", index, found)
		}
	}

	if list[0].Affected != 3 || len(list[0].Related) != 2 || list[0].Related[1].Pointer != "argv:4" {
		t.Errorf("origins and count of the shared spec: %+v", list[0])
	}

	if !strings.Contains(list[0].Message, `overflow "allow"`) {
		t.Errorf("the message must say how to repair: %q", list[0].Message)
	}
}

func TestPlaceSucceedsWithoutProblems(t *testing.T) {
	t.Parallel()

	spec := blankSpec("Ā ļ", 300, 200)

	placed, err := layout.Place(context.Background(), layoutOf(used(&spec, 1)), builtIn(loadFont(t)))
	if err != nil || len(placed) != 1 || placed[0].Bounds.Width <= 0 {
		t.Errorf("%v %+v", err, placed)
	}

	placed, err = layout.Place(context.Background(), layoutOf(), builtIn(loadFont(t)))
	if err != nil || len(placed) != 0 {
		t.Errorf("no specs: %v %v", err, placed)
	}
}

func TestPlaceWithoutKeepingLinesKeepsBounds(t *testing.T) {
	t.Parallel()

	spec := blankSpec("Ā ļ", 300, 200)
	table := layoutOf(used(&spec, 1))

	kept, err := layout.Place(context.Background(), table, builtIn(loadFont(t)))
	if err != nil {
		t.Fatal(err)
	}

	dropped, err := layout.PlaceBounds(context.Background(), table, builtIn(loadFont(t)))
	if err != nil {
		t.Fatal(err)
	}

	if len(kept[0].Lines) == 0 || len(dropped[0].Lines) != 0 || dropped[0].Bounds != kept[0].Bounds {
		t.Errorf("kept %d lines, dropped %d lines, bounds %v vs %v",
			len(kept[0].Lines), len(dropped[0].Lines), kept[0].Bounds, dropped[0].Bounds)
	}
}

func TestPlaceReportsMissingFontAndBadParameters(t *testing.T) {
	t.Parallel()

	spec := blankSpec("x", 300, 200)
	file := spec
	file.Text.Font = assembly.Font{File: "/fonts/missing.ttf"}

	tiny := blankSpec("x", 300, 200)
	tiny.Text.Size = 0.001 // below what the typesetter supports

	_, err := layout.Place(context.Background(), layoutOf(used(&file, 1), used(&tiny, 2)), builtIn(loadFont(t)))

	got := assembly.Diagnostics(err)
	if len(got) != 2 || got[0].Code != assembly.CodeFontUnavailable || got[1].Code != layout.CodeTextParameter {
		t.Fatalf("%v", err)
	}

	if !strings.Contains(got[0].Message, "missing.ttf") {
		t.Errorf("%q", got[0].Message)
	}
}

func TestPlaceHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	spec := blankSpec("x", 300, 200)

	placed, err := layout.Place(ctx, layoutOf(used(&spec, 1)), builtIn(loadFont(t)))
	if !errors.Is(err, context.Canceled) || placed != nil {
		t.Errorf("%v %v", err, placed)
	}
}

func TestCodeFor(t *testing.T) {
	t.Parallel()

	rows := []struct {
		err  error
		want assembly.Code
	}{
		{&typeset.OverflowError{}, layout.CodeTextOverflow},
		{&typeset.TextError{}, layout.CodeTextUnsupported},
		{&typeset.InvalidParamError{}, layout.CodeTextParameter},
		{&typeset.OutlineError{}, layout.CodeFontInvalid},
		{fmt.Errorf("%w: truncated header", typeset.ErrMalformedFont), layout.CodeFontInvalid},
		{typeset.ErrShapingFailed, layout.CodeTextFailed},
	}

	for _, row := range rows {
		got := layout.CodeFor(row.err)
		if got != row.want {
			t.Errorf("CodeFor(%v) = %s, want %s", row.err, got, row.want)
		}
	}
}
