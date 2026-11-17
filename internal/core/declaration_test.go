// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestTypedDeclarationValidationAndOwnership(t *testing.T) {
	size := Size{Width: 300, Height: 400}
	style := PageStyle{Size: Setting[Size]{Present: true, Value: size}, Text: TextStyle{Value: Setting[string]{Present: true, Value: "body"}}}
	source := &SourceItem{Path: "source.pdf", Selection: []PageRange{{1, 2}}}
	page := &PageItem{Style: style, Count: 2}
	group := &GroupItem{Title: "Section", Items: []Item{source, page}}
	d, ds := ValidateDeclaration(Declaration{Items: []Item{&ContentsItem{}, group}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	p := preparePlan(d)
	source.Selection[0].Last = 99
	page.Style.Text.Value.Value = "changed"
	group.Items = nil
	leaves := p.Leaves()
	if leaves[1].Selection[0].Last != 2 || leaves[2].Page.Text.Value.Value != "body" {
		t.Fatal("input mutation changed prepared state")
	}
	leaves[1].Selection[0].First = 88
	leaves[2].Page.Text.Value.Value = "snapshot mutation"
	snapshot := p.Items()
	groupSnapshot, ok := snapshot[1].(*GroupItem)
	if !ok {
		t.Fatal("missing group")
	}
	groupSnapshot.Items = nil
	if again := p.Leaves(); again[1].Selection[0].First != 1 || again[2].Page.Text.Value.Value != "body" {
		t.Fatal("snapshot mutation changed prepared state")
	}
	refs := map[string]bool{}
	for _, leaf := range p.Leaves() {
		if leaf.Ref == "" || refs[leaf.Ref] {
			t.Fatal("unstable identity")
		}
		refs[leaf.Ref] = true
	}
	cases := []Declaration{
		{Items: []Item{(*PageItem)(nil)}},
		{Items: []Item{&PageItem{Count: 0}}},
		{Items: []Item{&PageItem{Count: MaxGeneratedPages + 1}}},
		{Items: []Item{&SourceItem{Path: "s", Selection: []PageRange{{2, 3}, {3, 4}}}}},
		{Items: []Item{&ContentsItem{}}},
		{Items: []Item{&PageItem{Count: 1, Style: PageStyle{Text: TextStyle{Size: Setting[float64]{Present: true, Value: math.NaN()}}}}}},
		{Items: []Item{&PageItem{Count: 1, Style: PageStyle{Text: TextStyle{Align: Setting[Align]{Present: true, Value: "bad"}}}}}},
	}
	cycle := &GroupItem{}
	cycle.Items = []Item{cycle}
	cases = append(cases, Declaration{Items: []Item{cycle}})
	for i, input := range cases {
		if _, ds := AdmitDeclaration(input); len(ds) == 0 {
			t.Errorf("accepted invalid declaration %d", i)
		}
	}
}
func TestTypedPresenceDoesNotRequireProvenance(t *testing.T) {
	base := Defaults()
	base.Text.X = Setting[float64]{Present: true, Value: 15}
	base.Text.Value = Setting[string]{Present: true, Value: "default"}
	style := PageStyle{Text: TextStyle{X: Setting[float64]{Present: true, Value: 0}, Value: Setting[string]{Present: true, Value: ""}}}.Over(base)
	if style.Text.X.Value != 0 || style.Text.Value.Value != "" || style.Text.X.From != "" {
		t.Fatal(style)
	}
	if absent := (PageStyle{}).Over(base); absent.Text.X.Value != 15 || absent.Text.Value.Value != "default" {
		t.Fatal(absent)
	}
}

// Embedding cannot turn an unknown concrete variant into a valid item.
type embeddedItem struct {
	*PageItem
	Uncomparable []byte
}

func TestTypedUnknownVariantAndExplicitValuesRefused(t *testing.T) {
	for _, items := range [][]Item{
		{embeddedItem{PageItem: &PageItem{Count: 1}}},
		{&SourceItem{Path: "source.pdf", Selection: []PageRange{}}},
		{&ContentsItem{TitlePresent: true}},
		{&GroupItem{TitlePresent: true, Items: []Item{&PageItem{Count: 1}}}},
		{&GroupItem{Title: "Section", Divider: &PageStyle{Text: TextStyle{Value: Setting[string]{Present: true, Value: "ignored"}}}, Items: []Item{&PageItem{Count: 1}}}},
	} {
		if _, ds := ValidateDeclaration(Declaration{Items: items}); len(ds) == 0 {
			t.Fatalf("invalid explicit declaration accepted: %#v", items)
		}
	}
	numbers := DefaultPageNumbers()
	numbers.Start = 0
	if _, ds := ValidateDeclaration(Declaration{PageNumbers: numbers, Items: []Item{&PageItem{Count: 1}}}); len(ds) == 0 {
		t.Fatal("explicit zero numbering start defaulted silently")
	}
}

func TestTypedAggregateTextBudget(t *testing.T) {
	text := strings.Repeat("a", 10000)
	items := make([]Item, MaxAuthoredTextBytes/len(text)+1)
	for index := range items {
		items[index] = &PageItem{Count: 1, Style: PageStyle{Text: TextStyle{Value: Setting[string]{Present: true, Value: text}}}}
	}
	if _, ds := ValidateDeclaration(Declaration{Items: items}); len(ds) != 1 || ds[0].Code != diag.PlanLimit {
		t.Fatalf("aggregate authored text budget bypassed: %+v", ds)
	}
}

func TestAdmissionRejectsAllIdentityCollisions(t *testing.T) {
	for _, items := range [][]Item{
		{&GroupItem{Ref: "section", Title: "Section", Divider: &PageStyle{}, Items: []Item{&PageItem{Ref: "section/divider", Count: 1}}}},
		{&PageItem{Ref: "section/divider", Count: 1}, &GroupItem{Ref: "section", Title: "Section", Divider: &PageStyle{}, Items: []Item{&PageItem{Count: 1}}}},
		{&GroupItem{Ref: "outer", Items: []Item{&GroupItem{Ref: "nested", Title: "Nested", Divider: &PageStyle{}, Items: []Item{&PageItem{Ref: "nested/divider", Count: 1}}}}}},
		{&PageItem{Ref: "item/1/divider", Count: 1}, &GroupItem{Title: "Automatic", Divider: &PageStyle{}, Items: []Item{&PageItem{Count: 1}}}},
	} {
		if _, ds := AdmitDeclaration(Declaration{Items: items}); len(ds) == 0 {
			t.Fatalf("colliding physical identity admitted: %#v", items)
		}
	}
}
func TestEveryPreparedConstructionRevalidatesAndOwns(t *testing.T) {
	d, ds := ValidateDeclaration(Declaration{Items: []Item{&PageItem{Count: 1}}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	page, ok := d.Items[0].(*PageItem)
	if !ok {
		t.Fatal("expected admitted page declaration")
	}
	page.Count = -9
	if _, ds = AdmitDeclaration(d); len(ds) == 0 {
		t.Fatal("post-validation mutation bypassed admission")
	}
	page.Count = 1
	p, ds := AdmitDeclaration(d)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	page.Count = -10
	if p.Leaves()[0].Count != 1 {
		t.Fatal("admitted storage aliases input")
	}
	if _, ds = Resolve(Plan{}, nil, 0); len(ds) == 0 || diag.Outcome(ds) != diag.StatusFailed {
		t.Fatal("zero prepared state was plausible")
	}
}
