// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import "testing"

func TestResolvedNumberingEligibilityAndOverflow(t *testing.T) {
	yes, no := true, false
	items := []Item{&PageItem{Ref: "cover", Count: 1, Style: Defaults(), Numbering: &no}, &GroupItem{Ref: "group", Title: "Section", Numbering: &no, Items: []Item{&PageItem{Ref: "inherited", Count: 1, Style: Defaults()}, &PageItem{Ref: "override", Count: 2, Style: Defaults(), Numbering: &yes}}}}
	size := Size{Width: 300, Height: 400}
	for _, mode := range []NumberingMode{NumberPhysical, NumberConsecutive} {
		plan := NewPlan("", &size, &PageNumbers{Mode: mode, Start: 9, Size: 9}, Defaults(), items)
		layout, ds := Resolve(plan, nil, 0)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		expected := []string{"—", "—", "11", "12"}
		if mode == "consecutive" {
			expected = []string{"—", "—", "9", "10"}
		}
		for i, want := range expected {
			if got := layout.LabelAt(int64(i + 1)); got != want {
				t.Fatalf("%s page %d: %s != %s", mode, i+1, got, want)
			}
		}
		if len(layout.Numbering) != 2 {
			t.Fatalf("number spans not compact: %v", layout.Numbering)
		}
	}
	for _, count := range []int{1, 2} {
		p := NewPlan("", &size, &PageNumbers{Mode: "consecutive", Start: MaxPageIndex, Size: 9}, Defaults(), []Item{&PageItem{Ref: "page", Style: Defaults(), Count: count}, &PageItem{Ref: "skip", Count: 1, Style: Defaults(), Numbering: &no}})
		layout, ds := Resolve(p, nil, 0)
		if (len(ds) > 0) != (count == 2) {
			t.Fatalf("count %d: %v", count, ds)
		}
		if count == 1 && layout.LabelAt(1) != "9223372036854775807" {
			t.Fatal(layout.LabelAt(1))
		}
	}
	p := NewPlan("", &size, &PageNumbers{Kinds: []PartKind{}, Size: 9}, Defaults(), items)
	layout, ds := Resolve(p, nil, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for page := int64(1); page <= layout.TotalPages; page++ {
		if layout.LabelAt(page) != "—" {
			t.Fatal("empty kind set numbered a page")
		}
	}
}
