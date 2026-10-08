// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
)

type (
	// PageGeometry is the visible size of one source page in physical points.
	PageGeometry struct {
		Width, Height float64
	}

	// SourceGeometry is what inspecting one source tells the layout: its page count and the visible
	// geometry of its first and last pages.
	SourceGeometry struct {
		First, Last PageGeometry
		Pages       int64
	}

	// FontDigest is the SHA-256 identity of font file content.
	FontDigest [sha256.Size]byte

	// FontDigests returns the content identity of the font file at an absolute path, or of the built-in
	// font for the empty path. It reports false when the font was not loaded.
	FontDigests func(path string) (FontDigest, bool)

	// SpecKey identifies a distinct generated page: the resolved appearance plus the font's content.
	SpecKey struct {
		Spec       BlankSpec
		FontDigest FontDigest
	}

	// ResolvedSpec is one distinct generated page appearance.
	ResolvedSpec struct {
		// Origins locates the first contributions that use the spec, in input order, at most MaxRelated+1.
		Origins  []Origin
		Spec     BlankSpec
		Declared TextStyle
		// Uses is the number of contributions that use the spec.
		Uses       int64
		FontDigest FontDigest
	}

	// PageRange is a run of output pages, 1-based and inclusive.
	PageRange struct {
		Start, End, Count int64
	}

	// SizeSource says where a generated page got its size.
	SizeSource uint8

	// Placement is the resolved position of one contribution in the output.
	Placement struct {
		// Range is the output pages the contribution covers.
		Range PageRange
		// Spec indexes Layout.Specs for a blank and is -1 for a PDF.
		Spec int
		// SizeFrom is the contribution whose page supplied an inherited size; -1 unless SizeSource is
		// not SizeExplicit.
		SizeFrom int
		// Size says how a blank got its size.
		Size SizeSource
	}

	// RunKind says what a Run covers.
	RunKind uint8

	// Run is a compact piece of the output order for the engine.
	Run struct {
		// Index is the Layout.Specs index of a generated run, or the Flattened.Files index of a source run.
		Index int
		// FirstPage is the first source page of a source run (always 1, whole files); 0 for a generated run.
		FirstPage int64
		// Count is the number of source pages, or the repeat count of a generated page.
		Count int64
		Kind  RunKind
	}

	// Totals are the page counts of an output.
	Totals struct {
		Source, Generated, Total int64
	}

	// Layout is the resolved output order: every contribution's pages, the distinct generated appearances,
	// and the compact runs the engine assembles. Every sum was checked.
	Layout struct {
		Source Source
		Flat   *Flattened
		// Placements parallels Flat.Contributions.
		Placements []Placement
		Specs      []ResolvedSpec
		Runs       []Run
		Totals     Totals
	}

	// resolution is the state of one pass of Resolve.
	resolution struct {
		flat      *Flattened
		layout    *Layout
		fonts     FontDigests
		specIndex map[SpecKey]int
		preceding *inheritFrom
		following *inheritFrom
		sources   []SourceGeometry
		problems  Errors
		// resolved caches, per layered style, the spec index of each inherited size.
		resolved []map[PageDim]int
	}

	// inheritFrom is a source page a blank can take its size from.
	inheritFrom struct {
		geometry     PageGeometry
		contribution int
		size         SizeSource
	}
)

const (
	// SizeExplicit means the blank's size was written.
	SizeExplicit SizeSource = 1
	// SizeFromPrecedingLast means the blank took the last page of the nearest preceding source.
	SizeFromPrecedingLast SizeSource = 2
	// SizeFromFollowingFirst means a leading blank took the first page of the first source.
	SizeFromFollowingFirst SizeSource = 3

	// RunSource is the pages of one source occurrence.
	RunSource RunKind = 1
	// RunGenerated is one distinct generated page repeated.
	RunGenerated RunKind = 2

	// MaxOutputPages bounds domain page-number arithmetic to signed 32 bits.
	// The selected backend applies its own, potentially smaller resource-policy limit.
	MaxOutputPages = math.MaxInt32
)

