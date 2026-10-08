// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

type (
	// chooser returns a number in [0, n).
	chooser func(n int) int

	// outputPage is one page of the brute-force expansion of a generated tree.
	outputPage struct {
		file string // source file path; empty for a generated page
		text string // generated pages: the text that identifies the style
		dim  assembly.PageDim
		page int64 // source pages: 1-based page number
	}

	// generated is a random job together with its brute-force expansion.
	generated struct {
		job       *assembly.Job
		expansion []outputPage // generated pages have dim set only when explicit
		inherit   []bool       // generated pages that inherit their size
		big       bool
		sum       int64 // the sum of the blank counts
	}
)

const (
	genFiles    = 4
	genMaxDepth = 3
	genRounds   = 300
)

// hashedChooser is a deterministic chooser: each call hashes a counter, so the sequence is fixed by
// the seed alone and needs no random-number generator.
func hashedChooser(seed uint64) chooser {
	counter := seed

	return func(n int) int {
		counter++

		sum := sha256.Sum256([]byte(strconv.FormatUint(counter, 10)))

		return int(sum[0]) % n
	}
}

// String lists every field so a failing comparison shows the whole page.
func (p outputPage) String() string {
	return fmt.Sprintf("{file %q, text %q, dim %v, page %d}", p.file, p.text, p.dim, p.page)
}

func fileGeometry(index int) assembly.SourceGeometry {
	first := geom(float64(100+index*10), float64(200+index))
	pages := int64(index%3 + 1)

	last := first
	if pages > 1 {
		last = geom(float64(101+index*10), float64(201+index))
	}

	return assembly.SourceGeometry{Pages: pages, First: first, Last: last}
}

func fileName(index int) string { return "f" + strconv.Itoa(index) + ".pdf" }

// generate builds a random job. With big set, blank counts may be large and the expansion is not kept.
func generate(tb testing.TB, choose chooser, big bool) *generated {
	tb.Helper()

	result := &generated{big: big}
	next := assembly.Ref(0)

	items := result.items(tb, choose, 0, hostPath(workingDirectory), &next)
	result.job = argumentJob(items...)
	result.job.Base = hostPath(workingDirectory)

	return result
}

func (g *generated) items(tb testing.TB, choose chooser, depth int, base string, next *assembly.Ref) []assembly.Item {
	tb.Helper()

	items := make([]assembly.Item, 1+choose(4))
	for index := range items {
		*next++
		ref := *next

		switch kind := choose(5); {
		case kind == 0 && depth < genMaxDepth:
			dir := []string{"g", "../h", "", "x y"}[choose(4)]
			items[index] = assembly.NewGroupItem(assembly.Origin{Ref: ref}, dir, nil)
			items[index].Items = g.items(tb, choose, depth+1, filepath.Join(base, dir), next)
		case kind <= 2:
			file := choose(genFiles)
			items[index] = pdfAt(ref, fileName(file))

			info := fileGeometry(file)
			for page := range info.Pages {
				g.expansion = append(g.expansion, outputPage{file: filepath.Join(base, fileName(file)), page: page + 1})
				g.inherit = append(g.inherit, false)
			}
		default:
			items[index] = g.blank(tb, choose, ref)
		}
	}

	return items
}

func (g *generated) blank(tb testing.TB, choose chooser, ref assembly.Ref) assembly.Item {
	tb.Helper()

	var style assembly.BlankStyle

	explicit := choose(3) == 0
	if explicit {
		style = sizeStyle(50, 60)
	}

	text := "t" + strconv.Itoa(choose(3))
	style.Text.Value = assembly.Set(text, itemOrigin())

	count := int64(1 + choose(4))
	if g.big && choose(6) == 0 {
		count = assembly.MaxBlankCount
	}

	g.sum += count

	if !g.big {
		for range count {
			page := outputPage{text: text}
			if explicit {
				page.dim = assembly.PageDim{Width: 50, Height: 60}
			}

			g.expansion = append(g.expansion, page)
			g.inherit = append(g.inherit, !explicit)
		}
	}

	return mustBlankItem(tb, ref, &style, count)
}

