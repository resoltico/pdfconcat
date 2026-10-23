// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet_test

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

func TestValueGrammars(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want float64
	}{{"1in", 72}, {"25.4mm", 72}, {"2.54cm", 72}, {".5", .5}, {"-14400pt", -14400}, {"14400", 14400}} {
		got, err := packet.Length(tc.s)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("length %q = %v %v", tc.s, got, err)
		}
	}
	for _, s := range []string{"1e2", " 1", "1 ", "1.", "+1", "NaN", "14400.01", "-14400.01", "1m"} {
		if _, err := packet.Length(s); err == nil {
			t.Errorf("accepted length %q", s)
		}
	}
	for _, s := range []string{"A3", "A4", "A5", "B4", "B5", "Letter", "Legal", "Tabloid", "1x14400pt", "297x210mm", "inherit"} {
		if _, err := packet.ParseSize(s, true); err != nil {
			t.Errorf("size %q: %v", s, err)
		}
	}
	for _, s := range []string{"a4", "0x1", "1x14400.1", "A4P", "1 x 2", "inherit"} {
		if _, err := packet.ParseSize(s, false); err == nil {
			t.Errorf("accepted size %q", s)
		}
	}
	for _, s := range []string{"#abc", "#ABCDEF", "#012345", "none"} {
		if _, err := packet.ParseColor(s, true); err != nil {
			t.Errorf("color %q: %v", s, err)
		}
	}
	for _, s := range []string{"abc", "#abcd", "#ggg", "#12", "none"} {
		if _, err := packet.ParseColor(s, false); err == nil {
			t.Errorf("accepted color %q", s)
		}
	}
	for _, s := range []string{"", "a\x00b"} {
		if _, err := packet.Path(".", s); err == nil {
			t.Errorf("accepted path %q", s)
		}
	}
	path, err := packet.Path(".", "a/link/../b.pdf")
	want, _ := filepath.Abs("a/b.pdf")
	if err != nil || path != want {
		t.Fatalf("lexical path %q %v", path, err)
	}
}

func TestResolveCanvasesAndRanges(t *testing.T) {
	a, b := packet.Size{Width: 200, Height: 300}, packet.Size{Width: 400, Height: 500}
	explicit := packet.Defaults()
	explicit.Size.Value = packet.Size{Width: 123, Height: 456}
	inherited := packet.Defaults()
	parts := []packet.Item{&packet.PageItem{Ref: "lead", Style: inherited, Count: 2}, &packet.SourceItem{Ref: "source", Path: "a"}, &packet.PageItem{Ref: "after", Style: inherited, Count: 3}, &packet.PageItem{Ref: "explicit", Style: explicit, Count: 1}}
	facts := map[string]packet.SourceFacts{"source": {Pages: 4, FirstSize: a, LastSize: b}}
	j := packet.NewPlan("", nil, nil, packet.PageStyle{}, parts)
	layout, ds := resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), facts, 0)
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
	fit := packet.Size{Width: 612, Height: 792}
	j.FitTo = &fit
	layout, ds = resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), facts, 0)
	if len(ds) > 0 || layout.Parts[0].Size != fit || layout.Parts[2].Size != fit || layout.Parts[3].Size != explicit.Size.Value {
		t.Fatalf("fit %+v %+v", layout, ds)
	}
	_, ds = resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.PageItem{Style: inherited, Count: 1}}), nil, 0)
	if len(ds) != 1 || ds[0].Code != diag.PageSizeRequired {
		t.Fatal(ds)
	}
	facts["source"] = packet.SourceFacts{Pages: 1, FirstSize: packet.Size{Width: 14401, Height: 300}, LastSize: b}
	_, ds = resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, parts), facts, 0)
	if len(ds) != 1 || ds[0].Code != diag.PageSizeTooLarge {
		t.Fatal(ds)
	}
}

func TestRangesUseInt64(t *testing.T) {
	layout, ds := resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Path: "a", Ref: "a0"}, &packet.SourceItem{Path: "a", Ref: "a1"}}), map[string]packet.SourceFacts{"a0": {Pages: 2_000_000_000}, "a1": {Pages: 2_000_000_000}}, 0)
	if len(ds) > 0 || layout.TotalPages != 4_000_000_000 || layout.Parts[1].FirstPage != 2_000_000_001 {
		t.Fatalf("ranges %+v %+v", layout, ds)
	}
}

