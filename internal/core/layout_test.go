// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestValueGrammars(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want float64
	}{{"1in", 72}, {"25.4mm", 72}, {"2.54cm", 72}, {".5", .5}, {"-14400pt", -14400}, {"14400", 14400}} {
		got, err := Length(tc.s)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("length %q = %v %v", tc.s, got, err)
		}
	}
	for _, s := range []string{"1e2", " 1", "1 ", "1.", "+1", "NaN", "14400.01", "-14400.01", "1m"} {
		if _, err := Length(s); err == nil {
			t.Errorf("accepted length %q", s)
		}
	}
	for _, s := range []string{"A3", "A4", "A5", "B4", "B5", "Letter", "Legal", "Tabloid", "1x14400pt", "297x210mm", "inherit"} {
		if _, err := ParseSize(s, true); err != nil {
			t.Errorf("size %q: %v", s, err)
		}
	}
	for _, s := range []string{"a4", "0x1", "1x14400.1", "A4P", "1 x 2", "inherit"} {
		if _, err := ParseSize(s, false); err == nil {
			t.Errorf("accepted size %q", s)
		}
	}
	for _, s := range []string{"#abc", "#ABCDEF", "#012345", "none"} {
		if _, err := ParseColor(s, true); err != nil {
			t.Errorf("color %q: %v", s, err)
		}
	}
	for _, s := range []string{"abc", "#abcd", "#ggg", "#12", "none"} {
		if _, err := ParseColor(s, false); err == nil {
			t.Errorf("accepted color %q", s)
		}
	}

}

func TestResolveCanvasesAndRanges(t *testing.T) {
	a, b := Size{Width: 200, Height: 300}, Size{Width: 400, Height: 500}
	explicit := Defaults()
	explicit.Size.Value = Size{Width: 123, Height: 456}
	explicit.Size.Present = true
	inherited := Defaults()
	parts := []Item{&PageItem{Ref: "lead", Style: inherited, Count: 2}, &SourceItem{Ref: "source", Path: "a"}, &PageItem{Ref: "after", Style: inherited, Count: 3}, &PageItem{Ref: "explicit", Style: explicit, Count: 1}}
	facts := map[string]SourceFacts{"source": {Pages: 4, FirstSize: a, LastSize: b}}
	j := newPlan("", nil, nil, PageStyle{}, parts)
	layout, ds := resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), facts, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if layout.SourcePages != 4 || layout.GeneratedPages != 6 || layout.TotalPages != 10 || layout.Parts[0].Size != a || layout.Parts[2].Size != b || layout.Parts[3].Size != explicit.Size.Value {
		t.Fatalf("layout %+v", layout)
	}
	for i, want := range [][2]int64{{1, 2}, {3, 6}, {7, 9}, {10, 10}} {
		p := layout.Parts[i]
		if p.FirstPage != want[0] || p.LastPage != want[1] {
			t.Fatalf("part %d: %+v", i, p)
		}
	}
	fit := Size{Width: 612, Height: 792}
	{
		draft := j.Declaration()
		draft.FitTo = &fit
		j = preparePlan(draft)
	}
	layout, ds = resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), facts, 0)
	if len(ds) > 0 || layout.Parts[0].Size != fit || layout.Parts[2].Size != fit || layout.Parts[3].Size != explicit.Size.Value {
		t.Fatalf("fit %+v %+v", layout, ds)
	}
	_, ds = resolvePacket(newPlan("", nil, nil, PageStyle{}, []Item{&PageItem{Style: inherited, Count: 1}}), nil, 0)
	if len(ds) != 1 || ds[0].Code != diag.PageSizeRequired || !strings.Contains(ds[0].Message, "explicit page size") || !strings.Contains(ds[0].Message, "fitting size") {
		t.Fatal(ds)
	}
	facts["source"] = SourceFacts{Pages: 1, FirstSize: Size{Width: 14401, Height: 300}, LastSize: b}
	_, ds = resolvePacket(newPlan("", nil, nil, PageStyle{}, parts), facts, 0)
	if len(ds) != 1 || ds[0].Code != diag.PageSizeTooLarge {
		t.Fatal(ds)
	}
}

