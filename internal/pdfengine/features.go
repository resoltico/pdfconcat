// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	// FeatureKind identifies a material source feature whose handling matters to the assembled packet.
	FeatureKind string
	// FeatureDisposition is the assembly policy for an observed feature.
	FeatureDisposition string
	// SourceFeature is an observed source fact and its scope-specific assembly policy.
	SourceFeature struct {
		Kind        FeatureKind
		Disposition FeatureDisposition
	}
	featureObserver struct {
		pdf   *model.Context
		found map[FeatureKind]bool
	}
)

// Feature kinds distinguish catalog removal from page-local retention.
const (
	// maxFeatureDepth bounds recursive name/action/field observation independently of page-tree geometry.
	maxFeatureDepth = 64

	FeatureBookmarks          FeatureKind        = "bookmarks"
	FeatureTaggedStructure    FeatureKind        = "tagged_structure"
	FeatureCatalogAttachments FeatureKind        = "catalog_attachments"
	FeatureCatalogActions     FeatureKind        = "catalog_actions"
	FeaturePageActions        FeatureKind        = "page_actions"
	FeaturePageAttachments    FeatureKind        = "page_attachments"
	FeaturePageLabels         FeatureKind        = "page_labels"
	FeatureOtherCatalogNames  FeatureKind        = "other_catalog_names"
	FeatureRemoved            FeatureDisposition = "removed"
	FeatureRetained           FeatureDisposition = "retained"
)

// observeFeatures runs on the original object graph before validation can repair/delete catalog state.
func observeFeatures(ctx context.Context, pdf *model.Context) ([]SourceFeature, error) {
	observer := featureObserver{pdf: pdf, found: map[FeatureKind]bool{}}
	if err := observer.catalog(ctx); err != nil {
		return nil, fmt.Errorf("catalog features: %w", err)
	}

	root, err := pdf.Pages()
	if err != nil {
		return nil, fmt.Errorf("feature page tree: %w", err)
	}

	err = walkPages(
		ctx,
		pdf,
		*root,
		func(_ types.IndirectRef, page types.Dict, _ inheritedAttrs) error { return observer.page(ctx, page) },
	)
	if err != nil {
		return nil, fmt.Errorf("page features: %w", err)
	}

	form, err := pdf.DereferenceDict(pdf.RootDict[keyAcroForm])
	if err != nil {
		return nil, fmt.Errorf("feature form: %w", err)
	}

	fields, err := pdf.DereferenceArray(form["Fields"])
	if err != nil {
		return nil, fmt.Errorf("feature fields: %w", err)
	}

	if fieldErr := observer.fieldActions(ctx, fields, map[types.IndirectRef]bool{}, 0); fieldErr != nil {
		return nil, fieldErr
	}

	return observer.result(), nil
}

func (o *featureObserver) result() []SourceFeature {
	features := make([]SourceFeature, 0, len(o.found))
	for _, kind := range []FeatureKind{
		FeatureBookmarks, FeatureTaggedStructure, FeatureCatalogAttachments, FeatureCatalogActions,
		FeaturePageActions, FeaturePageAttachments,
		FeaturePageLabels, FeatureOtherCatalogNames,
	} {
		if !o.found[kind] {
			continue
		}

		disposition := FeatureRemoved
		if kind == FeaturePageActions || kind == FeaturePageAttachments {
			disposition = FeatureRetained
		}

		features = append(features, SourceFeature{Kind: kind, Disposition: disposition})
	}

	if len(features) == 0 {
		return nil
	}

	return features
}

func (o *featureObserver) catalog(ctx context.Context) error {
	for _, entry := range []struct {
		key, child string
		kind       FeatureKind
	}{{"Outlines", "First", FeatureBookmarks}, {"StructTreeRoot", "K", FeatureTaggedStructure}, {"PageLabels", "Nums", FeaturePageLabels}} {
		dictionary, err := o.pdf.DereferenceDict(o.pdf.RootDict[entry.key])
		if err != nil {
			return fmt.Errorf("/%s: %w", entry.key, err)
		}

		material, err := o.materialTree(ctx, dictionary, entry.child, entry.kind, map[types.IndirectRef]bool{}, 0)
		if err != nil {
			return err
		}

		o.found[entry.kind] = material
	}

	openAction, err := o.pdf.Dereference(o.pdf.RootDict["OpenAction"])
	if err != nil {
		return fmt.Errorf("catalog open action: %w", err)
	}

	if destination, ok := openAction.(types.Array); ok && len(destination) > 0 {
		o.found[FeatureCatalogActions] = true
	}

	if actionErr := o.actions(ctx, o.pdf.RootDict, FeatureCatalogActions); actionErr != nil {
		return actionErr
	}

	names, err := o.pdf.DereferenceDict(o.pdf.RootDict[keyNames])
	if err != nil {
		return fmt.Errorf("catalog names: %w", err)
	}

	if namesErr := o.nameTrees(ctx, names); namesErr != nil {
		return namesErr
	}

	return o.associatedFiles(ctx, o.pdf.RootDict["AF"])
}

func (o *featureObserver) nameTrees(ctx context.Context, names types.Dict) error {
	for name, object := range names {
		if name == keyDests {
			continue
		}

		dictionary, err := o.pdf.DereferenceDict(object)
		if err != nil {
			return fmt.Errorf("name tree /%s: %w", name, err)
		}

		kind := FeatureOtherCatalogNames

		switch name {
		case "EmbeddedFiles":
			kind = FeatureCatalogAttachments
		case "JavaScript":
			kind = FeatureCatalogActions
		default: // Other non-destination name trees also have document scope.
		}

		material, err := o.materialTree(ctx, dictionary, "Names", kind, map[types.IndirectRef]bool{}, 0)
		if err != nil {
			return err
		}

		if !material {
			continue
		}

		o.found[kind] = true
	}

	return nil
}
