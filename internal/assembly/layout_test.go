// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// inheritanceRow is one case of the size-inheritance table.
type inheritanceRow struct {
	name  string
	items []assembly.Item
	want  []assembly.PageDim // size of each blank in input order
	from  []int              // contribution each blank inherits from, -1 when explicit
}

func geom(width, height float64) assembly.PageGeometry {
	return assembly.PageGeometry{Width: width, Height: height}
}

func source(pages int64, first, last assembly.PageGeometry) assembly.SourceGeometry {
	return assembly.SourceGeometry{Pages: pages, First: first, Last: last}
}

// lengthByte is the length of path modulo 256, for fonts told apart by the length of their path.
func lengthByte(path string) byte { return byte(len(path) & 0xff) }

// digestsByLength identifies each font by the length of its path, which is enough to tell test fonts apart.
func digestsByLength(path string) (assembly.FontDigest, bool) {
	return assembly.FontDigest{lengthByte(path)}, true
}

func sourcesNamed(names ...string) []assembly.Item {
	items := make([]assembly.Item, len(names))
	for index, name := range names {
		items[index] = pdfAt(assembly.Ref(index+1), name)
	}

	return items
}

func resolveJob(tb testing.TB, job *assembly.Job, sources ...assembly.SourceGeometry) *assembly.Layout {
	tb.Helper()

	layout, err := mustFlatten(tb, job).Resolve(sources, digestsByLength)
	if err != nil {
		tb.Fatalf("Resolve: %v", err)
	}

	return layout
}

// resolveNamed resolves a job whose sources are described by file name.
func resolveNamed(tb testing.TB, job *assembly.Job, byName map[string]assembly.SourceGeometry) *assembly.Layout {
	tb.Helper()

	flat := mustFlatten(tb, job)
	sources := make([]assembly.SourceGeometry, len(flat.Files))

	for index, file := range flat.Files {
		sources[index] = byName[filepath.Base(file.Path)]
	}

	layout, err := flat.Resolve(sources, digestsByLength)
	if err != nil {
		tb.Fatalf("Resolve: %v", err)
	}

	return layout
}

func dimOf(tb testing.TB, layout *assembly.Layout, contribution int) assembly.PageDim {
	tb.Helper()

	placement := layout.Placements[contribution]
	if placement.Spec < 0 {
		tb.Fatalf("contribution %d is not a blank", contribution)
	}

	return layout.Specs[placement.Spec].Spec.Dim
}

func inheritanceRows(tb testing.TB) []inheritanceRow {
	tb.Helper()

	inherit := assembly.BlankStyle{}
	explicit := sizeStyle(50, 60)

	rows := []inheritanceRow{
		{
			"leading takes first page of first source",
			[]assembly.Item{mustBlankItem(tb, 1, &inherit, 1), pdfAt(2, aPath)},
			[]assembly.PageDim{{Width: 100, Height: 200}},
			[]int{1},
		},
		{
			"trailing takes last page of preceding",
			[]assembly.Item{pdfAt(1, aPath), mustBlankItem(tb, 2, &inherit, 1)},
			[]assembly.PageDim{{Width: 300, Height: 400}},
			[]int{0},
		},
		{
			"middle takes preceding, not following",
			[]assembly.Item{pdfAt(1, aPath), mustBlankItem(tb, 2, &inherit, 1), pdfAt(3, bPath)},
			[]assembly.PageDim{{Width: 300, Height: 400}},
			[]int{0},
		},
		{
			"explicit blank does not change inheritance",
			[]assembly.Item{pdfAt(1, aPath), mustBlankItem(tb, 2, &explicit, 1), mustBlankItem(tb, 3, &inherit, 1)},
			[]assembly.PageDim{{Width: 50, Height: 60}, {Width: 300, Height: 400}},
			[]int{-1, 0},
		},
	}

	return append(rows, laterInheritanceRows(tb, &inherit, &explicit)...)
}