func TestRangesUseInt64(t *testing.T) {
	layout, ds := resolvePacket(newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Path: "a", Ref: "a0"}, &SourceItem{Path: "a", Ref: "a1"}}), map[string]SourceFacts{"a0": {Pages: 2_000_000_000}, "a1": {Pages: 2_000_000_000}}, 0)
	if len(ds) > 0 || layout.TotalPages != 4_000_000_000 || layout.Parts[1].FirstPage != 2_000_000_001 {
		t.Fatalf("ranges %+v %+v", layout, ds)
	}
}

func TestInheritedSizeBelowGeneratedMinimumIsRefused(t *testing.T) {
	page := Defaults()
	_, ds := resolvePacket(newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Path: "tiny", Ref: "tiny"}, &PageItem{Style: page, Count: 1, Ref: "/items/1"}}), map[string]SourceFacts{"tiny": {Pages: 1, FirstSize: Size{Width: .5, Height: 100}, LastSize: Size{Width: .5, Height: 100}}}, 0)
	if len(ds) != 1 || ds[0].Code != diag.PlanValue {
		t.Fatalf("unusable inherited canvas: %+v", ds)
	}
}

func explicitPage(pointer string, size Size, count int) Item {
	style := Defaults()
	style.Size.Value = size
	style.Size.Present = true
	return &PageItem{Ref: pointer, Style: style, Count: count}
}

func TestNestedPaddingRangesAndTargets(t *testing.T) {
	size := Size{Width: 200, Height: 300}
	cover := explicitPage("cover", size, 1)
	for _, alignedParent := range []bool{false, true} {
		parent := &GroupItem{Ref: "parent", Title: "Repeated", Items: []Item{&GroupItem{Ref: "child", Title: "Repeated", StartOn: "odd", Items: []Item{explicitPage("body", size, 1)}}}}
		if alignedParent {
			parent.StartOn = "odd"
		}
		j := newPlan("", nil, nil, PageStyle{}, []Item{cover, parent})
		layout, ds := resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), nil, 0)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if layout.TotalPages != 3 || layout.GeneratedPages != 3 || len(layout.Parts) != 3 || len(layout.Sections) != 2 {
			t.Fatalf("layout %+v", layout)
		}
		padding := layout.Parts[1]
		before := "child"
		parentFirst := int64(2)
		if alignedParent {
			before = "parent"
			parentFirst = 3
		}
		if padding.Kind != "padding" || padding.FirstPage != 2 || padding.LastPage != 2 || padding.BeforeItem != before || padding.Reason != "odd_start" || padding.Page != nil || padding.Size != size {
			t.Fatalf("padding %+v", padding)
		}
		parentSection, child := layout.Sections[0], layout.Sections[1]
		if parentSection.FirstPage != parentFirst || parentSection.LastPage != 3 || parentSection.Target != 3 || child.FirstPage != 3 || child.LastPage != 3 || child.Target != 3 || child.Parent != "parent" || child.Depth != 1 {
			t.Fatalf("section target/ranges %+v", layout.Sections)
		}
	}
}

func TestOddStartBeforeEveryEmissionKind(t *testing.T) {
	size := Size{Width: 200, Height: 300}
	for _, kind := range []string{"source", "page", "divider", "contents"} {
		t.Run(kind, func(t *testing.T) {
			child := explicitPage("body", size, 1)
			section := &GroupItem{Ref: "section", Title: "Title", StartOn: "odd"}
			facts := map[string]SourceFacts{}
			contentsCount := 0
			switch kind {
			case "source":
				child = &SourceItem{Ref: "body", Path: "input"}
				facts["body"] = SourceFacts{Pages: 1, FirstSize: size, LastSize: size}
			case "divider":
				style := Defaults()
				style.Size.Value = size
				style.Size.Present = true
				section.Divider = &style
			case "contents":
				child = &ContentsItem{Ref: "body", Title: "Contents", Size: size}
				contentsCount = 2
			}
			section.Items = []Item{child}
			layout, ds := resolvePacket(newPlan("", nil, nil, PageStyle{}, []Item{explicitPage("cover", size, 1), section}), facts, contentsCount)
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			if layout.Parts[1].Kind != "padding" || layout.Parts[2].Kind.String() != kind || layout.Parts[2].FirstPage != 3 || layout.Sections[0].Target != 3 {
				t.Fatalf("layout %+v", layout)
			}
			if kind == "contents" && layout.Parts[2].Pages != 2 {
				t.Fatalf("contents count %+v", layout)
			}
		})
	}
}

