// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestArgumentSource(t *testing.T) {
	t.Parallel()

	location := assembly.Locate(assembly.ArgumentSource{}, assembly.Origin{Ref: 4}, "/ignored")

	if location.Source != "argv" || location.Pointer != "argv:4" || location.Line != 0 || location.Column != 0 || location.Offset != 0 {
		t.Errorf(detailedValueFormat, location)
	}

	if got := location.String(); got != "argv at argv:4" {
		t.Errorf(stringValueFormat, got)
	}

	if got := (assembly.Location{Source: "p.json", Offset: 7, Line: 2, Column: 3}).String(); got != "p.json:2:3 (byte 7)" {
		t.Errorf(stringValueFormat, got)
	}
}

func TestCheckBlankCount(t *testing.T) {
	t.Parallel()

	for _, count := range []int64{1, 2, 999_999, 1_000_000} {
		err := assembly.CheckBlankCount(count)
		if err != nil {
			t.Errorf("CheckBlankCount(%d): %v", count, err)
		}
	}

	for _, count := range []int64{math.MinInt64, -1, 0, 1_000_001, math.MaxInt64} {
		if !errors.Is(assembly.CheckBlankCount(count), assembly.ErrOutOfRange) {
			t.Errorf("CheckBlankCount(%d) accepted", count)
		}
	}
}

func TestAddCountsAcceptsSumsWithinTheLimit(t *testing.T) {
	t.Parallel()

	for _, row := range []struct{ a, b, limit, want int64 }{
		{0, 0, 10, 0}, {3, 4, 10, 7}, {5, 5, 10, 10}, {0, 10, 10, 10}, {
			10,
			0, 10, 10,
		}, {math.MaxInt64 - 1, 1, math.MaxInt64, math.MaxInt64},
	} {
		got, err := assembly.AddCounts(row.a, row.b, row.limit)
		if err != nil || got != row.want {
			t.Errorf("AddCounts(%d,%d,%d) = %d, %v", row.a, row.b, row.limit, got, err)
		}
	}
}

func TestAddCountsRejectsOverflowAndNegatives(t *testing.T) {
	t.Parallel()

	for _, row := range []struct{ a, b, limit int64 }{
		{5, 6, 10},
		{11, 0, 10},
		{0, 11, 10},
		{-1, 5, 10},
		{5, -1, 10},
		{math.MaxInt64, 1, math.MaxInt64},
		{1, math.MaxInt64, math.MaxInt64},
		{math.MaxInt64, math.MaxInt64, math.MaxInt64},
	} {
		got, err := assembly.AddCounts(row.a, row.b, row.limit)
		if !errors.Is(err, assembly.ErrOutOfRange) {
			t.Errorf("AddCounts(%d,%d,%d) = %d, %v; want an error", row.a, row.b, row.limit, got, err)
		}
	}
}

func TestPDFItemConstructor(t *testing.T) {
	t.Parallel()

	pdf := pdfAt(2, aPath)

	if pdf.Kind != assembly.ItemPDF || pdf.Path != aPath || pdf.Origin.Ref != 2 || pdf.Kind.String() != "pdf" {
		t.Errorf(detailedValueFormat, pdf)
	}
}

func TestBlankItemConstructor(t *testing.T) {
	t.Parallel()

	blank := mustBlankItem(t, 3, &assembly.BlankStyle{}, 5)

	if blank.Kind != assembly.ItemBlank || blank.Blank.Count != 5 || blank.Blank.CountOrigin.Ref != 3 || blank.Kind.String() != "blank" {
		t.Errorf(detailedValueFormat, blank)
	}
}

func TestGroupItemConstructor(t *testing.T) {
	t.Parallel()

	group := assembly.NewGroupItem(assembly.Origin{Ref: 4}, "dir", []assembly.Item{pdfAt(2, aPath)})
	holdsItem := group.Dir.Value == "dir" && group.Dir.IsSet() && len(group.Items) == 1

	if group.Kind != assembly.ItemGroup || !holdsItem || group.Kind.String() != "group" {
		t.Errorf(detailedValueFormat, group)
	}
}