func laterInheritanceRows(tb testing.TB, inherit, explicit *assembly.BlankStyle) []inheritanceRow {
	tb.Helper()

	return []inheritanceRow{
		{
			"leading explicit then leading inherit",
			[]assembly.Item{mustBlankItem(tb, 1, explicit, 1), mustBlankItem(tb, 2, inherit, 2), pdfAt(3, bPath)},
			[]assembly.PageDim{{Width: 50, Height: 60}, {Width: 10, Height: 20}},
			[]int{-1, 2},
		},
		{
			"second source supplies later blanks",
			[]assembly.Item{pdfAt(1, aPath), pdfAt(2, bPath), mustBlankItem(tb, 3, inherit, 1)},
			[]assembly.PageDim{{Width: 30, Height: 40}},
			[]int{1},
		},
		{
			"nested groups inherit like flat items",
			[]assembly.Item{
				mustBlankItem(tb, 1, inherit, 1),
				assembly.NewGroupItem(assembly.Origin{Ref: 2}, "g", []assembly.Item{
					assembly.NewGroupItem(assembly.Origin{Ref: 3}, "h", []assembly.Item{pdfAt(4, aPath)}),
					mustBlankItem(tb, 5, inherit, 1),
				}),
			},
			[]assembly.PageDim{{Width: 100, Height: 200}, {Width: 300, Height: 400}},
			[]int{1, 1},
		},
	}
}

func TestResolveInheritance(t *testing.T) {
	t.Parallel()

	twoPages := source(2, geom(100, 200), geom(300, 400))
	onePage := source(1, geom(10, 20), geom(30, 40))

	for _, row := range inheritanceRows(t) {
		layout := resolveNamed(t, argumentJob(row.items...), map[string]assembly.SourceGeometry{aPath: twoPages, bPath: onePage})

		blank := 0

		for index, contribution := range layout.Flat.Contributions {
			if contribution.Kind != assembly.ItemBlank {
				continue
			}

			if got := dimOf(t, layout, index); got != row.want[blank] {
				t.Errorf("%s: blank %d is %v, want %v", row.name, blank, got, row.want[blank])
			}

			if got := layout.Placements[index].SizeFrom; got != row.from[blank] {
				t.Errorf("%s: blank %d inherits from %d, want %d", row.name, blank, got, row.from[blank])
			}

			blank++
		}
	}
}

func TestResolveSizeFromKinds(t *testing.T) {
	t.Parallel()

	job := argumentJob(
		mustBlankItem(t, 1, &assembly.BlankStyle{}, 1), pdfAt(2, aPath),
		mustBlankItem(t, 3, &assembly.BlankStyle{}, 1), mustBlankItem(t, 4, &[]assembly.BlankStyle{sizeStyle(5, 5)}[0], 1),
	)
	layout := resolveJob(t, job, source(1, geom(10, 10), geom(20, 20)))

	got := []assembly.SizeSource{
		layout.Placements[0].Size, layout.Placements[1].Size, layout.Placements[2].Size, layout.Placements[3].Size,
	}

	want := []assembly.SizeSource{assembly.SizeFromFollowingFirst, 0, assembly.SizeFromPrecedingLast, assembly.SizeExplicit}

	for index := range want {
		if got[index] != want[index] {
			t.Errorf("placement %d Size = %d, want %d", index, got[index], want[index])
		}
	}

	if layout.Placements[1].Spec != -1 || layout.Placements[1].SizeFrom != -1 {
		t.Errorf("a PDF has no spec: %+v", layout.Placements[1])
	}
}

func TestResolveAllGeneratedAndUnresolved(t *testing.T) {
	t.Parallel()

	explicit := sizeStyle(595, 842)
	layout := resolveJob(t, argumentJob(mustBlankItem(t, 1, &explicit, 4)))

	if layout.Totals != (assembly.Totals{Generated: 4, Total: 4}) || len(layout.Specs) != 1 || len(layout.Flat.Files) != 0 {
		t.Errorf(detailedValueFormat, layout.Totals)
	}

	job := argumentJob(
		mustBlankItem(t, 1, &explicit, 1),
		mustBlankItem(t, 2, &assembly.BlankStyle{}, 1),
		mustBlankItem(t, 3, &assembly.BlankStyle{}, 1),
	)

	_, err := mustFlatten(t, job).Resolve(nil, digestsByLength)
	if got := codesOf(err); len(got) != 2 || got[0] != assembly.CodeSizeUnresolved || got[1] != assembly.CodeSizeUnresolved {
		t.Fatalf("%v", err)
	}

	list := assembly.Diagnostics(err)
	located := list[0].Location.Pointer == "argv:2" && list[1].Location.Pointer == argumentThree

	if !located || !strings.Contains(list[0].Message, "give the blank an explicit size") {
		t.Errorf(detailedValueFormat, list[0])
	}

	if !errors.Is(err, assembly.ErrUnresolvedSize) {
		t.Error("cause lost")
	}
}