func TestPaddingUsesPreviousPhysicalSheetWhileOtherPagesUseSourceNeighbor(t *testing.T) {
	sourceSize := Size{Width: 100, Height: 200}
	previousPhysical := Size{Width: 500, Height: 600}
	style := Defaults()
	j := newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Ref: "source", Path: "input"}, explicitPage("middle", Size{Width: 300, Height: 400}, 2), &GroupItem{Ref: "aligned", StartOn: "odd", Items: []Item{&PageItem{Ref: "inherited", Style: style, Count: 1}}}})
	facts := map[string]SourceFacts{"source": {Pages: 1, FirstSize: sourceSize, LastSize: sourceSize}}
	items := j.Items()
	itemAs[*PageItem](t, items[1]).Style.Size.Value = previousPhysical
	j = newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), items)
	for _, fit := range []*Size{nil, {Width: 612, Height: 792}} {
		{
			draft := j.Declaration()
			draft.FitTo = fit
			j = preparePlan(draft)
		}
		layout, ds := resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), facts, 0)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		wantPadding, wantInherited := previousPhysical, sourceSize
		if fit != nil {
			wantPadding, wantInherited = *fit, *fit
		}
		if layout.Parts[2].Size != wantPadding || layout.Parts[2].Kind != "padding" || layout.Parts[3].Size != wantInherited {
			t.Fatalf("physical vs source inheritance %+v", layout)
		}
	}
	facts["source"] = SourceFacts{Pages: 1, FirstSize: sourceSize, LastSize: Size{Width: .5, Height: 200}}
	j = newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Ref: "source", Path: "input"}, &GroupItem{Ref: "boundary", StartOn: "odd", Items: []Item{explicitPage("body", sourceSize, 1)}}})
	layout, ds := resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), facts, 0)
	if len(ds) != 1 || ds[0].Items[0] != "boundary" || !strings.Contains(ds[0].Message, "fit") || len(layout.Parts) != 0 {
		t.Fatalf("unusable parity canvas: %+v %+v", layout, ds)
	}
}

func TestCompactMappingsAndSections(t *testing.T) {
	size := Size{Width: 200, Height: 300}
	divider := Defaults()
	divider.Size.Value = size
	divider.Size.Present = true
	divider.Text.Value.Value = "not a stored title"
	j := newPlan("", nil, &PageNumbers{Size: 9, Margin: 0}, PageStyle{}, []Item{&GroupItem{Ref: "outer", Title: "Outer", Divider: &divider, Items: []Item{&GroupItem{Ref: "inner", Title: "Inner", Items: []Item{&SourceItem{Ref: "input", Path: "same.pdf", Selection: []PageRange{{First: 2, Last: 4}, {First: 7, Last: 7}}}}}, explicitPage("after-inner", size, 2)}}, &GroupItem{Ref: "sibling", Title: "Sibling", Items: []Item{explicitPage("bulk", size, 99997)}}})
	leaves, sections := j.Leaves(), j.Sections()
	if len(leaves) != 4 || len(sections) != 3 || leaves[2].Owner != "outer" || sections[2].Parent != "" || sections[2].Depth != 0 || leaves[0].Page.Text.Value.Value != "Outer" || divider.Text.Value.Value != "not a stored title" {
		t.Fatalf("declarations %+v %+v", leaves, sections)
	}
	facts := map[string]SourceFacts{"input": {Pages: 4, FirstSize: size, LastSize: size, Selection: []PageRange{{First: 2, Last: 4}, {First: 7, Last: 7}}}}
	layout, ds := resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), facts, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(layout.Parts) != 4 || layout.GeneratedPages != 100000 || layout.SourcePages != 4 || layout.TotalPages != 100004 || !reflect.DeepEqual(layout.PageNumbers, j.PageNumbers()) {
		t.Fatalf("compact layout %+v", layout)
	}
	mapping := layout.Parts[1].Mapping
	if len(mapping) != 2 || mapping[0] != (SourceMapping{SourceFirst: 2, SourceLast: 4, OutputFirst: 2}) || mapping[1] != (SourceMapping{SourceFirst: 7, SourceLast: 7, OutputFirst: 5}) {
		t.Fatalf("mapping %+v", mapping)
	}
	var next int64 = 1
	for _, part := range layout.Parts {
		if part.FirstPage != next || part.LastPage-part.FirstPage+1 != int64(part.Pages) {
			t.Fatalf("parts do not partition output: %+v", layout)
		}
		next = part.LastPage + 1
	}
	if next != layout.TotalPages+1 {
		t.Fatal("missing emitted pages")
	}
}

