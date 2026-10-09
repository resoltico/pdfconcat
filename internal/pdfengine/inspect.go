// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	// SourceInfo is what the assembly policy and layout need to know about one PDF.
	SourceInfo struct {
		// Features captures material scope-specific source effects before validator repairs.
		Features []SourceFeature
		// Fits captures compact all-page fitting geometry when a target was requested.
		Fits []FitRange
		// Pages is the number of pages, at least 1.
		Pages int
		// Version is the effective PDF version: the catalog /Version entry when present, else the header.
		Version Version
		// PageLocal reports that some page has an /Annots entry, an /AA additional-actions
		// dictionary or a /B article-beads array: objects that belong to one page and refer to others.
		PageLocal bool
		// AcroForm reports that the catalog has an interactive form.
		AcroForm bool
		// NamedDests reports that the catalog has a /Names /Dests name tree.
		NamedDests bool
		// LegacyDests reports that the catalog has the legacy /Dests dictionary.
		LegacyDests bool
		// First and Last are the visible sizes of the first and last pages; see PageSize.
		First, Last PageSize
	}

	// pageRecord is a leaf page with the attributes it inherits.
	pageRecord struct {
		page      types.Dict
		inherited inheritedAttrs
	}
)

var (
	errNoPages           = errors.New("the PDF has no pages")
	errPageCountMismatch = errors.New("the page tree and the catalog disagree on the page count")
)

// ImportPerOccurrence reports whether Assemble imports the source anew for every run that uses it,
// because its annotations, forms or destinations are tied to its page objects and a shared copy would
// leave the second occurrence pointing at the first.
func (s SourceInfo) ImportPerOccurrence() bool {
	return s.PageLocal || s.AcroForm || s.NamedDests || s.LegacyDests
}

// Inspect reads and validates the PDF at path and reports its facts in one pass.
//
// The object graph is not retained after Inspect returns: only the returned values survive, so
// memory use is bounded by one source at a time per concurrent caller.
//
// Encrypted PDFs are rejected with CodeEncrypted even when they open with an empty password. A first
// or last page whose boxes, rotation or UserUnit are malformed is rejected with CodePageGeometry.
// Optional content, document output intents and dynamic XFA rendering are rejected with CodeUnsupportedRendering.
// A nonnil target adds shared raw all-page fitting policy and geometry capture before validation repairs.
func (e *Engine) Inspect(ctx context.Context, path string, target *PageSize) (SourceInfo, error) {
	var (
		info SourceInfo
		fits []FitRange
	)

	observe := observeFeatures
	if target != nil {
		observe = func(ctx context.Context, pdf *model.Context) ([]SourceFeature, error) {
			features, err := observeFeatures(ctx, pdf)
			if err != nil {
				return nil, err
			}

			fits, err = inspectPageFits(ctx, pdf, *target)

			return features, err
		}
	}

	err := guard(CodeInvalid, path, func() error {
		document, readErr := e.readDocument(ctx, NoSource, path, observe)
		if readErr != nil {
			if target != nil {
				return fittingSourceReadError(ctx, NoSource, path, readErr)
			}

			return readErr
		}

		var inspectErr error

		info, inspectErr = inspectContext(ctx, document.pdf, path)

		info.Features = document.features
		info.Fits = fits

		return inspectErr
	})
	if err != nil {
		return SourceInfo{}, err
	}

	return info, nil
}

// inspectContext gathers the facts of one read and validated document.
func inspectContext(ctx context.Context, pdf *model.Context, path string) (SourceInfo, error) {
	info := SourceInfo{Version: versionOf(pdf.XRefTable.Version())}

	_, info.AcroForm = pdf.RootDict.Find(keyAcroForm)
	_, info.LegacyDests = pdf.RootDict.Find(keyDests)

	if names, found := pdf.RootDict.Find(keyNames); found {
		dict, err := pdf.DereferenceDictContext(ctx, names)
		if err != nil {
			return SourceInfo{}, classifyWalk(ctx, path, fmt.Errorf("catalog /Names: %w", err))
		}

		_, info.NamedDests = dict.Find(keyDests)
	}

	root, err := pdf.PagesContext(ctx)
	if err != nil {
		return SourceInfo{}, classifyWalk(ctx, path, fmt.Errorf(pageTreeRootFailureFormat, err))
	}

	var (
		first, last pageRecord
		pageCount   int
	)

	err = walkPages(ctx, pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		last = pageRecord{page: page, inherited: inherited}
		if pageCount == 0 {
			first = last
		}

		pageCount++

		info.PageLocal = info.PageLocal || hasPageLocalObjects(page)

		return nil
	})
	if err != nil {
		return SourceInfo{}, classifyWalk(ctx, path, err)
	}

	if pageCount == 0 {
		return SourceInfo{}, newError(CodeNoPages, NoSource, path, errNoPages)
	}

	if pageCount != pdf.PageCount {
		return SourceInfo{}, newError(CodeInvalid, NoSource, path,
			fmt.Errorf("%w: the tree holds %d, the catalog counts %d", errPageCountMismatch, pageCount, pdf.PageCount))
	}

	info.Pages = pageCount

	if info.First, err = visibleSize(ctx, pdf, first.page, first.inherited); err != nil {
		return SourceInfo{}, pageGeometryFailure(ctx, path, "first page", err)
	}

	if info.Last, err = visibleSize(ctx, pdf, last.page, last.inherited); err != nil {
		return SourceInfo{}, pageGeometryFailure(ctx, path, "last page", err)
	}

	return info, nil
}

func pageGeometryFailure(ctx context.Context, path, page string, err error) error {
	if failure := canceled(ctx, NoSource, path); failure != nil {
		return failure
	}

	return newError(CodePageGeometry, NoSource, path, fmt.Errorf(labelCauseFormat, page, err))
}

// classifyWalk maps a page-tree failure to an Error: cancellation, else an invalid document.
func classifyWalk(ctx context.Context, path string, err error) *Error {
	if ctx.Err() != nil {
		return newError(CodeCanceled, NoSource, path, ctx.Err())
	}

	return newError(CodeInvalid, NoSource, path, err)
}

// hasPageLocalObjects reports whether the page has an /Annots, /AA or /B entry.
func hasPageLocalObjects(page types.Dict) bool {
	for _, key := range [...]string{"Annots", "AA", "B"} {
		if _, found := page.Find(key); found {
			return true
		}
	}

	return false
}