func TestResolveInheritedSizeMustBeSupported(t *testing.T) {
	t.Parallel()

	explicit := sizeStyle(50, 50)

	for _, bad := range []assembly.PageGeometry{
		geom(14401, 100), geom(100, 20000), geom(0, 10), geom(10, -1), geom(math.NaN(), 10), geom(10, math.Inf(1)), geom(0.5, 10),
	} {
		job := argumentJob(pdfAt(1, "big.pdf"), mustBlankItem(t, 2, &assembly.BlankStyle{}, 1), mustBlankItem(t, 3, &explicit, 1))

		_, err := mustFlatten(t, job).Resolve([]assembly.SourceGeometry{source(1, bad, bad)}, digestsByLength)
		codes := codesOf(err)

		if len(codes) != 1 || codes[0] != assembly.CodeSizeOutOfRange || !strings.Contains(err.Error(), "give an explicit size") {
			t.Errorf("%+v: %v", bad, err)
		}

		// The same source page is preserved when nothing inherits from it, and an explicit size is unaffected.
		kept := argumentJob(pdfAt(1, "big.pdf"), mustBlankItem(t, 3, &explicit, 1))
		if layout := resolveJob(t, kept, source(1, bad, bad)); layout.Totals.Total != 2 {
			t.Errorf("%+v: %+v", bad, layout.Totals)
		}
	}

	// The limits themselves are valid.
	for _, edge := range []assembly.PageGeometry{geom(1, 1), geom(14400, 14400)} {
		layout := resolveJob(t, argumentJob(pdfAt(1, aPath), mustBlankItem(t, 2, &assembly.BlankStyle{}, 1)), source(1, edge, edge))
		if got := dimOf(t, layout, 1); float64(got.Width) != edge.Width {
			t.Errorf("edge %+v gave %v", edge, got)
		}
	}
}

func TestResolveInputChecks(t *testing.T) {
	t.Parallel()

	job := argumentJob(pdfAt(1, aPath), pdfAt(2, bPath))
	flat := mustFlatten(t, job)

	_, err := flat.Resolve([]assembly.SourceGeometry{source(1, geom(1, 1), geom(1, 1))}, digestsByLength)
	if !errors.Is(err, assembly.ErrSourceGeometry) {
		t.Errorf("mismatch: %v", err)
	}

	good := source(1, geom(1, 1), geom(1, 1))

	_, err = flat.Resolve([]assembly.SourceGeometry{good, source(0, geom(1, 1), geom(1, 1))}, digestsByLength)
	codes := codesOf(err)

	if len(codes) != 1 || codes[0] != assembly.CodeSourceEmpty || assembly.Diagnostics(err)[0].Location.Pointer != "argv:2" {
		t.Errorf("empty source: %v", err)
	}

	blank := argumentJob(mustBlankItem(t, 1, &[]assembly.BlankStyle{sizeStyle(10, 10)}[0], 1))

	_, err = mustFlatten(t, blank).Resolve(nil, func(string) (assembly.FontDigest, bool) { return assembly.FontDigest{}, false })
	if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodeFontUnavailable {
		t.Errorf("missing font identity: %v", err)
	}
}

func TestResolveInvalidStyleAfterLayering(t *testing.T) {
	t.Parallel()

	// An anchor outside the grid passes the job's value checks and is caught when the spec is validated.
	style := assembly.BlankStyle{
		Size: assembly.Set(assembly.PageSize{Dim: assembly.PageDim{Width: 10, Height: 10}}, itemOrigin()),
		Text: assembly.TextStyle{Anchor: assembly.Set(assembly.Anchor(77), itemOrigin())},
	}

	_, err := mustFlatten(t, argumentJob(mustBlankItem(t, 1, &style, 1))).Resolve(nil, digestsByLength)
	if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodeStyleInvalid || !errors.Is(err, assembly.ErrUnknownName) {
		t.Errorf("%v", err)
	}
}

