// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"crypto/sha256"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// pageContent records page order and encoded content-stream identity without
// copying stream bytes or comparing mutable form/resource representations.
type pageContent struct {
	ref     types.IndirectRef
	streams []contentStream
}

type pageContentSnapshot []pageContent

type contentStream struct {
	ref    types.IndirectRef
	digest [sha256.Size]byte
}

func contentFailure(page pageRef, number int, message diag.Explanation) error {
	d := diag.Report(diag.SourceDamaged, message)
	d.PDFLocation = &diag.PDFLocation{Object: page.ref.ObjectNumber.Value(), Page: number, Path: "Pages.Contents"}
	return &policyError{finding: d}
}

// capturePageContent also refuses dangling references, including wrong-generation
// references, before a validator can interpret missing content as a blank page.
func capturePageContent(c context.Context, s *model.Context) (pageContentSnapshot, error) {
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	return observePageContent(c, s, pages)
}

func observePageContent(c context.Context, s *model.Context, pages []pageRef) (pageContentSnapshot, error) {
	result := make(pageContentSnapshot, len(pages))
	digests := map[types.IndirectRef][sha256.Size]byte{}
	for i, page := range pages {
		if err := c.Err(); err != nil {
			return nil, err
		}
		result[i].ref = page.ref
		original := page.dict["Contents"]
		if original == nil {
			continue // An absent or explicit null Contents represents a blank page.
		}
		if ref, indirect := original.(types.IndirectRef); indirect && !liveReference(s, ref) {
			return nil, contentFailure(page, i+1, diag.PageContentsReferencesAMissingOrFreeObjectOrThe())
		}
		value, err := s.Dereference(original)
		if err != nil {
			return nil, err
		}
		var entries types.Array
		switch value := value.(type) {
		case nil:
			continue // A live indirect null is also an absent optional Contents.
		case types.StreamDict:
			entries = types.Array{original}
		case types.Array:
			entries = value // An empty array contains no streams to preserve.
		default:
			return nil, contentFailure(page, i+1, diag.PageContentsMustResolveToAStreamOrAnArray())
		}
		for _, entry := range entries {
			if err := c.Err(); err != nil {
				return nil, err
			}
			ref, ok := entry.(types.IndirectRef)
			if !ok {
				return nil, contentFailure(page, i+1, diag.PageContentStreamsMustBeIndirectReferencesToExistingStreams())
			}
			if !liveReference(s, ref) {
				return nil, contentFailure(page, i+1, diag.APageContentStreamReferencesAMissingOrFreeObject())
			}
			digest, known := digests[ref]
			if !known {
				object, err := s.Dereference(ref)
				if err != nil {
					return nil, err
				}
				stream, ok := object.(types.StreamDict)
				if !ok {
					return nil, contentFailure(page, i+1, diag.APageContentStreamIsMissingHasTheWrongGeneration())
				}
				digest = sha256.Sum256(stream.Raw)
				digests[ref] = digest
			}
			result[i].streams = append(result[i].streams, contentStream{ref: ref, digest: digest})
		}
	}
	return result, nil
}

func (before pageContentSnapshot) verify(c context.Context, s *model.Context) error {
	after, err := capturePageContent(c, s)
	if err != nil {
		return err
	}
	return before.compare(after)
}

func (before pageContentSnapshot) compare(after pageContentSnapshot) error {
	if !slices.EqualFunc(before, after, func(a, b pageContent) bool {
		return a.ref == b.ref && slices.Equal(a.streams, b.streams)
	}) {
		return refuse(diag.SourceDamaged, diag.PDFValidationChangedThePageSequenceOrOriginalEncodedContent())
	}
	return nil
}

// validatePreservingPages applies the caller's semantic validation mode, then
// checks the actual page/content invariant independently of validation notices.
// It does not establish equivalence of arbitrary PDF objects or rendering.
func validatePreservingPages(c context.Context, s *model.Context) (model.ValidationReport, error) {
	_, report, err := validateObservedPages(c, s, nil)
	return report, err
}

// validateObservedPages returns the post-validation observation for consumers
// of the same stable graph. A fresh traversal is mandatory after validation.
func validateObservedPages(c context.Context, s *model.Context, pages []pageRef) (pageContentSnapshot, model.ValidationReport, error) {
	var before pageContentSnapshot
	var err error
	if pages == nil {
		before, err = capturePageContent(c, s)
	} else {
		before, err = observePageContent(c, s, pages)
	}
	if err != nil {
		return nil, s.ValidationReport(), err
	}
	report, err := api.ValidateContextWithReport(c, s)
	if err != nil {
		return nil, report, err
	}
	after, err := capturePageContent(c, s)
	if err != nil {
		return nil, report, err
	}
	return after, report, before.compare(after)
}