func TestInheritedSizeBelowGeneratedMinimumIsRefused(t *testing.T) {
	page := packet.Defaults()
	_, ds := resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Path: "tiny", Ref: "tiny"}, &packet.PageItem{Style: page, Count: 1, Ref: "/items/1"}}), map[string]packet.SourceFacts{"tiny": {Pages: 1, FirstSize: packet.Size{Width: .5, Height: 100}, LastSize: packet.Size{Width: .5, Height: 100}}}, 0)
	if len(ds) != 1 || ds[0].Code != diag.PlanValue {
		t.Fatalf("unusable inherited canvas: %+v", ds)
	}
}

func explicitPage(pointer string, size packet.Size, count int) packet.Item {
	style := packet.Defaults()
	style.Size.Value = size
	return &packet.PageItem{Ref: pointer, Style: style, Count: count}
}

func TestNestedPaddingRangesAndTargets(t *testing.T) {
	size := packet.Size{Width: 200, Height: 300}
	cover := explicitPage("cover", size, 1)
	for _, alignedParent := range []bool{false, true} {
		parent := &packet.GroupItem{Ref: "parent", Title: "Repeated", Items: []packet.Item{&packet.GroupItem{Ref: "child", Title: "Repeated", StartOn: "odd", Items: []packet.Item{explicitPage("body", size, 1)}}}}
		if alignedParent {
			parent.StartOn = "odd"
		}
		j := packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{cover, parent})
		layout, ds := resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, 0)
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
	size := packet.Size{Width: 200, Height: 300}
	for _, kind := range []string{"source", "page", "divider", "contents"} {
		t.Run(kind, func(t *testing.T) {
			child := explicitPage("body", size, 1)
			section := &packet.GroupItem{Ref: "section", Title: "Title", StartOn: "odd"}
			facts := map[string]packet.SourceFacts{}
			contentsCount := 0
			switch kind {
			case "source":
				child = &packet.SourceItem{Ref: "body", Path: "input"}
				facts["body"] = packet.SourceFacts{Pages: 1, FirstSize: size, LastSize: size}
			case "divider":
				style := packet.Defaults()
				style.Size.Value = size
				section.Divider = &style
			case "contents":
				child = &packet.ContentsItem{Ref: "body", Title: "Contents", Size: size}
				contentsCount = 2
			}
			section.Items = []packet.Item{child}
			layout, ds := resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{explicitPage("cover", size, 1), section}), facts, contentsCount)
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
	sourceSize := packet.Size{Width: 100, Height: 200}
	previousPhysical := packet.Size{Width: 500, Height: 600}
	style := packet.Defaults()
	j := packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Ref: "source", Path: "input"}, explicitPage("middle", packet.Size{Width: 300, Height: 400}, 2), &packet.GroupItem{Ref: "aligned", StartOn: "odd", Items: []packet.Item{&packet.PageItem{Ref: "inherited", Style: style, Count: 1}}}})
	facts := map[string]packet.SourceFacts{"source": {Pages: 1, FirstSize: sourceSize, LastSize: sourceSize}}
	itemAs[*packet.PageItem](t, j.Items[1]).Style.Size.Value = previousPhysical
	for _, fit := range []*packet.Size{nil, {Width: 612, Height: 792}} {
		j.FitTo = fit
		layout, ds := resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), facts, 0)
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
	facts["source"] = packet.SourceFacts{Pages: 1, FirstSize: sourceSize, LastSize: packet.Size{Width: .5, Height: 200}}
	j = packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Ref: "source", Path: "input"}, &packet.GroupItem{Ref: "boundary", StartOn: "odd", Items: []packet.Item{explicitPage("body", sourceSize, 1)}}})
	layout, ds := resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), facts, 0)
	if len(ds) != 1 || ds[0].Items[0] != "boundary" || !strings.Contains(ds[0].Message, "fit") || len(layout.Parts) != 0 {
		t.Fatalf("unusable parity canvas: %+v %+v", layout, ds)
	}
}