// ErrSourceGeometry reports inspection results that do not match the flattened sources.
var ErrSourceGeometry = errors.New("source geometry does not match the flattened sources")

// Resolve takes the inspected sources (parallel to f.Files) and the loaded fonts' identities, and
// resolves every generated page's size, the distinct generated appearances, the output ranges and the
// compact runs. Leading blanks inherit the first page of the first source; every other blank inherits the
// last page of the nearest preceding source; an explicit size changes nothing for later blanks. All
// problems found are returned together as a Errors, in input order.
func (f *Flattened) Resolve(sources []SourceGeometry, fonts FontDigests) (*Layout, error) {
	if len(sources) != len(f.Files) {
		return nil, fmt.Errorf("%w: %d results for %d sources", ErrSourceGeometry, len(sources), len(f.Files))
	}

	state := &resolution{
		flat:      f,
		sources:   sources,
		fonts:     fonts,
		layout:    &Layout{Source: f.Source, Flat: f, Placements: make([]Placement, len(f.Contributions))},
		specIndex: map[SpecKey]int{},
		resolved:  make([]map[PageDim]int, len(f.Styles)),
		following: firstSourcePage(f, sources),
	}

	state.emptySources()

	for index := range f.Contributions {
		if !state.place(&f.Contributions[index]) {
			break
		}
	}

	if len(state.problems) > 0 {
		return nil, state.problems
	}

	return state.layout, nil
}

// firstSourcePage is the first source page of the first PDF, which leading blanks inherit.
func firstSourcePage(f *Flattened, sources []SourceGeometry) *inheritFrom {
	for index := range f.Contributions {
		contribution := &f.Contributions[index]
		if contribution.Kind == ItemPDF {
			return &inheritFrom{
				geometry: sources[contribution.File].First, contribution: index, size: SizeFromFollowingFirst,
			}
		}
	}

	return nil
}

func (r *resolution) emptySources() {
	for index, source := range r.sources {
		if source.Pages < 1 {
			file := &r.flat.Files[index]

			r.failf(file.FirstUse, "", CodeSourceEmpty, nil,
				"source %q reports %d pages; a PDF needs at least one page", file.Path, source.Pages)
		}
	}
}

// place resolves one contribution and appends its run. It reports false when the page totals can no longer
// be trusted, so the walk stops.
func (r *resolution) place(contribution *Contribution) bool {
	var (
		placement Placement
		pages     int64
		resolved  = true
	)

	if contribution.Kind == ItemPDF {
		placement, pages = r.placeSource(contribution)
	} else {
		placement, pages, resolved = r.placeBlank(contribution)
	}

	if !resolved {
		return true // a recoverable problem in one blank; later contributions still resolve
	}

	return r.account(contribution, placement, pages)
}

func (r *resolution) placeSource(contribution *Contribution) (Placement, int64) {
	source := &r.sources[contribution.File]
	r.preceding = &inheritFrom{geometry: source.Last, contribution: contribution.Index, size: SizeFromPrecedingLast}

	return Placement{Spec: -1, SizeFrom: -1}, max(source.Pages, 0)
}

func (r *resolution) placeBlank(contribution *Contribution) (Placement, int64, bool) {
	placement := Placement{SizeFrom: -1, Size: SizeExplicit}

	var inherited PageDim

	if size := r.flat.Styles[contribution.Style].Style.Size; !size.IsSet() || size.Value.Inherit {
		from := r.inheritSource()
		if from != nil {
			dim, err := NewPageDim(Length(from.geometry.Width), Length(from.geometry.Height))
			if err != nil {
				r.failf(contribution.Origin, "/blank/size", CodeSizeOutOfRange, err,
					"the page this blank inherits its size from is %vx%v pt, outside the supported %d to %d points per side; "+
						"give an explicit size",
					from.geometry.Width, from.geometry.Height, int(MinPageSide), int(MaxPageSide))

				return placement, 0, false
			}

			inherited = dim
			placement.SizeFrom, placement.Size = from.contribution, from.size
		}
	}

	spec, found := r.spec(contribution, inherited)
	placement.Spec = spec

	return placement, contribution.Count, found
}