func TestNewBlankItemChecksTheCount(t *testing.T) {
	t.Parallel()

	for _, count := range []int64{0, -3, 1_000_001} {
		_, err := assembly.NewBlankItem(assembly.Origin{}, &assembly.BlankStyle{}, count)
		if !errors.Is(err, assembly.ErrOutOfRange) {
			t.Errorf("NewBlankItem count %d = %v", count, err)
		}
	}
}

func TestItemsBase(t *testing.T) {
	t.Parallel()

	withDir := &assembly.Job{Base: "/b", Dir: assembly.Set("src", assembly.Origin{})}
	withoutDir := &assembly.Job{Base: "/b"}

	if withDir.ItemsBase() != "/b/src" || withoutDir.ItemsBase() != "/b" {
		t.Errorf("ItemsBase() = %q, %q", withDir.ItemsBase(), withoutDir.ItemsBase())
	}
}

func TestValidateAcceptsWellFormedJobs(t *testing.T) {
	t.Parallel()

	nested := assembly.NewGroupItem(
		assembly.Origin{Ref: 2},
		"g",
		[]assembly.Item{pdfAt(3, bPath), mustBlankItem(t, 4, &assembly.BlankStyle{}, 2)},
	)
	job := argumentJob(pdfAt(0, aPath), mustBlankItem(t, 1, &assembly.BlankStyle{}, 999_998), nested)

	err := job.Validate()
	if err != nil {
		t.Fatal(err)
	}

	// An all-generated job is valid: size resolution is a later concern.
	err = argumentJob(mustBlankItem(t, 0, &assembly.BlankStyle{}, 1)).Validate()
	if err != nil {
		t.Fatal(err)
	}
}

func manyPDFs(count int) []assembly.Item {
	items := make([]assembly.Item, count)
	for index := range items {
		items[index] = pdfAt(assembly.Ref(index), aPath)
	}

	return items
}

// deepGroups nests depth groups around one PDF.
func deepGroups(depth int) assembly.Item {
	item := pdfAt(0, leafPath)
	for range depth {
		item = assembly.NewGroupItem(assembly.Origin{}, "d", []assembly.Item{item})
	}

	return item
}

func blankWith(tb testing.TB, style *assembly.BlankStyle) assembly.Item {
	tb.Helper()

	return mustBlankItem(tb, 0, style, 1)
}

// malformedJobs are jobs that Validate must reject, by description.
func malformedJobs(tb testing.TB) map[string]*assembly.Job {
	tb.Helper()

	good := pdfAt(0, aPath)
	bigBlank := mustBlankItem(tb, 1, &assembly.BlankStyle{}, 600_000)
	nan := assembly.Set(assembly.Length(math.NaN()), assembly.Origin{})
	badSize := assembly.PageSize{Dim: assembly.PageDim{Width: assembly.Length(math.NaN()), Height: 5}}

	return map[string]*assembly.Job{
		"no source":          {Items: []assembly.Item{good}},
		"no items":           argumentJob(),
		"empty path":         argumentJob(pdfAt(0, "")),
		"NUL path":           argumentJob(pdfAt(0, "a\x00")),
		"NUL group dir":      argumentJob(assembly.NewGroupItem(assembly.Origin{}, "a\x00", []assembly.Item{good})),
		"empty group":        argumentJob(assembly.NewGroupItem(assembly.Origin{}, "g", nil)),
		"unknown kind":       argumentJob(assembly.Item{Kind: 0}),
		"blank without body": argumentJob(assembly.Item{Kind: assembly.ItemBlank}),
		"blank count 0":      argumentJob(assembly.Item{Kind: assembly.ItemBlank, Blank: &assembly.BlankItem{Count: 0}}),
		"blank count huge":   argumentJob(assembly.Item{Kind: assembly.ItemBlank, Blank: &assembly.BlankItem{Count: 1_000_001}}),
		"generated overflow": argumentJob(bigBlank, bigBlank),
		"blank NaN length":   argumentJob(blankWith(tb, &assembly.BlankStyle{Text: assembly.TextStyle{X: nan}})),
		"blank NaN size":     argumentJob(blankWith(tb, &assembly.BlankStyle{Size: assembly.Set(badSize, assembly.Origin{})})),
		"blank NaN leading": argumentJob(
			blankWith(tb, &assembly.BlankStyle{Text: assembly.TextStyle{Leading: assembly.Set(math.NaN(), assembly.Origin{})}}),
		),
		"blank leading 11": argumentJob(
			blankWith(tb, &assembly.BlankStyle{Text: assembly.TextStyle{Leading: assembly.Set(11.0, assembly.Origin{})}}),
		),
		"blank long text": argumentJob(
			blankWith(
				tb,
				&assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Set(strings.Repeat("a", 10001), assembly.Origin{})}},
			),
		),
		"too many contribs": argumentJob(manyPDFs(assembly.MaxContributions + 1)...),
		"too deep":          argumentJob(deepGroups(assembly.MaxGroupDepth + 1)),
		"deep inside a group": argumentJob(
			assembly.NewGroupItem(assembly.Origin{}, "x", []assembly.Item{deepGroups(assembly.MaxGroupDepth)}),
		),
	}
}