func TestResolveArithmeticBoundaries(t *testing.T) {
	t.Parallel()

	unit := geom(10, 10)
	explicit := sizeStyle(10, 10)
	huge := func(pages int64) assembly.SourceGeometry { return source(pages, unit, unit) }

	ok := []struct {
		name  string
		items []assembly.Item
		pages []assembly.SourceGeometry
		total int64
	}{
		{"one source of the maximum", sourcesNamed(aPath), []assembly.SourceGeometry{huge(math.MaxInt32)}, math.MaxInt32},
		{
			"source plus the maximum generated pages",
			[]assembly.Item{pdfAt(1, aPath), mustBlankItem(t, 2, &explicit, assembly.MaxBlankCount)},
			[]assembly.SourceGeometry{huge(math.MaxInt32 - assembly.MaxBlankCount)},
			math.MaxInt32,
		},
	}

	for _, row := range ok {
		layout := resolveJob(t, argumentJob(row.items...), row.pages...)
		if layout.Totals.Total != row.total {
			t.Errorf("%s: total %d", row.name, layout.Totals.Total)
		}
	}

	bad := []struct {
		name  string
		items []assembly.Item
		pages []assembly.SourceGeometry
	}{
		{"one source past the boundary", sourcesNamed(aPath), []assembly.SourceGeometry{huge(math.MaxInt32 + 1)}},
		{"a source of the maximum used twice", sourcesNamed(aPath, aPath), []assembly.SourceGeometry{huge(math.MaxInt32)}},
		{
			"generated pages tip the total over",
			[]assembly.Item{pdfAt(1, aPath), mustBlankItem(t, 2, &explicit, assembly.MaxBlankCount)},
			[]assembly.SourceGeometry{huge(math.MaxInt32 - assembly.MaxBlankCount + 1)},
		},
		{"int64 overflow", sourcesNamed(aPath, bPath), []assembly.SourceGeometry{huge(math.MaxInt64), huge(math.MaxInt64)}},
	}

	for _, row := range bad {
		_, err := mustFlatten(t, argumentJob(row.items...)).Resolve(row.pages, digestsByLength)
		if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodePageTotalExceeded || !errors.Is(err, assembly.ErrOutOfRange) {
			t.Errorf(namedFailureFormat, row.name, err)
		}
	}
}

func TestResolveSpecTableRunsAndOrigins(t *testing.T) {
	t.Parallel()

	threePages := source(3, geom(10, 10), geom(10, 10))
	twoPages := source(2, geom(20, 20), geom(20, 20))
	hi := textStyle("hi")
	items := []assembly.Item{
		mustBlankItem(t, 1, &hi, 2), mustBlankItem(t, 2, &hi, 3), // merge into one run of 5 (same spec)
		pdfAt(3, aPath), mustBlankItem(t, 4, &hi, 1), pdfAt(5, aPath), pdfAt(6, bPath),
		mustBlankItem(t, 7, &hi, 1), // 20x20: a different spec from the 10x10 ones
		mustBlankItem(t, 8, &assembly.BlankStyle{}, 1),
	}

	layout := resolveNamed(t, argumentJob(items...), map[string]assembly.SourceGeometry{aPath: threePages, bPath: twoPages})

	if layout.Totals != (assembly.Totals{Source: 8, Generated: 8, Total: 16}) {
		t.Fatalf("totals %+v", layout.Totals)
	}

	checkPlacementRanges(t, layout)
	checkRuns(t, layout)

	// Specs: "hi" at the leading blank's 10x10, "hi" at 20x20, empty text at 20x20.
	if len(layout.Specs) != 3 || layout.Specs[0].Uses != 3 || layout.Specs[1].Uses != 1 {
		t.Fatalf("specs %+v", layout.Specs)
	}

	if got := layout.Specs[0].Origins; len(got) != 3 || got[0].Ref != 1 || got[1].Ref != 2 || got[2].Ref != 4 {
		t.Errorf("origins in input order: %+v", got)
	}
}

