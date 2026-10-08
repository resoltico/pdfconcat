// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

var errUnpairedNameEntry = errors.New("name tree has an unpaired entry")

func (o *featureObserver) materialTree(
	ctx context.Context,
	dict types.Dict,
	key string,
	kind FeatureKind,
	active map[types.IndirectRef]bool,
	depth int,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("feature tree canceled: %w", err)
	}

	if depth > maxFeatureDepth {
		return false, errTreeTooDeep
	}

	value, err := o.pdf.Dereference(dict[key])
	if err != nil {
		return false, fmt.Errorf("feature /%s: %w", key, err)
	}

	material, err := o.treeValue(ctx, value, key, kind)
	if err != nil {
		return false, err
	}

	children, err := o.pdf.DereferenceArray(dict["Kids"])
	if err != nil {
		return false, fmt.Errorf("feature tree children: %w", err)
	}

	for _, object := range children {
		found, walkErr := o.materialChild(ctx, object, key, kind, active, depth)
		if walkErr != nil {
			return false, walkErr
		}

		material = material || found
	}

	return material, nil
}

func materialFeatureValue(value types.Object) bool {
	switch object := value.(type) {
	case types.Array:
		return len(object) > 0
	case types.Dict:
		return len(object) > 0
	case types.StringLiteral:
		return object.Value() != ""
	case types.HexLiteral:
		return object.Value() != ""
	case types.StreamDict:
		return object.StreamLength != nil && *object.StreamLength > 0 || len(object.Raw) > 0 || len(object.Content) > 0
	default:
		return value != nil
	}
}

func (o *featureObserver) actions(ctx context.Context, dict types.Dict, kind FeatureKind) error {
	for _, key := range []string{"A", "OpenAction"} {
		found, err := o.action(ctx, dict[key], kind, map[types.IndirectRef]bool{}, 0)
		if err != nil {
			return err
		}

		o.found[kind] = o.found[kind] || found
	}

	additional, err := o.pdf.DereferenceDict(dict["AA"])
	if err != nil {
		return fmt.Errorf("additional actions: %w", err)
	}

	for _, object := range additional {
		found, readErr := o.action(ctx, object, kind, map[types.IndirectRef]bool{}, 0)
		if readErr != nil {
			return readErr
		}

		o.found[kind] = o.found[kind] || found
	}

	return nil
}

func (o *featureObserver) action(
	ctx context.Context,
	object types.Object,
	kind FeatureKind,
	active map[types.IndirectRef]bool,
	depth int,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("action canceled: %w", err)
	}

	if depth > maxFeatureDepth {
		return false, errTreeTooDeep
	}

	if ref, indirect := object.(types.IndirectRef); indirect {
		if active[ref] {
			return false, errNodeRepeated
		}

		active[ref] = true
		defer delete(active, ref)
	}

	value, err := o.pdf.Dereference(object)
	if err != nil {
		return false, fmt.Errorf("action reference: %w", err)
	}

	if array, ok := value.(types.Array); ok {
		return o.actionArray(ctx, array, kind, active, depth)
	}

	dictionary, ok := value.(types.Dict)
	if !ok {
		return false, nil
	}

	material, err := o.materialAction(dictionary, kind)
	if err != nil {
		return false, err
	}

	next, err := o.action(ctx, dictionary["Next"], kind, active, depth+1)

	return material || next, err
}

func (o *featureObserver) actionArray(
	ctx context.Context,
	objects types.Array,
	kind FeatureKind,
	active map[types.IndirectRef]bool,
	depth int,
) (bool, error) {
	material := false

	for _, object := range objects {
		found, err := o.action(ctx, object, kind, active, depth+1)
		if err != nil {
			return false, err
		}

		material = material || found
	}

	return material, nil
}

func (o *featureObserver) treeValue(ctx context.Context, value types.Object, key string, kind FeatureKind) (bool, error) {
	if key != "Names" {
		return materialFeatureValue(value), nil
	}

	entries, ok := value.(types.Array)
	if !ok || len(entries) == 0 {
		return false, nil
	}

	if len(entries)%2 != 0 {
		return false, errUnpairedNameEntry
	}

	material := false

	for index := 1; index < len(entries); index += 2 {
		found, err := o.nameValue(ctx, entries[index], kind)
		if err != nil {
			return false, err
		}

		material = material || found
	}

	return material, nil
}

func (o *featureObserver) nameValue(ctx context.Context, object types.Object, kind FeatureKind) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("name entry canceled: %w", err)
	}

	if kind == FeatureCatalogAttachments {
		return o.attachmentPayload(object)
	}

	if kind == FeatureCatalogActions {
		return o.action(ctx, object, FeatureCatalogActions, map[types.IndirectRef]bool{}, 0)
	}

	value, err := o.pdf.Dereference(object)
	if err != nil {
		return false, fmt.Errorf("name entry: %w", err)
	}

	return materialFeatureValue(value), nil
}

func (o *featureObserver) materialChild(
	ctx context.Context,
	object types.Object,
	key string,
	kind FeatureKind,
	active map[types.IndirectRef]bool,
	depth int,
) (bool, error) {
	if ref, indirect := object.(types.IndirectRef); indirect {
		if active[ref] {
			return false, errNodeRepeated
		}

		active[ref] = true
		defer delete(active, ref)
	}

	child, err := o.pdf.DereferenceDict(object)
	if err != nil {
		return false, fmt.Errorf("feature tree child: %w", err)
	}

	return o.materialTree(ctx, child, key, kind, active, depth+1)
}

func (o *featureObserver) materialAction(dictionary types.Dict, kind FeatureKind) (bool, error) {
	name, _, err := o.pdf.DereferenceNameEntry(dictionary, "S")
	if err != nil {
		return false, fmt.Errorf("action kind: %w", err)
	}

	if name == nil {
		return false, nil
	}

	if *name != "JavaScript" {
		return kind == FeatureCatalogActions || *name != "GoTo" && *name != "URI", nil
	}

	script, err := o.pdf.Dereference(dictionary["JS"])
	if err != nil {
		return false, fmt.Errorf("JavaScript action: %w", err)
	}

	return materialScript(script)
}

func materialScript(object types.Object) (bool, error) {
	switch value := object.(type) {
	case types.StreamDict:
		if err := value.Decode(); err != nil {
			return false, fmt.Errorf("JavaScript stream: %w", err)
		}

		return len(value.Content) > 0, nil
	case types.HexLiteral:
		content, err := value.Bytes()
		if err != nil {
			return false, fmt.Errorf("JavaScript hex string: %w", err)
		}

		return len(content) > 0, nil
	case types.StringLiteral:
		return value.Value() != "", nil
	default:
		return false, nil
	}
}