// bruteForceSizes fills the inherited sizes by scanning, page by page, for the nearest source page before
// the page, or else the first source page after it.
func (g *generated) bruteForceSizes() {
	for index := range g.expansion {
		if !g.inherit[index] {
			continue
		}

		for before := index - 1; before >= 0; before-- {
			if g.expansion[before].file != "" {
				g.expansion[index].dim = pageDim(g.expansion[before])

				break
			}
		}

		if g.expansion[index].dim != (assembly.PageDim{}) {
			continue
		}

		for after := index + 1; after < len(g.expansion); after++ {
			if g.expansion[after].file != "" {
				g.expansion[index].dim = pageDim(g.expansion[after])

				break
			}
		}
	}
}

// cannotInherit reports whether some page inherits its size while the output has no source page.
func (g *generated) cannotInherit() bool {
	hasSource, hasInherit := false, false

	for index, page := range g.expansion {
		hasSource = hasSource || page.file != ""
		hasInherit = hasInherit || g.inherit[index]
	}

	return hasInherit && !hasSource
}

// pageDim is the geometry of the source page: the first page of its file or, for a later page, the last.
func pageDim(page outputPage) assembly.PageDim {
	var index int

	for candidate := range genFiles {
		if filepath.Base(page.file) == fileName(candidate) {
			index = candidate
		}
	}

	info := fileGeometry(index)

	size := info.Last
	if page.page == 1 {
		size = info.First
	}

	return assembly.PageDim{Width: assembly.Length(size.Width), Height: assembly.Length(size.Height)}
}

func resolveGenerated(tb testing.TB, flat *assembly.Flattened) (*assembly.Layout, error) {
	tb.Helper()

	sources := make([]assembly.SourceGeometry, len(flat.Files))

	for index, file := range flat.Files {
		for candidate := range genFiles {
			if filepath.Base(file.Path) == fileName(candidate) {
				sources[index] = fileGeometry(candidate)
			}
		}
	}

	return flat.Resolve(sources, digestsByLength)
}

func TestResolveMatchesBruteForceExpansion(t *testing.T) {
	t.Parallel()

	choose := hashedChooser(1)

	for round := range genRounds {
		gen := generate(t, choose, false)
		gen.bruteForceSizes()

		layout, err := resolveGenerated(t, mustFlatten(t, gen.job))
		if gen.cannotInherit() {
			if got := codesOf(err); len(got) == 0 || got[0] != assembly.CodeSizeUnresolved {
				t.Fatalf("round %d: a blank inheriting with no source page must fail: %v", round, err)
			}

			continue
		}

		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}

		checkAgainstExpansion(t, round, layout, gen.expansion)
	}
}

func checkAgainstExpansion(t *testing.T, round int, layout *assembly.Layout, want []outputPage) {
	t.Helper()

	if layout.Totals.Total != int64(len(want)) || layout.Totals.Source+layout.Totals.Generated != layout.Totals.Total {
		t.Fatalf("round %d: totals %+v for %d pages", round, layout.Totals, len(want))
	}

	checkRangesTile(t, round, layout, int64(len(want)))

	// Page by page, from the ranges.
	for index := range layout.Flat.Contributions {
		placement := layout.Placements[index]

		for page := placement.Range.Start; page <= placement.Range.End; page++ {
			got := describePage(layout, index, page)
			if got != want[page-1] {
				t.Fatalf("round %d: page %d = %+v, want %+v", round, page, got, want[page-1])
			}
		}
	}

	pages := pagesFromRuns(layout)
	if !slices.Equal(pages, want) {
		t.Fatalf("round %d: runs expand to %+v, want %+v", round, pages, want)
	}
}

