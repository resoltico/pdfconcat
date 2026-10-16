// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package job_test

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/job"
)

func TestValueGrammars(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want float64
	}{{"1in", 72}, {"25.4mm", 72}, {"2.54cm", 72}, {".5", .5}, {"-14400pt", -14400}, {"14400", 14400}} {
		got, err := job.Length(tc.s)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("length %q = %v %v", tc.s, got, err)
		}
	}
	for _, s := range []string{"1e2", " 1", "1 ", "1.", "+1", "NaN", "14400.01", "-14400.01", "1m"} {
		if _, err := job.Length(s); err == nil {
			t.Errorf("accepted length %q", s)
		}
	}
	for _, s := range []string{"A3", "A4", "A5", "B4", "B5", "Letter", "Legal", "Tabloid", "1x14400pt", "297x210mm", "inherit"} {
		if _, err := job.ParseSize(s, true); err != nil {
			t.Errorf("size %q: %v", s, err)
		}
	}
	for _, s := range []string{"a4", "0x1", "1x14400.1", "A4P", "1 x 2", "inherit"} {
		if _, err := job.ParseSize(s, false); err == nil {
			t.Errorf("accepted size %q", s)
		}
	}
	for _, s := range []string{"#abc", "#ABCDEF", "#012345", "none"} {
		if _, err := job.ParseColor(s, true); err != nil {
			t.Errorf("color %q: %v", s, err)
		}
	}
	for _, s := range []string{"abc", "#abcd", "#ggg", "#12", "none"} {
		if _, err := job.ParseColor(s, false); err == nil {
			t.Errorf("accepted color %q", s)
		}
	}
	for _, s := range []string{"", "a\x00b"} {
		if _, err := job.Path(".", s); err == nil {
			t.Errorf("accepted path %q", s)
		}
	}
	path, err := job.Path(".", "a/link/../b.pdf")
	want, _ := filepath.Abs("a/b.pdf")
	if err != nil || path != want {
		t.Fatalf("lexical path %q %v", path, err)
	}
}

