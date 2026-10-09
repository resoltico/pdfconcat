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

var errPageKidsAmbiguity = errors.New("source page role is ambiguous across supported readers")

// checkSourcePageRoles is raw source admissibility, separate from typed structural traversal.
// A native Page remains a leaf; a nonnull extra Kids value must not turn it into a different
// rendered document in another supported reader. No entry or referenced object is deleted.
func checkSourcePageRoles(ctx context.Context, pdf *model.Context) error {
	root, err := pdf.PagesContext(ctx)
	if err != nil {
		return fmt.Errorf("source page roles: %w", err)
	}

	number := 0

	return walkPages(ctx, pdf, *root, func(ref types.IndirectRef, page types.Dict, _ inheritedAttrs) error {
		number++
		if roleErr := checkLeafKids(ctx, pdf, page); roleErr != nil {
			return fmt.Errorf("source page %d (object %s): %w", number, ref.PDFString(), roleErr)
		}

		return nil
	})
}

func checkLeafKids(ctx context.Context, pdf *model.Context, page types.Dict) error {
	// Names were decoded by the maintained parser. Direct lookup prevents a private
	// double-escaped name from being decoded again into the reserved canonical key.
	object := page[keyKids]
	if object == nil {
		return nil
	}

	if ref, indirect := object.(types.IndirectRef); indirect {
		entry, found := pdf.FindTableEntryForIndRef(&ref)
		if !found || entry == nil || entry.Free {
			return fmt.Errorf(
				"%w: declared /Page /Kids has unresolved object %s; repair the source page tree and recheck",
				errPageKidsAmbiguity,
				ref.PDFString(),
			)
		}
	}

	value, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fmt.Errorf("%w: resolve declared /Page /Kids: %w", errPageKidsAmbiguity, err)
	}

	if value != nil {
		return fmt.Errorf(
			"%w: declared /Page has non-null /Kids; readers can discard its content; "+
				"repair the source page tree/remove the conflicting entry and recheck",
			errPageKidsAmbiguity,
		)
	}

	return nil
}

func fittingSourceReadError(ctx context.Context, source int, path string, err error) error {
	if cancellation := canceled(ctx, source, path); cancellation != nil {
		return cancellation
	}

	if errors.Is(err, errPageKidsAmbiguity) {
		return newError(CodeFitUnsupported, source, path, err)
	}

	return err
}