// inheritSource is the source page a blank takes its size from: the last page of the nearest preceding
// source, or for a leading blank the first page of the first source; nil when the job has no source.
func (r *resolution) inheritSource() *inheritFrom {
	if r.preceding != nil {
		return r.preceding
	}

	return r.following
}

// failf records a geometry problem located at origin.
func (r *resolution) failf(origin Origin, member string, code Code, cause error, format string, args ...any) {
	r.problems = append(r.problems, errorAt(r.flat.Source, origin, member, StageGeometry, code, cause, format, args...))
}

// spec resolves the style at an inherited size, once per distinct pair, and returns its index in the table.
func (r *resolution) spec(contribution *Contribution, inherited PageDim) (int, bool) {
	cache := r.resolved[contribution.Style]
	if cache == nil {
		cache = map[PageDim]int{}
		r.resolved[contribution.Style] = cache
	}

	index, known := cache[inherited]
	if !known {
		var resolvedOK bool

		index, resolvedOK = r.resolveSpec(contribution, inherited)
		if !resolvedOK {
			return 0, false
		}

		cache[inherited] = index
	}

	spec := &r.layout.Specs[index]
	spec.Uses++

	if len(spec.Origins) <= MaxRelated {
		spec.Origins = append(spec.Origins, contribution.Origin)
	}

	return index, true
}

func (r *resolution) resolveSpec(contribution *Contribution, inherited PageDim) (int, bool) {
	style := r.flat.Styles[contribution.Style].Style

	resolved, err := style.Resolve(inherited)
	if err != nil {
		code := CodeStyleInvalid
		if errors.Is(err, ErrUnresolvedSize) {
			code = CodeSizeUnresolved
		}

		r.failf(contribution.Origin, "/blank", code, err, "%v", err)

		return 0, false
	}

	digest, found := r.fonts(resolved.Text.Font.File)
	if !found {
		r.failf(contribution.Origin, "/blank/text/font", CodeFontUnavailable, nil,
			"font %q was not loaded; the layout needs its content identity", resolved.Text.Font)

		return 0, false
	}

	key := SpecKey{Spec: resolved, FontDigest: digest}

	index, known := r.specIndex[key]
	if !known {
		index = len(r.layout.Specs)
		r.specIndex[key] = index
		r.layout.Specs = append(r.layout.Specs, ResolvedSpec{Spec: resolved, Declared: style.Text, FontDigest: digest})
	}

	return index, true
}

// account adds the contribution's pages to the totals and the order, and reports false when a total passes
// its bound.
func (r *resolution) account(contribution *Contribution, placement Placement, pages int64) bool {
	totals := &r.layout.Totals

	total, err := AddCounts(totals.Total, pages, MaxOutputPages)
	if err != nil {
		r.failf(contribution.Origin, "", CodePageTotalExceeded, err,
			"the output would exceed %d pages at this %s; domain page numbers use 32 bits",
			int64(MaxOutputPages), contribution.Kind)

		return false
	}

	placement.Range = PageRange{Start: totals.Total + 1, End: total, Count: pages}
	totals.Total = total

	if contribution.Kind == ItemPDF {
		totals.Source += pages // bounded by Total, which was just checked
		r.layout.Runs = append(r.layout.Runs, Run{Kind: RunSource, Index: contribution.File, FirstPage: 1, Count: pages})
	} else {
		totals.Generated += pages // bounded by Total, which was just checked
		r.appendGenerated(placement.Spec, pages)
	}

	r.layout.Placements[contribution.Index] = placement

	return true
}

// appendGenerated adds a generated run, merging it into the previous run when that repeats the same page.
func (r *resolution) appendGenerated(spec int, count int64) {
	if last := len(r.layout.Runs) - 1; last >= 0 {
		previous := &r.layout.Runs[last]
		if previous.Kind == RunGenerated && previous.Index == spec {
			previous.Count += count // within Total, so within int32

			return
		}
	}

	r.layout.Runs = append(r.layout.Runs, Run{Kind: RunGenerated, Index: spec, Count: count})
}
