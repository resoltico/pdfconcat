// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const keySubtype = "Subtype"

func (o *featureObserver) page(ctx context.Context, page types.Dict) error {
	if err := o.actions(ctx, page, FeaturePageActions); err != nil {
		return err
	}

	annotations, err := o.pdf.DereferenceArray(page["Annots"])
	if err != nil {
		return fmt.Errorf("page annotations: %w", err)
	}

	for _, object := range annotations {
		if annotationErr := o.annotation(ctx, object); annotationErr != nil {
			return annotationErr
		}
	}

	return nil
}

func (o *featureObserver) annotation(ctx context.Context, object types.Object) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("annotation canceled: %w", err)
	}

	dict, readErr := o.pdf.DereferenceDict(object)
	if readErr != nil {
		return fmt.Errorf("annotation: %w", readErr)
	}

	if actionErr := o.actions(ctx, dict, FeaturePageActions); actionErr != nil {
		return actionErr
	}

	subtype, _, typeErr := o.pdf.DereferenceNameEntry(dict, keySubtype)
	if typeErr != nil {
		return fmt.Errorf("annotation subtype: %w", typeErr)
	}

	if subtype != nil && *subtype == "FileAttachment" {
		retained, payloadErr := o.attachmentPayload(dict["FS"])
		if payloadErr != nil {
			return payloadErr
		}

		o.found[FeaturePageAttachments] = o.found[FeaturePageAttachments] || retained
	}

	return nil
}

func (o *featureObserver) attachmentPayload(object types.Object) (bool, error) {
	file, err := o.pdf.DereferenceDict(object)
	if err != nil {
		return false, fmt.Errorf("attachment filespec: %w", err)
	}

	embedded, err := o.pdf.DereferenceDict(file["EF"])
	if err != nil {
		return false, fmt.Errorf("attachment embedded file: %w", err)
	}

	for _, reference := range embedded {
		value, readErr := o.pdf.Dereference(reference)
		if readErr != nil {
			return false, fmt.Errorf("attachment payload: %w", readErr)
		}

		if _, ok := value.(types.StreamDict); ok {
			return true, nil
		}
	}

	return false, nil
}

func (o *featureObserver) fieldActions(ctx context.Context, objects types.Array, active map[types.IndirectRef]bool, depth int) error {
	if depth > maxFeatureDepth {
		return errTreeTooDeep
	}

	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("field actions canceled: %w", err)
		}

		if err := o.fieldAction(ctx, object, active, depth); err != nil {
			return err
		}
	}

	return nil
}

func (o *featureObserver) fieldAction(ctx context.Context, object types.Object, active map[types.IndirectRef]bool, depth int) error {
	if ref, indirect := object.(types.IndirectRef); indirect {
		if active[ref] {
			return errNodeRepeated
		}

		active[ref] = true
		defer delete(active, ref)
	}

	dict, err := o.pdf.DereferenceDict(object)
	if err != nil {
		return fmt.Errorf("field actions: %w", err)
	}

	if actionErr := o.actions(ctx, dict, FeaturePageActions); actionErr != nil {
		return actionErr
	}

	kids, err := o.pdf.DereferenceArray(dict["Kids"])
	if err != nil {
		return fmt.Errorf("field action children: %w", err)
	}

	return o.fieldActions(ctx, kids, active, depth+1)
}

func (o *featureObserver) associatedFiles(ctx context.Context, object types.Object) error {
	files, err := o.pdf.DereferenceArray(object)
	if err != nil {
		return fmt.Errorf("catalog associated files: %w", err)
	}

	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("associated files canceled: %w", err)
		}

		material, payloadErr := o.attachmentPayload(file)
		if payloadErr != nil {
			return payloadErr
		}

		o.found[FeatureCatalogAttachments] = o.found[FeatureCatalogAttachments] || material
	}

	return nil
}
