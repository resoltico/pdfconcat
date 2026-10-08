// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
)

type (
	// Builder accumulates one Report. It is safe for concurrent use, so bounded workers can add the
	// diagnostics of their own inputs; Build orders them by input position, never by completion time.
	Builder struct {
		styleIndex  map[styleKey][]int
		sourceIndex map[sourceKey]int
		fontIndex   map[Font]int
		ordered     []orderedDiagnostic
		styles      []Style
		sources     []sourceKey
		report      Report
		mutex       sync.Mutex
	}

	orderedDiagnostic struct {
		diagnostic Diagnostic
		order      int
	}

	// sourceKey is a Source with its optional size held by value, so identical sources intern to one entry.
	sourceKey struct {
		path   string
		digest string
		bytes  int64
		known  bool
	}

	// styleKey is a Style with its optional text held by value, so identical styles intern to one entry.
	styleKey struct {
		background        string
		size              PageSize
		text              TextSettings
		bounds, inkBounds Rect
		hasBounds, hasInk bool
		hasText           bool
		font              int
	}
)

// NewBuilder starts a report for command. All phases start as not run.
func NewBuilder(command string) *Builder {
	return &Builder{
		styleIndex:  map[styleKey][]int{},
		sourceIndex: map[sourceKey]int{},
		fontIndex:   map[Font]int{},
		report: Report{
			FormatVersion: Version,
			AttemptID:     rand.Text(),
			Kind:          KindReport,
			Command:       command,
			Publication:   Publication{ReportStatus: ReportNotRequested},
			Phases: Phases{
				Instructions: PhaseNotRun, InputInspection: PhaseNotRun, Layout: PhaseNotRun, OutputVerification: PhaseNotRun,
			},
		},
	}
}

func keyOfSource(s Source) sourceKey {
	key := sourceKey{path: s.Path, digest: s.Digest}
	if s.Bytes != nil {
		key.known, key.bytes = true, *s.Bytes
	}

	return key
}

func (k sourceKey) source() Source {
	source := Source{Path: k.path, Digest: k.digest}
	if k.known {
		size := k.bytes
		source.Bytes = &size
	}

	return source
}

func keyOfStyle(s *Style) styleKey {
	key := styleKey{size: s.Size, background: s.Background}
	if s.Text != nil {
		key.hasText, key.text, key.font = true, s.Text.TextSettings, s.Text.Font

		key.text.Bounds, key.text.InkBounds = nil, nil
		if s.Text.Bounds != nil {
			key.hasBounds, key.bounds = true, *s.Text.Bounds
		}

		if s.Text.InkBounds != nil {
			key.hasInk, key.inkBounds = true, *s.Text.InkBounds
		}
	}

	return key
}

// cloneStyle owns every optional geometry value and finding of its snapshot.
func cloneStyle(style Style) Style {
	if style.Text == nil {
		return style
	}

	text := *style.Text
	if text.Bounds != nil {
		bounds := *text.Bounds
		text.Bounds = &bounds
	}

	if text.InkBounds != nil {
		ink := *text.InkBounds
		text.InkBounds = &ink
	}

	text.Findings = slices.Clone(text.Findings)
	style.Text = &text

	return style
}

// HexDigest formats a SHA-256 digest as the 64 lower-case hexadecimal digits reports use.
func HexDigest(sum [sha256.Size]byte) string {
	return hex.EncodeToString(sum[:])
}

// AddDiagnostic records a diagnostic. order is the stage's input-order key (for example a contribution
// or distinct-source ordinal); Build sorts stably by order, retaining append order for equal keys.
// Locations carry the original declaration independently of this private sorting key.
func (b *Builder) AddDiagnostic(order int, d Diagnostic) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if d.Severity == "" {
		d.Severity = SeverityError
	}

	b.ordered = append(b.ordered, orderedDiagnostic{diagnostic: cloneDiagnostic(d), order: order})
}

// SetPhases records how far each phase got.
func (b *Builder) SetPhases(p Phases) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.report.Phases = p
}

// SetCounts records the page totals; nil is unknown.
func (b *Builder) SetCounts(c Counts) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.report.Counts = c
}

// SetPublication records the publication state.
func (b *Builder) SetPublication(p Publication) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.report.Publication = p
}

// AddPart appends a part, in output order, and returns its index.
func (b *Builder) AddPart(p Part) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.report.Parts = append(b.report.Parts, p)

	return len(b.report.Parts) - 1
}

// Source interns a source and returns its index in the sources table.
func (b *Builder) Source(s Source) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return intern(b.sourceIndex, &b.sources, keyOfSource(s))
}

// Font interns a font and returns its index in the fonts table.
func (b *Builder) Font(f Font) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return intern(b.fontIndex, &b.report.Fonts, f)
}

// Style interns a resolved style and returns its index in the styles table, so a million identical blanks
// share one entry. Source and Font are interned the same way.
func (b *Builder) Style(s *Style) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	key := keyOfStyle(s)
	for _, index := range b.styleIndex[key] {
		if s.Text == nil || slices.Equal(b.styles[index].Text.Findings, s.Text.Findings) {
			return index
		}
	}

	index := len(b.styles)
	b.styleIndex[key] = append(b.styleIndex[key], index)
	b.styles = append(b.styles, cloneStyle(*s))

	return index
}

// intern returns the index of value in table, appending it when new.
func intern[T comparable](index map[T]int, table *[]T, value T) int {
	if found, ok := index[value]; ok {
		return found
	}

	*table = append(*table, value)
	index[value] = len(*table) - 1

	return len(*table) - 1
}

// tableOf rebuilds a report table from its interned keys; an empty table stays nil.
func tableOf[K, V any](keys []K, build func(K) V) []V {
	if len(keys) == 0 {
		return nil
	}

	table := make([]V, len(keys))
	for i, key := range keys {
		table[i] = build(key)
	}

	return table
}

// Build returns the report with the given status and the diagnostics sorted stably by input order. The builder can
// keep being used afterwards; the result does not share its diagnostics slice.
func (b *Builder) Build(status Status) *Report {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	sorted := slices.Clone(b.ordered)
	slices.SortStableFunc(sorted, func(x, y orderedDiagnostic) int {
		return cmp.Compare(x.order, y.order)
	})

	out := b.report
	out.Status = status
	out.Sources = tableOf(b.sources, sourceKey.source)
	out.Styles = tableOf(b.styles, cloneStyle)
	out.Diagnostics = make([]Diagnostic, len(sorted))

	for i := range sorted {
		out.Diagnostics[i] = cloneDiagnostic(sorted[i].diagnostic)
	}

	out.Diagnostics = append(out.Diagnostics, layoutWarnings(&out)...)
	out.FinalizeDiagnostics()

	return &out
}

func cloneDiagnostic(diagnostic Diagnostic) Diagnostic {
	diagnostic.Consumers = slices.Clone(diagnostic.Consumers)
	diagnostic.Location = cloneLocation(diagnostic.Location)

	diagnostic.Recovery = clonePointer(diagnostic.Recovery)
	if diagnostic.Recovery != nil {
		diagnostic.Recovery.Location = cloneLocation(diagnostic.Recovery.Location)
	}

	return diagnostic
}

// clonePointer copies an optional scalar or value before another owner can mutate it.
func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}

	copyValue := *value

	return &copyValue
}

func cloneLocation(location *Location) *Location {
	copyLocation := clonePointer(location)
	if copyLocation != nil {
		copyLocation.ArgvIndex = clonePointer(location.ArgvIndex)
		copyLocation.Offset = clonePointer(location.Offset)
	}

	return copyLocation
}