func TestLayoutLimitsAndNoInventedPartialSequence(t *testing.T) {
	size := Size{Width: 100, Height: 200}
	for _, j := range []Plan{newPlan("", nil, nil, PageStyle{}, []Item{explicitPage("bulk", size, 100000), &ContentsItem{Ref: "contents", Title: "Contents", Size: size}}), newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Ref: "source", Path: "input"}, explicitPage("bulk", size, 100000), &GroupItem{Ref: "odd", StartOn: "odd", Items: []Item{explicitPage("body", size, 1)}}}), newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Ref: "source", Path: "input"}, &SourceItem{Ref: "source2", Path: "input"}})} {
		facts := map[string]SourceFacts{"source": {Pages: 1, FirstSize: size, LastSize: size}}
		_, isSource := j.Items()[1].(*SourceItem)
		if len(j.Items()) == 2 && isSource {
			facts["source"] = SourceFacts{Pages: int(MaxPageIndex)}
			facts["source2"] = SourceFacts{Pages: 1}
		}
		layout, ds := resolvePacket(newPlan(j.Output(), j.FitTo(), j.PageNumbers(), j.PageDefaults(), j.Items()), facts, 1)
		if len(ds) == 0 || ds[0].Code != diag.PlanLimit || len(layout.Parts) != 0 || layout.TotalPages != 0 || len(layout.Sections) != 0 {
			t.Fatalf("invented partial layout: %+v %+v", layout, ds)
		}
	}
	layout, ds := resolvePacket(newPlan("", nil, nil, PageStyle{}, []Item{&SourceItem{Ref: "source", Path: "input"}}), map[string]SourceFacts{"source": {Pages: 3, Selection: []PageRange{{First: 1, Last: 2}}}}, 0)
	if len(ds) != 1 || ds[0].Code != diag.SourceProcessingFailed || layout.TotalPages != 0 {
		t.Fatalf("inconsistent occurrence facts: %+v %+v", layout, ds)
	}
}

func itemAs[T Item](t testing.TB, item Item) T {
	t.Helper()
	value, ok := item.(T)
	if !ok {
		t.Fatalf("item has type %T, expected %T", item, value)
	}
	return value
}

func resolvePacket(p Plan, facts map[string]SourceFacts, contents int) (Layout, []diag.Diagnostic) {
	shapes := make([]SourceFacts, len(p.Leaves()))
	for i, item := range p.Leaves() {
		shapes[i] = facts[item.Ref]
	}
	return Resolve(p, shapes, contents)
}

func TestMissingInspectionFactsAreNotMalformedDeclarations(t *testing.T) {
	plan := newPlan("", nil, nil, Defaults(), []Item{&SourceItem{Ref: "source", Path: "source.pdf"}})
	_, ds := Resolve(plan, nil, 0)
	if len(ds) != 1 || ds[0].Code != diag.SourceProcessingFailed || diag.Outcome(ds) != diag.StatusFailed {
		t.Fatalf("missing inspection data became invalid input: %+v", ds)
	}
}