func TestResolveCanvasesAndRanges(t *testing.T) {
	a, b := job.Size{Width: 200, Height: 300}, job.Size{Width: 400, Height: 500}
	explicit := job.Defaults()
	explicit.Size = job.Size{Width: 123, Height: 456}
	inherited := job.Defaults()
	parts := []job.Part{{Pointer: "lead", Page: &inherited, Count: 2}, {Pointer: "source", Source: "a"}, {Pointer: "after", Page: &inherited, Count: 3}, {Pointer: "explicit", Page: &explicit, Count: 1}}
	facts := map[string]job.SourceFacts{"source": {Pages: 4, FirstSize: a, LastSize: b}}
	j := job.Job{Parts: parts}
	layout, ds := job.Resolve(j, facts, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if layout.SourcePages != 4 || layout.GeneratedPages != 6 || layout.TotalPages != 10 || layout.Parts[0].Size != a || layout.Parts[2].Size != b || layout.Parts[3].Size != explicit.Size {
		t.Fatalf("layout %+v", layout)
	}
	for i, want := range [][2]int64{{1, 2}, {3, 6}, {7, 9}, {10, 10}} {
		p := layout.Parts[i]
		if p.FirstPage != want[0] || p.LastPage != want[1] {
			t.Fatalf("part %d: %+v", i, p)
		}
	}
	fit := job.Size{Width: 612, Height: 792}
	j.FitTo = &fit
	layout, ds = job.Resolve(j, facts, 0)
	if len(ds) > 0 || layout.Parts[0].Size != fit || layout.Parts[2].Size != fit || layout.Parts[3].Size != explicit.Size {
		t.Fatalf("fit %+v %+v", layout, ds)
	}
	_, ds = job.Resolve(job.Job{Parts: []job.Part{{Page: &inherited, Count: 1}}}, nil, 0)
	if len(ds) != 1 || ds[0].Code != "page_size_required" {
		t.Fatal(ds)
	}
	facts["source"] = job.SourceFacts{Pages: 1, FirstSize: job.Size{Width: 14401, Height: 300}, LastSize: b}
	_, ds = job.Resolve(job.Job{Parts: parts}, facts, 0)
	if len(ds) != 1 || ds[0].Code != "page_size_too_large" {
		t.Fatal(ds)
	}
}

func TestRangesUseInt64(t *testing.T) {
	layout, ds := job.Resolve(job.Job{Parts: []job.Part{{Source: "a", Pointer: "a0"}, {Source: "a", Pointer: "a1"}}}, map[string]job.SourceFacts{"a0": {Pages: 2_000_000_000}, "a1": {Pages: 2_000_000_000}}, 0)
	if len(ds) > 0 || layout.TotalPages != 4_000_000_000 || layout.Parts[1].FirstPage != 2_000_000_001 {
		t.Fatalf("ranges %+v %+v", layout, ds)
	}
}

func TestInheritedSizeBelowGeneratedMinimumIsRefused(t *testing.T) {
	page := job.Defaults()
	_, ds := job.Resolve(job.Job{Parts: []job.Part{{Source: "tiny", Pointer: "tiny"}, {Page: &page, Count: 1, Pointer: "/items/1"}}}, map[string]job.SourceFacts{"tiny": {Pages: 1, FirstSize: job.Size{Width: .5, Height: 100}, LastSize: job.Size{Width: .5, Height: 100}}}, 0)
	if len(ds) != 1 || ds[0].Code != "plan_value" {
		t.Fatalf("unusable inherited canvas: %+v", ds)
	}
}

func explicitPage(pointer string, size job.Size, count int) job.Part {
	style := job.Defaults()
	style.Size = size
	return job.Part{Pointer: pointer, Page: &style, Count: count}
}

func TestNestedPaddingRangesAndTargets(t *testing.T) {
	size := job.Size{Width: 200, Height: 300}
	cover := explicitPage("cover", size, 1)
	for _, alignedParent := range []bool{false, true} {
		parent := job.Part{Pointer: "parent", Title: "Repeated", Items: []job.Part{{Pointer: "child", Title: "Repeated", StartOn: "odd", Items: []job.Part{explicitPage("body", size, 1)}}}}
		if alignedParent {
			parent.StartOn = "odd"
		}
		j := job.Job{Parts: []job.Part{cover, parent}}
		layout, ds := job.Resolve(j, nil, 0)
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
	size := job.Size{Width: 200, Height: 300}
	for _, kind := range []string{"source", "page", "divider", "contents"} {
		t.Run(kind, func(t *testing.T) {
			child := explicitPage("body", size, 1)
			section := job.Part{Pointer: "section", Title: "Title", StartOn: "odd"}
			facts := map[string]job.SourceFacts{}
			contentsCount := 0
			switch kind {
			case "source":
				child = job.Part{Pointer: "body", Source: "input"}
				facts["body"] = job.SourceFacts{Pages: 1, FirstSize: size, LastSize: size}
			case "divider":
				style := job.Defaults()
				style.Size = size
				section.Divider = &style
			case "contents":
				child = job.Part{Pointer: "body", Contents: &job.ContentsStyle{Title: "Contents", Size: size}}
				contentsCount = 2
			}
			section.Items = []job.Part{child}
			layout, ds := job.Resolve(job.Job{Parts: []job.Part{explicitPage("cover", size, 1), section}}, facts, contentsCount)
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			if layout.Parts[1].Kind != "padding" || layout.Parts[2].Kind != kind || layout.Parts[2].FirstPage != 3 || layout.Sections[0].Target != 3 {
				t.Fatalf("layout %+v", layout)
			}
			if kind == "contents" && layout.Parts[2].Pages != 2 {
				t.Fatalf("contents count %+v", layout)
			}
		})
	}
}

func TestPaddingUsesPreviousPhysicalSheetWhileOtherPagesUseSourceNeighbor(t *testing.T) {
	sourceSize := job.Size{Width: 100, Height: 200}
	previousPhysical := job.Size{Width: 500, Height: 600}
	style := job.Defaults()
	j := job.Job{Parts: []job.Part{{Pointer: "source", Source: "input"}, explicitPage("middle", job.Size{Width: 300, Height: 400}, 2), {Pointer: "aligned", StartOn: "odd", Items: []job.Part{{Pointer: "inherited", Page: &style, Count: 1}}}}}
	facts := map[string]job.SourceFacts{"source": {Pages: 1, FirstSize: sourceSize, LastSize: sourceSize}}
	j.Parts[1].Page.Size = previousPhysical
	for _, fit := range []*job.Size{nil, {Width: 612, Height: 792}} {
		j.FitTo = fit
		layout, ds := job.Resolve(j, facts, 0)
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
	facts["source"] = job.SourceFacts{Pages: 1, FirstSize: sourceSize, LastSize: job.Size{Width: .5, Height: 200}}
	j = job.Job{Parts: []job.Part{{Pointer: "source", Source: "input"}, {Pointer: "boundary", StartOn: "odd", Items: []job.Part{explicitPage("body", sourceSize, 1)}}}}
	layout, ds := job.Resolve(j, facts, 0)
	if len(ds) != 1 || ds[0].Items[0] != "boundary" || !strings.Contains(ds[0].Message, "fit") || len(layout.Parts) != 0 {
		t.Fatalf("unusable parity canvas: %+v %+v", layout, ds)
	}
}

func TestCompactMappingsAndDeclarations(t *testing.T) {
	size := job.Size{Width: 200, Height: 300}
	divider := job.Defaults()
	divider.Size = size
	divider.Text.Value = "not a stored title"
	j := job.Job{Parts: []job.Part{{Pointer: "outer", Title: "Outer", Divider: &divider, Items: []job.Part{{Pointer: "inner", Title: "Inner", Items: []job.Part{{Pointer: "input", Source: "same.pdf", Selection: []job.PageRange{{First: 2, Last: 4}, {First: 7, Last: 7}}}}}, explicitPage("after-inner", size, 2)}}, {Pointer: "sibling", Title: "Sibling", Items: []job.Part{explicitPage("bulk", size, 99997)}}}, PageNumbers: &job.PageNumbers{Size: 9, Margin: 0}}
	leaves, sections := job.Declarations(j)
	if len(leaves) != 4 || len(sections) != 3 || leaves[2].Owner != "outer" || sections[2].Parent != "" || sections[2].Depth != 0 || leaves[0].Page.Text.Value != "Outer" || divider.Text.Value != "not a stored title" {
		t.Fatalf("declarations %+v %+v", leaves, sections)
	}
	facts := map[string]job.SourceFacts{"input": {Pages: 4, FirstSize: size, LastSize: size, Selection: []job.PageRange{{First: 2, Last: 4}, {First: 7, Last: 7}}}}
	layout, ds := job.Resolve(j, facts, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(layout.Parts) != 4 || layout.GeneratedPages != 100000 || layout.SourcePages != 4 || layout.TotalPages != 100004 || layout.PageNumbers != j.PageNumbers {
		t.Fatalf("compact layout %+v", layout)
	}
	mapping := layout.Parts[1].Mapping
	if len(mapping) != 2 || mapping[0] != (job.SourceMapping{SourceFirst: 2, SourceLast: 4, OutputFirst: 2}) || mapping[1] != (job.SourceMapping{SourceFirst: 7, SourceLast: 7, OutputFirst: 5}) {
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
	size := job.Size{Width: 100, Height: 200}
	for _, j := range []job.Job{
		{Parts: []job.Part{explicitPage("bulk", size, 100000), {Pointer: "contents", Contents: &job.ContentsStyle{Title: "Contents", Size: size}}}},
		{Parts: []job.Part{{Pointer: "source", Source: "input"}, explicitPage("bulk", size, 100000), {Pointer: "odd", StartOn: "odd", Items: []job.Part{explicitPage("body", size, 1)}}}},
		{Parts: []job.Part{{Pointer: "source", Source: "input"}, {Pointer: "source2", Source: "input"}}},
	} {
		facts := map[string]job.SourceFacts{"source": {Pages: 1, FirstSize: size, LastSize: size}}
		if len(j.Parts) == 2 && j.Parts[1].Source != "" {
			facts["source"] = job.SourceFacts{Pages: int(job.MaxPageIndex)}
			facts["source2"] = job.SourceFacts{Pages: 1}
		}
		layout, ds := job.Resolve(j, facts, 1)
		if len(ds) == 0 || ds[0].Code != "plan_limit" || len(layout.Parts) != 0 || layout.TotalPages != 0 || len(layout.Sections) != 0 {
			t.Fatalf("invented partial layout: %+v %+v", layout, ds)
		}
	}
	layout, ds := job.Resolve(job.Job{Parts: []job.Part{{Pointer: "source", Source: "input"}}}, map[string]job.SourceFacts{"source": {Pages: 3, Selection: []job.PageRange{{First: 1, Last: 2}}}}, 0)
	if len(ds) != 1 || ds[0].Code != "source_invalid" || layout.TotalPages != 0 {
		t.Fatalf("inconsistent occurrence facts: %+v %+v", layout, ds)
	}
}