// checkRangesTile verifies that placement ranges tile the output: each starts where the previous
// ended, and PDFs and blanks cover their pages.
func checkRangesTile(t *testing.T, round int, layout *assembly.Layout, total int64) {
	t.Helper()

	var end int64

	for index, placement := range layout.Placements {
		span := placement.Range
		if span.Start != end+1 || span.Count != span.End-span.Start+1 || span.Count < 1 {
			t.Fatalf("round %d: range %d = %+v after %d", round, index, span, end)
		}

		end = span.End
	}

	if end != total {
		t.Fatalf("round %d: ranges end at %d, want %d", round, end, total)
	}
}

// pagesFromRuns expands the layout's runs into one outputPage per page.
func pagesFromRuns(layout *assembly.Layout) []outputPage {
	var pages []outputPage

	for _, run := range layout.Runs {
		for offset := range run.Count {
			if run.Kind == assembly.RunSource {
				pages = append(pages, outputPage{file: layout.Flat.Files[run.Index].Path, page: run.FirstPage + offset})

				continue
			}

			spec := layout.Specs[run.Index].Spec
			pages = append(pages, outputPage{text: spec.Text.Value, dim: spec.Dim})
		}
	}

	return pages
}

func describePage(layout *assembly.Layout, index int, page int64) outputPage {
	contribution, placement := &layout.Flat.Contributions[index], layout.Placements[index]
	if contribution.Kind == assembly.ItemPDF {
		return outputPage{file: contribution.Path, page: page - placement.Range.Start + 1}
	}

	spec := layout.Specs[placement.Spec].Spec

	return outputPage{text: spec.Text.Value, dim: spec.Dim}
}

func FuzzFlatten(f *testing.F) {
	seeds := [][]byte{
		nil,
		{0},
		{1, 2, 3, 4, 5, 6, 7},
		{0, 0, 0, 0, 9, 9, 9, 1, 1, 1, 4, 4},
		[]byte("flatten-seed-with-groups-and-blanks"),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		position := 0
		choose := func(n int) int {
			if position >= len(data) {
				return 0
			}

			position++

			return int(data[position-1]) % n
		}

		checkFlattenedTree(t, generate(t, choose, true))
	})
}

// checkFlattenedTree flattens and resolves a generated tree and checks the outcome for its limits.
func checkFlattenedTree(t *testing.T, gen *generated) {
	t.Helper()

	flat, err := assembly.Flatten(gen.job)
	if gen.sum > assembly.MaxGeneratedPages {
		if got := codesOf(err); len(got) != 1 || got[0] != assembly.CodeInvalidJob {
			t.Fatalf("%d generated pages are over the limit: %v", gen.sum, err)
		}

		return
	}

	if err != nil {
		t.Fatalf("a tree within the limits must flatten: %v", err)
	}

	again, err := assembly.Flatten(gen.job)
	if err != nil || !reflect.DeepEqual(flat, again) {
		t.Fatalf("Flatten is not deterministic: %v", err)
	}

	layout, err := resolveGenerated(t, flat)
	if err != nil {
		// The only legitimate failures: totals past the backend boundary cannot happen within the limits,
		// and a job with no source page cannot inherit a size.
		if codes := codesOf(err); codes[0] != assembly.CodeSizeUnresolved {
			t.Fatalf("unexpected failure: %v", err)
		}

		return
	}

	checkInvariants(t, layout)
}

func checkInvariants(t *testing.T, layout *assembly.Layout) {
	t.Helper()

	sum, generated := checkContiguity(t, layout)

	if sum != layout.Totals.Total || generated != layout.Totals.Generated || generated != layout.Flat.GeneratedPages {
		t.Fatalf("totals %+v vs sum %d generated %d", layout.Totals, sum, generated)
	}

	var runPages int64
	for _, run := range layout.Runs {
		runPages += run.Count
	}

	if runPages != sum {
		t.Fatalf("runs cover %d pages, want %d", runPages, sum)
	}

	var uses int64
	for index := range layout.Specs {
		uses += layout.Specs[index].Uses
	}

	if uses != int64(len(layout.Flat.Contributions)-pdfCount(layout.Flat)) {
		t.Fatalf("spec uses %d", uses)
	}
}