func TestValidateRejectsMalformedJobs(t *testing.T) {
	t.Parallel()

	for name, job := range malformedJobs(t) {
		err := job.Validate()
		if err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestValidateErrorsAreClassified(t *testing.T) {
	t.Parallel()

	for name, job := range map[string]*assembly.Job{
		"no items":        argumentJob(),
		"too many":        argumentJob(manyPDFs(assembly.MaxContributions + 1)...),
		"unknown kind":    argumentJob(assembly.Item{Kind: 0}),
		"deeper than 64":  argumentJob(deepGroups(assembly.MaxGroupDepth + 1)),
		"count too large": argumentJob(assembly.Item{Kind: assembly.ItemBlank, Blank: &assembly.BlankItem{Count: 1_000_001}}),
	} {
		err := job.Validate()
		if !errors.Is(err, assembly.ErrInvalidJob) && !errors.Is(err, assembly.ErrOutOfRange) {
			t.Errorf("%s: %v is not classified", name, err)
		}
	}
}

func TestValidateAcceptsTheDeclaredBounds(t *testing.T) {
	t.Parallel()

	empty := &assembly.BlankStyle{}

	for name, job := range map[string]*assembly.Job{
		"exact contributions": argumentJob(manyPDFs(assembly.MaxContributions)...),
		"exact depth":         argumentJob(deepGroups(assembly.MaxGroupDepth)),
		"exact generated":     argumentJob(mustBlankItem(t, 0, empty, 500_000), mustBlankItem(t, 1, empty, 500_000)),
	} {
		err := job.Validate()
		if err != nil {
			t.Errorf(namedFailureFormat, name, err)
		}
	}
}

func TestValidateChargesStructuralNodes(t *testing.T) {
	t.Parallel()

	// Every blank item with a text and font object is four nodes; 62501 of them exceed the 250000 bound while
	// staying below the contribution bound.
	style := assembly.BlankStyle{Text: assembly.TextStyle{Font: assembly.Set(assembly.Font{File: fontFile}, assembly.Origin{})}}

	build := func(count int) *assembly.Job {
		items := make([]assembly.Item, count)
		for index := range items {
			items[index] = mustBlankItem(t, 0, &style, 1)
		}

		return argumentJob(items...)
	}

	err := build(62500).Validate()
	if err != nil {
		t.Fatalf("exactly the bound: %v", err)
	}

	err = build(62501).Validate()
	if !errors.Is(err, assembly.ErrInvalidJob) || !strings.Contains(err.Error(), "structural nodes") {
		t.Fatalf("one over the bound: %v", err)
	}

	// The defaults object counts as well.
	job := build(62499)
	job.Defaults = style

	err = job.Validate()
	if err != nil {
		t.Fatalf("defaults within the bound: %v", err)
	}

	job.Items = append(job.Items, build(1).Items...)

	err = job.Validate()
	if err == nil {
		t.Fatal("the defaults object must be charged")
	}
}
