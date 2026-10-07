// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"errors"
	"fmt"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	keyAcroForm       = "AcroForm"
	keyNeedsRendering = "NeedsRendering"
)

var errRenderingState = errors.New("unsupported document rendering state")

// checkRenderingState rejects document rendering state that the pool cannot safely reconcile.
// Inspect and import share this boundary; supplied inspection facts cannot bypass it.
func checkRenderingState(pdf *model.Context) error {
	if err := checkDynamicForms(pdf); err != nil {
		return err
	}

	for _, key := range []string{"OCProperties", "OutputIntents"} {
		if _, found := pdf.RootDict.Find(key); found {
			return fmt.Errorf(
				"%w: catalog /%s; provide a source without layers or document output-color configuration",
				errRenderingState, key,
			)
		}
	}
	// OCG/OCMD objects and /OC references remain dangerous even when a missing catalog
	// declaration leaves their default visibility up to a reader. Scan direct children too.
	for _, entry := range pdf.Table {
		if entry == nil || entry.Free {
			continue
		}

		if hasOptionalContent(entry.Object) {
			return fmt.Errorf("%w: /OC, /OCG or /OCMD; provide a source without layers to preserve visibility", errRenderingState)
		}
	}

	return nil
}

func checkDynamicForms(pdf *model.Context) error {
	if object, found := pdf.RootDict.Find(keyNeedsRendering); found {
		value, err := pdf.Dereference(object)

		needsRendering, boolean := value.(types.Boolean)
		if err != nil || !boolean {
			return fmt.Errorf("%w: catalog /NeedsRendering must be a boolean", errRenderingState)
		}

		if bool(needsRendering) {
			return fmt.Errorf("%w: /NeedsRendering true; provide a static source without dynamic XFA rendering", errRenderingState)
		}
	}

	if object, found := pdf.RootDict.Find(keyAcroForm); found {
		form, err := pdf.DereferenceDict(object)
		if err != nil {
			return fmt.Errorf("%w: catalog /AcroForm: %w", errRenderingState, err)
		}

		if _, xfa := form.Find("XFA"); xfa {
			return fmt.Errorf("%w: AcroForm /XFA; provide a static source without dynamic XFA forms", errRenderingState)
		}
	}

	return nil
}

func hasOptionalContent(object types.Object) bool {
	switch object := object.(type) {
	case types.Dict:
		return optionalDictionary(object)
	case types.StreamDict:
		return hasOptionalContent(object.Dict)
	case types.Array:
		return slices.ContainsFunc(object, hasOptionalContent)
	default:
		return false
	}
}

func optionalDictionary(object types.Dict) bool {
	if _, found := object.Find("OC"); found {
		return true
	}

	if kind := object.NameEntry(keyType); kind != nil && (*kind == "OCG" || *kind == "OCMD") {
		return true
	}

	for _, child := range object {
		if hasOptionalContent(child) {
			return true
		}
	}

	return false
}