func TestCompactMappingsAndSections(t *testing.T) {
	size := packet.Size{Width: 200, Height: 300}
	divider := packet.Defaults()
	divider.Size.Value = size
	divider.Text.Value.Value = "not a stored title"
	j := packet.NewPlan("", nil, &packet.PageNumbers{Size: 9, Margin: 0}, packet.PageStyle{}, []packet.Item{&packet.GroupItem{Ref: "outer", Title: "Outer", Divider: &divider, Items: []packet.Item{&packet.GroupItem{Ref: "inner", Title: "Inner", Items: []packet.Item{&packet.SourceItem{Ref: "input", Path: "same.pdf", Selection: []packet.PageRange{{First: 2, Last: 4}, {First: 7, Last: 7}}}}}, explicitPage("after-inner", size, 2)}}, &packet.GroupItem{Ref: "sibling", Title: "Sibling", Items: []packet.Item{explicitPage("bulk", size, 99997)}}})
	leaves, sections := j.Leaves(), j.Sections()
	if len(leaves) != 4 || len(sections) != 3 || leaves[2].Owner != "outer" || sections[2].Parent != "" || sections[2].Depth != 0 || leaves[0].Page.Text.Value.Value != "Outer" || divider.Text.Value.Value != "not a stored title" {
		t.Fatalf("declarations %+v %+v", leaves, sections)
	}
	facts := map[string]packet.SourceFacts{"input": {Pages: 4, FirstSize: size, LastSize: size, Selection: []packet.PageRange{{First: 2, Last: 4}, {First: 7, Last: 7}}}}
	layout, ds := resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), facts, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(layout.Parts) != 4 || layout.GeneratedPages != 100000 || layout.SourcePages != 4 || layout.TotalPages != 100004 || layout.PageNumbers != j.PageNumbers {
		t.Fatalf("compact layout %+v", layout)
	}
	mapping := layout.Parts[1].Mapping
	if len(mapping) != 2 || mapping[0] != (packet.SourceMapping{SourceFirst: 2, SourceLast: 4, OutputFirst: 2}) || mapping[1] != (packet.SourceMapping{SourceFirst: 7, SourceLast: 7, OutputFirst: 5}) {
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
	size := packet.Size{Width: 100, Height: 200}
	for _, j := range []packet.Plan{packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{explicitPage("bulk", size, 100000), &packet.ContentsItem{Ref: "contents", Title: "Contents", Size: size}}), packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Ref: "source", Path: "input"}, explicitPage("bulk", size, 100000), &packet.GroupItem{Ref: "odd", StartOn: "odd", Items: []packet.Item{explicitPage("body", size, 1)}}}), packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Ref: "source", Path: "input"}, &packet.SourceItem{Ref: "source2", Path: "input"}})} {
		facts := map[string]packet.SourceFacts{"source": {Pages: 1, FirstSize: size, LastSize: size}}
		_, isSource := j.Items[1].(*packet.SourceItem)
		if len(j.Items) == 2 && isSource {
			facts["source"] = packet.SourceFacts{Pages: int(packet.MaxPageIndex)}
			facts["source2"] = packet.SourceFacts{Pages: 1}
		}
		layout, ds := resolvePacket(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), facts, 1)
		if len(ds) == 0 || ds[0].Code != diag.PlanLimit || len(layout.Parts) != 0 || layout.TotalPages != 0 || len(layout.Sections) != 0 {
			t.Fatalf("invented partial layout: %+v %+v", layout, ds)
		}
	}
	layout, ds := resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Ref: "source", Path: "input"}}), map[string]packet.SourceFacts{"source": {Pages: 3, Selection: []packet.PageRange{{First: 1, Last: 2}}}}, 0)
	if len(ds) != 1 || ds[0].Code != diag.SourceInvalid || layout.TotalPages != 0 {
		t.Fatalf("inconsistent occurrence facts: %+v %+v", layout, ds)
	}
}

func itemAs[T packet.Item](t testing.TB, item packet.Item) T {
	t.Helper()
	value, ok := item.(T)
	if !ok {
		t.Fatalf("item has type %T, expected %T", item, value)
	}
	return value
}

func resolvePacket(p packet.Plan, facts map[string]packet.SourceFacts, contents int) (packet.Layout, []diag.Diagnostic) {
	shapes := make([]packet.SourceFacts, len(p.Leaves()))
	for i, item := range p.Leaves() {
		shapes[i] = facts[item.Ref]
	}
	return packet.Resolve(p, shapes, contents)
}
