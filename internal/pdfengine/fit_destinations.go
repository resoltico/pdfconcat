// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const fitDestinationNameError = "fit destination name: %w"

func (i *fitInspector) catalog(ctx context.Context) error {
	if err := i.editableForm(ctx); err != nil {
		return err
	}

	legacy, err := i.pdf.DereferenceDictContext(ctx, i.pdf.RootDict[keyDests])
	if err != nil {
		return fmt.Errorf("%w: catalog /Dests: %w", errFitUnsupported, err)
	}

	for name, object := range legacy {
		i.destinations[fitDestinationKey{legacy: true, name: name}] = object
	}

	names, err := i.pdf.DereferenceDictContext(ctx, i.pdf.RootDict[keyNames])
	if err != nil {
		return fmt.Errorf("%w: catalog /Names: %w", errFitUnsupported, err)
	}

	if treeErr := i.destinationTree(ctx, names[keyDests], newFitGraphWalk(), 0); treeErr != nil {
		return treeErr
	}

	keys := slices.Collect(maps.Keys(i.destinations))
	slices.SortFunc(keys, func(a, b fitDestinationKey) int {
		if a.legacy != b.legacy {
			if a.legacy {
				return -1
			}

			return 1
		}

		return cmp.Compare(a.name, b.name)
	})

	for _, key := range keys {
		if err = i.destination(ctx, i.destinations[key], newFitGraphWalk(), 0); err != nil {
			return fmt.Errorf("%w: named destination %q (legacy=%t): %w", errFitUnsupported, key.name, key.legacy, err)
		}
	}

	return nil
}

func (i *fitInspector) editableForm(ctx context.Context) error {
	form, err := i.pdf.DereferenceDictContext(ctx, i.pdf.RootDict[keyAcroForm])
	if err != nil {
		return fmt.Errorf("%w: /AcroForm: %w", errFitUnsupported, err)
	}

	fields, err := i.pdf.DereferenceArrayContext(ctx, form[keyFields])
	if err != nil {
		return fmt.Errorf("%w: /AcroForm /Fields: %w", errFitUnsupported, err)
	}

	if len(fields) > 0 {
		return fmt.Errorf("%w: /AcroForm /Fields object %s is editable; affected source pages 1..%d",
			errFitUnsupported, fitObjectIdentity(fields[0]), len(i.pages))
	}

	return nil
}

func (i *fitInspector) destinationTree(ctx context.Context, object types.Object, walk *fitGraphWalk, depth int) error {
	if err := walk.enter(ctx, object, depth); err != nil {
		return err
	}

	defer walk.leave(object)

	tree, err := i.pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return fmt.Errorf("%w: /Names /Dests tree: %w", errFitUnsupported, err)
	}

	if entriesErr := i.destinationEntries(ctx, tree, walk); entriesErr != nil {
		return entriesErr
	}

	kids, err := i.pdf.DereferenceArrayContext(ctx, tree["Kids"])
	if err != nil {
		return fmt.Errorf("%w: destination tree /Kids: %w", errFitUnsupported, err)
	}

	for index, child := range kids {
		if child == nil {
			return fmt.Errorf("%w: destination tree /Kids[%d] must be a dictionary", errFitUnsupported, index)
		}

		if childErr := i.destinationTree(ctx, child, walk, depth+1); childErr != nil {
			return childErr
		}
	}

	return nil
}

func (i *fitInspector) destinationEntries(ctx context.Context, tree types.Dict, walk *fitGraphWalk) error {
	entries, err := i.pdf.DereferenceArrayContext(ctx, tree[keyNames])
	if err != nil {
		return fmt.Errorf("destination tree /Names: %w", err)
	}

	if len(entries)%2 != 0 {
		return fmt.Errorf("%w: /Names /Dests /Names must contain name/value pairs", errFitUnsupported)
	}

	for index := 0; index < len(entries); index += 2 {
		if entryErr := walk.visit(ctx); entryErr != nil {
			return entryErr
		}

		key, nameErr := i.destinationKey(ctx, entries[index])

		name := key.name

		if nameErr != nil {
			return fmt.Errorf("destination tree key: %w", nameErr)
		}

		if key.legacy || name == "" {
			return fmt.Errorf("%w: invalid destination name", errFitUnsupported)
		}

		if _, duplicate := i.destinations[key]; duplicate {
			return fmt.Errorf("%w: ambiguous duplicate destination name %q", errFitUnsupported, name)
		}

		i.destinations[key] = entries[index+1]
	}

	return nil
}