func checkPlacementRanges(t *testing.T, layout *assembly.Layout) {
	t.Helper()

	wantRanges := []assembly.PageRange{{1, 2, 2}, {3, 5, 3}, {6, 8, 3}, {9, 9, 1}, {10, 12, 3}, {13, 14, 2}, {15, 15, 1}, {16, 16, 1}}
	for index, want := range wantRanges {
		if got := layout.Placements[index].Range; got != want {
			t.Errorf("range %d = %+v, want %+v", index, got, want)
		}
	}
}

func checkRuns(t *testing.T, layout *assembly.Layout) {
	t.Helper()

	wantRuns := []assembly.Run{
		{Kind: assembly.RunGenerated, Index: 0, Count: 5},
		{Kind: assembly.RunSource, Index: 0, FirstPage: 1, Count: 3},
		{Kind: assembly.RunGenerated, Index: 0, Count: 1},
		{Kind: assembly.RunSource, Index: 0, FirstPage: 1, Count: 3},
		{Kind: assembly.RunSource, Index: 1, FirstPage: 1, Count: 2},
		{Kind: assembly.RunGenerated, Index: 1, Count: 1},
		{Kind: assembly.RunGenerated, Index: 2, Count: 1},
	}

	if len(layout.Runs) != len(wantRuns) {
		t.Fatalf("runs %+v", layout.Runs)
	}

	for index, want := range wantRuns {
		if layout.Runs[index] != want {
			t.Errorf("run %d = %+v, want %+v", index, layout.Runs[index], want)
		}
	}
}

func TestResolveSpecIdentityIncludesFontContent(t *testing.T) {
	t.Parallel()

	font := func(file string) assembly.BlankStyle {
		f, err := assembly.FontFile(file, hostPath("/f"))
		if err != nil {
			t.Fatal(err)
		}

		style := sizeStyle(10, 10)
		style.Text.Font = assembly.Set(f, itemOrigin())

		return style
	}

	one, two := font("a.ttf"), font("b.ttf")
	job := argumentJob(mustBlankItem(t, 1, &one, 1), mustBlankItem(t, 2, &two, 1), mustBlankItem(t, 3, &one, 1))

	same := func(string) (assembly.FontDigest, bool) { return assembly.FontDigest{1}, true }

	layout, err := mustFlatten(t, job).Resolve(nil, same)
	if err != nil || len(layout.Specs) != 2 {
		t.Fatalf("different paths stay distinct even with equal content: %v %d", err, len(layout.Specs))
	}

	byPath := func(path string) (assembly.FontDigest, bool) {
		return assembly.FontDigest{lengthByte(path), path[len(path)-5]}, true
	}

	layout, err = mustFlatten(t, job).Resolve(nil, byPath)
	if err != nil || layout.Specs[0].FontDigest == layout.Specs[1].FontDigest || layout.Specs[0].FontDigest[1] != 'a' {
		t.Errorf("each spec carries the identity of its font: %v", err)
	}
}

func TestResolveRecordsAtMostRelatedOrigins(t *testing.T) {
	t.Parallel()

	explicit := sizeStyle(10, 10)

	items := make([]assembly.Item, 0, 30)
	for ref := range 30 {
		items = append(items, mustBlankItem(t, assembly.Ref(ref), &explicit, 1))
	}

	layout := resolveJob(t, argumentJob(items...))
	spec := layout.Specs[0]

	if spec.Uses != 30 || len(spec.Origins) != assembly.MaxRelated+1 || spec.Origins[0].Ref != 0 {
		t.Errorf("uses %d origins %d", spec.Uses, len(spec.Origins))
	}

	diagnostic := assembly.NewSharedError(layout.Source, spec.Origins, "/blank", assembly.Error{Message: "m", Affected: spec.Uses})

	pointer := diagnostic.Location.Pointer
	if len(diagnostic.Related) != assembly.MaxRelated || diagnostic.Affected != 30 || pointer != "argv:0/blank" && pointer != "argv:0" {
		t.Errorf(detailedValueFormat, diagnostic)
	}
}
