// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"errors"
	"fmt"
)

type (
	// SourceFile is one captured source document and the facts Inspect reported for it.
	SourceFile struct {
		// Path is the private snapshot to read; the bytes inspected are the bytes assembled.
		Path string
		// Info is the result of Inspect(Path).
		Info SourceInfo
	}

	// ResourceDocument is the generated-pages PDF: one page per distinct generated-page specification.
	// Assemble only reads it, and uses its pages by index.
	ResourceDocument struct {
		// Path is the generated PDF.
		Path string
		// Pages is the number of pages it holds.
		Pages int
	}

	// Run is one compact element of the final page order.
	Run struct {
		// Generated selects the kind: false for pages of Sources[Index], true for page Index of the resource.
		Generated bool
		// Index is the source index, or the 0-based resource page index.
		Index int
		// Start is the 1-based first source page; it is unused for generated runs.
		Start int
		// Count is the number of consecutive source pages, or the number of repetitions of the generated page.
		Count int
	}

	// AssembleRequest describes one output document.
	AssembleRequest struct {
		Resource      *ResourceDocument
		Destination   string
		OutputDigest  string
		writtenDigest string
		Sources       []SourceFile
		Order         []Run
		ExpectedPages int
	}
)

// MaxOutputPages is the largest page total Assemble accepts. The pool holds one dictionary per output
// page in memory, so the bound turns a request that could only exhaust memory into an immediate error.
// The engine's measured envelope is far smaller (see the package documentation).
const MaxOutputPages = 2_000_000

var (
	errNoDestination      = errors.New("no destination")
	errEmptyOrder         = errors.New("the page order is empty")
	errCountNotPositive   = errors.New("count is not positive")
	errTotalOverLimit     = errors.New("the page total exceeds the limit")
	errTotalMismatch      = errors.New("the order produces a different page count than expected")
	errNoResource         = errors.New("generated page but no resource document")
	errResourceOutOfRange = errors.New("resource page is out of range")
	errSourceOutOfRange   = errors.New("source is out of range")
	errPagesOutOfRange    = errors.New("pages are outside the source")
	errMustBeWhole        = errors.New("a source with annotations, forms or named destinations must be used whole")
	errLegacyDestsClash   = errors.New("sources with a legacy catalog /Dests dictionary occur more than once: " +
		"their destination names would overwrite each other")
)

// SourcePages is a run of count consecutive pages of Sources[source] beginning at the 1-based page start.
func SourcePages(source, start, count int) Run {
	return Run{Index: source, Start: start, Count: count}
}

// GeneratedPages is a run of repeat copies of the 0-based resource page spec.
func GeneratedPages(spec, repeat int) Run {
	return Run{Generated: true, Index: spec, Count: repeat}
}

// check validates the request and enforces the source policy before any file is read.
//
// The page total is accumulated without overflow, is bounded by MaxOutputPages, and must equal ExpectedPages.
func (r *AssembleRequest) check() error {
	if r.Destination == "" {
		return newError(CodeRequestInvalid, NoSource, "", errNoDestination)
	}

	if len(r.Order) == 0 {
		return newError(CodeRequestInvalid, NoSource, "", errEmptyOrder)
	}

	total, legacyRuns := 0, 0

	for position, run := range r.Order {
		if run.Count < 1 {
			return requestError(position, run, fmt.Errorf(causeNumberFormat, errCountNotPositive, run.Count))
		}

		if err := r.checkRun(position, run, &legacyRuns); err != nil {
			return err
		}

		if run.Count > MaxOutputPages-total {
			return requestError(position, run, fmt.Errorf("%w of %d pages", errTotalOverLimit, MaxOutputPages))
		}

		total += run.Count
	}

	if total != r.ExpectedPages {
		return newError(CodeRequestInvalid, NoSource, "",
			fmt.Errorf("%w: %d pages, expected %d", errTotalMismatch, total, r.ExpectedPages))
	}

	return nil
}

// checkRun validates one run and applies the per-source policy. legacyRuns counts the runs of sources
// that carry a legacy /Dests dictionary.
func (r *AssembleRequest) checkRun(position int, run Run, legacyRuns *int) error {
	if run.Generated {
		return r.checkGeneratedRun(position, run)
	}

	if run.Index < 0 || run.Index >= len(r.Sources) {
		return requestError(position, run, fmt.Errorf("%w: %d not in 0..%d", errSourceOutOfRange, run.Index, len(r.Sources)-1))
	}

	source := &r.Sources[run.Index]
	pages := source.Info.Pages

	if run.Start < 1 || run.Start > pages || run.Count > pages-run.Start+1 {
		return requestError(position, run, fmt.Errorf("%w: pages %d+%d of %d", errPagesOutOfRange, run.Start, run.Count, pages))
	}

	if source.Info.ImportPerOccurrence() && (run.Start != 1 || run.Count != pages) {
		return newError(CodePartialRange, run.Index, source.Path,
			fmt.Errorf("%w: pages %d-%d only", errMustBeWhole, run.Start, run.Start+run.Count-1))
	}

	if source.Info.LegacyDests {
		*legacyRuns++
		if *legacyRuns > 1 {
			return newError(CodeLegacyDestsRepeated, run.Index, source.Path, errLegacyDestsClash)
		}
	}

	return nil
}

// checkGeneratedRun validates a run of a generated page against the resource document.
func (r *AssembleRequest) checkGeneratedRun(position int, run Run) error {
	if r.Resource == nil {
		return requestError(position, run, errNoResource)
	}

	if run.Index < 0 || run.Index >= r.Resource.Pages {
		return requestError(position, run, fmt.Errorf("%w: %d not in 0..%d", errResourceOutOfRange, run.Index, r.Resource.Pages-1))
	}

	return nil
}

func requestError(position int, run Run, err error) *Error {
	source := NoSource
	if !run.Generated {
		source = run.Index
	}

	return newError(CodeRequestInvalid, source, "", fmt.Errorf("order run %d: %w", position, err))
}