func fitGraphGuard(ctx context.Context, object types.Object, active map[types.IndirectRef]bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fit graph canceled: %w", err)
	}

	if depth > maxFeatureDepth {
		return fmt.Errorf("%w: graph exceeds %d levels", errFitUnsupported, maxFeatureDepth)
	}

	if ref, ok := object.(types.IndirectRef); ok && active[ref] {
		return fmt.Errorf("%w: cyclic graph at object %s", errFitUnsupported, ref.PDFString())
	}

	return nil
}

func (i *fitInspector) destination(ctx context.Context, object types.Object, walk *fitGraphWalk, depth int) error {
	if err := walk.enter(ctx, object, depth); err != nil {
		return err
	}
	defer walk.leave(object)

	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fmt.Errorf("fit destination reference: %w", err)
	}

	switch destination := value.(type) {
	case types.Array:
		return i.fitDestinationArray(ctx, destination)
	case types.Dict:
		if sdErr := i.nonempty(ctx, destination, "SD"); sdErr != nil {
			return sdErr
		}

		if destination["D"] == nil {
			return fmt.Errorf("%w: destination dictionary has no /D", errFitUnsupported)
		}

		return i.destination(ctx, destination["D"], walk, depth+1)
	case types.Name, types.StringLiteral, types.HexLiteral:
		return i.namedDestination(ctx, value, walk, depth)
	default:
		return fmt.Errorf("%w: destination must be a direct or named local [page /Fit]", errFitUnsupported)
	}
}

func (i *fitInspector) destinationKey(ctx context.Context, object types.Object) (fitDestinationKey, error) {
	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fitDestinationKey{}, fmt.Errorf(fitDestinationNameError, err)
	}

	switch value.(type) {
	case types.Name, types.StringLiteral, types.HexLiteral:
	default:
		return fitDestinationKey{}, fmt.Errorf("%w: destination name must be a name or byte string", errFitUnsupported)
	}

	name, err := i.pdf.DestNameContext(ctx, value)
	if err != nil {
		return fitDestinationKey{}, fmt.Errorf(fitDestinationNameError, err)
	}

	_, legacy := value.(types.Name)

	return fitDestinationKey{legacy: legacy, name: name}, nil
}

func (i *fitInspector) namedDestination(ctx context.Context, object types.Object, walk *fitGraphWalk, depth int) error {
	key, err := i.destinationKey(ctx, object)
	if err != nil {
		return fmt.Errorf(fitDestinationNameError, err)
	}

	if walk.names[key] || i.destinations[key] == nil {
		return fmt.Errorf("%w: destination %q (legacy=%t) is cyclic or unresolved", errFitUnsupported, key.name, key.legacy)
	}

	walk.names[key] = true
	defer delete(walk.names, key)

	return i.destination(ctx, i.destinations[key], walk, depth+1)
}

func (i *fitInspector) fitDestinationArray(ctx context.Context, array types.Array) error {
	if len(array) != 2 {
		return fmt.Errorf("%w: destination must be coordinate-free [page /Fit]", errFitUnsupported)
	}

	page, reference := array[0].(types.IndirectRef)

	mode, err := i.pdf.DereferenceContext(ctx, array[1])
	if err != nil {
		return fmt.Errorf("fit destination mode: %w", err)
	}

	if !reference || !i.pages[page] || mode != types.Name(fitDestinationMode) {
		return fmt.Errorf("%w: destination must refer to a source page with /Fit", errFitUnsupported)
	}

	return nil
}