// checkContiguity verifies each placement starts where the previous one ended and returns the
// number of output pages and the number of generated pages.
func checkContiguity(t *testing.T, layout *assembly.Layout) (int64, int64) {
	t.Helper()

	var end, sum, generated int64

	for index := range layout.Flat.Contributions {
		contribution, placement := &layout.Flat.Contributions[index], layout.Placements[index]
		if contribution.Index != index || placement.Range.Start != end+1 || placement.Range.Count != placement.Range.End-end {
			t.Fatalf("contribution %d breaks contiguity: %+v after %d", index, placement.Range, end)
		}

		end = placement.Range.End
		sum += placement.Range.Count

		if contribution.Kind == assembly.ItemBlank {
			generated += contribution.Count
		}
	}

	if end != sum {
		t.Fatalf("placements end at %d but cover %d pages", end, sum)
	}

	return sum, generated
}

func pdfCount(flat *assembly.Flattened) int {
	count := 0

	for index := range flat.Contributions {
		if flat.Contributions[index].Kind == assembly.ItemPDF {
			count++
		}
	}

	return count
}

func FuzzPathResolve(f *testing.F) {
	seeds := []string{
		"", aPath, "../x", driveRelativePath, `C:\x`, `\\s\sh\x`, "/abs", blankOperand, "Ābols ļ", "a\x00b",
		"x/./y//z", `\x`, "C:", "é:é", "\xb1", "\ufffd", `C:\Ābols\x`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		for _, style := range []assembly.PathStyle{assembly.PathStylePOSIX, assembly.PathStyleWindows} {
			checkPathStyle(t, style, name)
		}
	})
}

// checkPathStyle checks classification and resolution of one name under one path style.
func checkPathStyle(t *testing.T, style assembly.PathStyle, name string) {
	t.Helper()

	class := assembly.ClassifyPath(style, name)
	if class < assembly.PathRelative || class > assembly.PathRootedWithoutDrive {
		t.Fatalf("class %d", class)
	}

	ambiguous := class == assembly.PathDriveRelative || class == assembly.PathRootedWithoutDrive
	if style == assembly.PathStylePOSIX && ambiguous {
		t.Fatalf("POSIX has no drives: %q", name)
	}

	code, got, err := assembly.ResolvePathWithStyle(style, hostPath(baseDirectory), "path", name)
	checkResolutionIsDeterministic(t, style, name, code, got, err)

	invalidText := !utf8.ValidString(name) || containsNUL(name)
	if (err != nil) != (ambiguous || invalidText) {
		t.Fatalf("%q under style %d: class %d err %v", name, style, class, err)
	}

	if invalidText {
		checkUnrepresentablePathResult(t, code, got, err)
	}

	if err == nil && !filepath.IsAbs(got) {
		t.Fatalf("%q resolved to the relative %q", name, got)
	}
}

func checkUnrepresentablePathResult(t *testing.T, code assembly.Code, got string, err error) {
	t.Helper()

	if code != assembly.CodeInvalidJob || got != "" || !errors.Is(err, assembly.ErrInvalidPath) {
		t.Fatalf("unrepresentable path text lost its rejection contract: %s %q %v", code, got, err)
	}
}

func checkResolutionIsDeterministic(t *testing.T, style assembly.PathStyle, name string, code assembly.Code, got string, err error) {
	t.Helper()

	code2, got2, err2 := assembly.ResolvePathWithStyle(style, hostPath(baseDirectory), "path", name)
	if code != code2 || got != got2 || (err == nil) != (err2 == nil) {
		t.Fatal("not deterministic")
	}
}

func containsNUL(text string) bool {
	for index := range len(text) {
		if text[index] == 0 {
			return true
		}
	}

	return false
}
