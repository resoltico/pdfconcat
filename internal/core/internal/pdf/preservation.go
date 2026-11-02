// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"crypto/sha256"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
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

func contentFailure(page pageRef, number int, message string) error {
	d := diag.New(diag.SourceDamaged, message)
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
			return nil, contentFailure(page, i+1, "Page Contents references a missing or free object, or the wrong object generation.")
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
			return nil, contentFailure(page, i+1, "Page Contents must resolve to a stream or an array of existing streams.")
		}
		for _, entry := range entries {
			if err := c.Err(); err != nil {
				return nil, err
			}
			ref, ok := entry.(types.IndirectRef)
			if !ok {
				return nil, contentFailure(page, i+1, "Page content streams must be indirect references to existing streams.")
			}
			if !liveReference(s, ref) {
				return nil, contentFailure(page, i+1, "A page content stream references a missing or free object, or the wrong object generation.")
			}
			digest, known := digests[ref]
			if !known {
				object, err := s.Dereference(ref)
				if err != nil {
					return nil, err
				}
				stream, ok := object.(types.StreamDict)
				if !ok {
					return nil, contentFailure(page, i+1, "A page content stream is missing, has the wrong generation, or is not a stream.")
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
	if !slices.EqualFunc(before, after, func(a, b pageContent) bool {
		return a.ref == b.ref && slices.Equal(a.streams, b.streams)
	}) {
		return refuse(diag.SourceDamaged, "PDF validation changed the page sequence or original encoded content streams.")
	}
	return nil
}

// validatePreservingPages applies the caller's semantic validation mode, then
// checks the actual page/content invariant independently of validation notices.
// It does not establish equivalence of arbitrary PDF objects or rendering.
func validatePreservingPages(c context.Context, s *model.Context) (model.ValidationReport, error) {
	before, err := capturePageContent(c, s)
	if err != nil {
		return s.ValidationReport(), err
	}
	report, err := api.ValidateContextWithReport(c, s)
	if err != nil {
		return report, err
	}
	return report, before.verify(c, s)
}
