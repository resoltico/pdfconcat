// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	formResourcesFailure = "form resources: %w"
	keyEncoding          = "Encoding"
	keyProperties        = "Properties"
	keyProcSet           = "ProcSet"
	keyXObject           = "XObject"
	keyBaseFont          = "BaseFont"
	fontHelvetica        = "Helvetica"
)

func (s *formState) readResources(ctx context.Context, pdf *model.Context) error {
	s.resources = map[string]types.Dict{}

	resources, err := pdf.DereferenceDictContext(ctx, s.root["DR"])
	if err != nil {
		return fmt.Errorf("form DR must be a dictionary: %w", err)
	}

	for category, object := range resources {
		if categoryErr := s.readResourceCategory(ctx, pdf, category, object); categoryErr != nil {
			return categoryErr
		}
	}

	return nil
}

func (s *formState) readResourceCategory(ctx context.Context, pdf *model.Context, category string, object types.Object) error {
	if category == keyProcSet {
		return s.readProcSet(ctx, pdf, object)
	}

	bindings, err := pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return fmt.Errorf("form DR /%s: %w", category, err)
	}

	if bindings == nil {
		return fmt.Errorf("%w: form DR /%s must be a resource dictionary", errFormState, category)
	}

	for name, binding := range bindings {
		if bindingErr := checkFormResource(ctx, pdf, category, binding); bindingErr != nil {
			return fmt.Errorf("form DR /%s /%s: %w", category, name, bindingErr)
		}
	}

	s.resources[category] = bindings

	return nil
}

func (s *formState) readProcSet(ctx context.Context, pdf *model.Context, object types.Object) error {
	values, err := pdf.DereferenceArrayContext(ctx, object)
	if err != nil {
		return fmt.Errorf("form DR ProcSet: %w", err)
	}

	for _, value := range values {
		if _, ok := value.(types.Name); !ok {
			return fmt.Errorf("%w: form DR ProcSet must contain names: %T", errFormState, value)
		}
	}

	s.procSet = values

	return nil
}

func checkFormResource(ctx context.Context, pdf *model.Context, category string, object types.Object) error {
	value, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	switch category {
	case keyFont:
		return checkFormFont(ctx, pdf, value)
	case keyExtGState, keyProperties, keyEncoding:
		return checkResourceDictionary(value)
	case keyXObject, "Pattern":
		return checkResourceStream(value)
	case "ColorSpace":
		return checkResourceColorSpace(value)
	default:
		return fmt.Errorf("%w: unsupported form default resource category /%s", errFormState, category)
	}
}

func checkFormFont(ctx context.Context, pdf *model.Context, object types.Object) error {
	dict, ok := object.(types.Dict)
	if !ok {
		return fmt.Errorf("%w: font resource must be a dictionary: %T", errFormState, object)
	}

	subtype, _, err := pdf.DereferenceNameEntryContext(ctx, dict, keySubtype)
	if err != nil {
		return fmt.Errorf("font subtype: %w", err)
	}

	if subtype == nil {
		return fmt.Errorf("%w: font resource requires a Subtype", errFormState)
	}

	return nil
}

func checkResourceDictionary(object types.Object) error {
	if _, ok := object.(types.Dict); !ok {
		return fmt.Errorf("%w: resource must be a dictionary: %T", errFormState, object)
	}

	return nil
}

func checkResourceStream(object types.Object) error {
	switch object.(type) {
	case types.Dict, types.StreamDict:
		return nil
	default:
		return fmt.Errorf("%w: resource must be a dictionary or stream: %T", errFormState, object)
	}
}

func checkResourceColorSpace(object types.Object) error {
	switch object.(type) {
	case types.Name, types.Array:
		return nil
	default:
		return fmt.Errorf("%w: color-space resource must be a name or array: %T", errFormState, object)
	}
}

// prepareFormResources gives every resource binding a distinct occurrence namespace before merge.
// Actual object bindings are preserved; neither font labels nor encodings are used to deduplicate.
func prepareFormResources(ctx context.Context, pdf *model.Context, occurrence int) (*formState, error) {
	state, err := analyzeForm(ctx, pdf)
	if err != nil || state.root == nil {
		return state, err
	}

	names := map[string]map[string]string{}

	resources := types.Dict{}
	for category, bindings := range state.resources {
		keys := make([]string, 0, len(bindings))
		for name := range bindings {
			keys = append(keys, name)
		}

		slices.Sort(keys)

		mapped := types.Dict{}
		names[category] = map[string]string{}

		for index, name := range keys {
			if err = ctx.Err(); err != nil {
				return nil, fmt.Errorf(formResourcesFailure, err)
			}

			renamed := fmt.Sprintf("Form%d_%s%d", occurrence, category, index+1)
			names[category][name] = renamed
			mapped[renamed] = bindings[name]
		}

		resources[category] = mapped
	}

	if len(state.procSet) > 0 {
		resources[keyProcSet] = state.procSet
	}

	if normalizeErr := state.normalize(ctx, pdf, names); normalizeErr != nil {
		return nil, normalizeErr
	}

	state.root["DR"] = resources

	return state, nil
}

// mergeFormResources runs after pdfcpu renumbers imported references. Its merge deliberately keeps
// the first DR dictionary, so append the already namespaced later bindings to that dictionary here.
func mergeFormResources(ctx context.Context, pdf *model.Context, source *formState) error {
	if source == nil || source.root == nil {
		return nil
	}

	form, err := pdf.DereferenceDictContext(ctx, pdf.RootDict[keyAcroForm])
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	resources, err := pdf.DereferenceDictContext(ctx, form["DR"])
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	if resources == nil {
		resources = types.Dict{}
		form["DR"] = resources
	}

	imported, err := pdf.DereferenceDictContext(ctx, source.root["DR"])
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	for category, object := range imported {
		if categoryErr := mergeFormResourceCategory(ctx, pdf, resources, category, object); categoryErr != nil {
			return categoryErr
		}
	}

	return nil
}

func mergeFormResourceCategory(ctx context.Context, pdf *model.Context, resources types.Dict, category string, object types.Object) error {
	if category == keyProcSet {
		return mergeFormProcSet(ctx, pdf, resources, object)
	}

	bindings, err := pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	destination, err := pdf.DereferenceDictContext(ctx, resources[category])
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	if destination == nil {
		destination = types.Dict{}
		resources[category] = destination
	}

	maps.Copy(destination, bindings)

	return nil
}

func mergeFormProcSet(ctx context.Context, pdf *model.Context, resources types.Dict, object types.Object) error {
	imported, err := pdf.DereferenceArrayContext(ctx, object)
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	destination, err := pdf.DereferenceArrayContext(ctx, resources[keyProcSet])
	if err != nil {
		return fmt.Errorf(formResourcesFailure, err)
	}

	for _, value := range imported {
		if !slices.Contains(destination, value) {
			destination = append(destination, value)
		}
	}

	resources[keyProcSet] = destination

	return nil
}
