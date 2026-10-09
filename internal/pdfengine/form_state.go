// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	formDefaults struct {
		appearance    *formAppearance
		kind          string
		justification int
	}
	fieldDefaults struct {
		dict       types.Dict
		variableAP types.Dict
		variable   *variableAppearancePlan
		button     *buttonAppearancePlan
		defaults   formDefaults
		widget     bool
	}
	formState struct {
		root       types.Dict
		resources  map[string]types.Dict
		defaults   []fieldDefaults
		procSet    types.Array
		regenerate bool
	}
	formTraversal struct {
		pdf   *model.Context
		state *formState
		seen  map[types.IndirectRef]bool
	}
)

const (
	keyFields     = "Fields"
	keyParent     = "Parent"
	widgetSubtype = "Widget"
)

var errFormState = errors.New("unsupported static form state")

// checkFormState detects deterministic unsupported form shapes before backend validation repairs them.
// Inspection and import share this analysis; appearance resources retain their original scopes here.
func checkFormState(ctx context.Context, pdf *model.Context) error {
	_, err := analyzeForm(ctx, pdf)
	return err
}

func analyzeForm(ctx context.Context, pdf *model.Context) (*formState, error) {
	object, found := pdf.RootDict.Find(keyAcroForm)
	if !found {
		return &formState{}, nil
	}

	root, err := pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return nil, fmt.Errorf("%w: AcroForm: %w", errFormState, err)
	}

	if len(root) == 0 {
		return &formState{}, nil
	}

	state := &formState{root: root}
	if flagErr := state.readRegeneration(ctx, pdf); flagErr != nil {
		return nil, flagErr
	}

	if err = state.readResources(ctx, pdf); err != nil {
		return nil, fmt.Errorf("%w: %w", errFormState, err)
	}

	defaults, err := state.readDefaults(ctx, pdf, root, formDefaults{})
	if err != nil {
		return nil, fmt.Errorf("%w: AcroForm defaults: %w", errFormState, err)
	}

	state.defaults = append(state.defaults, fieldDefaults{dict: root, defaults: defaults})

	fields, err := pdf.DereferenceArrayContext(ctx, root[keyFields])
	if err != nil {
		return nil, fmt.Errorf("%w: AcroForm Fields: %w", errFormState, err)
	}

	walk := formTraversal{pdf: pdf, state: state, seen: map[types.IndirectRef]bool{}}
	if err = walk.fields(ctx, fields, nil, defaults, 1); err != nil {
		return nil, fmt.Errorf("%w: %w", errFormState, err)
	}

	return state, nil
}

func (s *formState) readRegeneration(ctx context.Context, pdf *model.Context) error {
	flag, exists := s.root.Find("NeedAppearances")
	if !exists || flag == nil {
		return nil
	}

	value, err := pdf.DereferenceContext(ctx, flag)
	if err != nil {
		return fmt.Errorf("%w: NeedAppearances: %w", errFormState, err)
	}

	if value == nil {
		return nil
	}

	boolean, valid := value.(types.Boolean)
	if !valid {
		return fmt.Errorf("%w: NeedAppearances must be boolean", errFormState)
	}

	s.regenerate = bool(boolean)

	return nil
}

func (s *formState) readDefaults(ctx context.Context, pdf *model.Context, dict types.Dict, inherited formDefaults) (formDefaults, error) {
	if err := inherited.readAppearance(ctx, pdf, dict, s.resources); err != nil {
		return inherited, err
	}

	if err := inherited.readJustification(ctx, pdf, dict); err != nil {
		return inherited, err
	}

	kind, err := fieldKind(ctx, pdf, dict, inherited.kind)
	if err != nil {
		return inherited, err
	}

	inherited.kind = kind

	return inherited, nil
}

func (d *formDefaults) readAppearance(ctx context.Context, pdf *model.Context, dict types.Dict, resources map[string]types.Dict) error {
	if _, found := dict.Find("DA"); !found {
		return nil
	}

	text, err := pdf.DereferenceStringEntryBytesContext(ctx, dict, "DA")
	if err != nil {
		return fmt.Errorf("default appearance: %w", err)
	}

	if text == nil {
		return fmt.Errorf("%w: default appearance must be a string", errFormState)
	}

	appearance, err := parseFormAppearance(ctx, string(text), resources)
	if err != nil {
		return err
	}

	d.appearance = &appearance

	return nil
}

func (d *formDefaults) readJustification(ctx context.Context, pdf *model.Context, dict types.Dict) error {
	if _, found := dict.Find("Q"); !found {
		return nil
	}

	value, err := pdf.DereferenceContext(ctx, dict["Q"])
	if err != nil {
		return fmt.Errorf("field justification: %w", err)
	}

	integer, ok := value.(types.Integer)
	if !ok || integer < 0 || integer > 2 {
		return fmt.Errorf("%w: field justification /Q must be an integer in 0..2: %v", errFormState, value)
	}

	d.justification = integer.Value()

	return nil
}

func (w *formTraversal) fields(
	ctx context.Context,
	fields types.Array,
	parent *types.IndirectRef,
	inherited formDefaults,
	depth int,
) error {
	if err := w.pdf.CheckRecursionDepth("form fields", depth); err != nil {
		return fmt.Errorf("form depth: %w", err)
	}

	for _, object := range fields {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("form traversal: %w", err)
		}

		ref, ok := object.(types.IndirectRef)
		if !ok {
			return fmt.Errorf("%w: form field must be an indirect dictionary: %T", errFormState, object)
		}

		if w.seen[ref] {
			return fmt.Errorf("%w: form field %s has cyclic or shared ancestry", errFormState, ref.PDFString())
		}

		w.seen[ref] = true
		if err := w.field(ctx, ref, parent, inherited, depth); err != nil {
			return fmt.Errorf("form traversal: %w", err)
		}
	}

	return nil
}

func (w *formTraversal) field(
	ctx context.Context,
	ref types.IndirectRef,
	parent *types.IndirectRef,
	inherited formDefaults,
	depth int,
) error {
	dict, err := w.pdf.DereferenceDictContext(ctx, ref)
	if err != nil {
		return fmt.Errorf("form field %s: %w", ref.PDFString(), err)
	}

	if dict == nil {
		return fmt.Errorf("%w: form field %s is not a dictionary", errFormState, ref.PDFString())
	}

	if parentErr := checkFormParent(dict, parent); parentErr != nil {
		return parentErr
	}

	if _, found := dict.Find("DR"); found {
		return fmt.Errorf("%w: field-local /DR is unsupported; use AcroForm resources and independent /AP resources", errFormState)
	}

	defaults, err := w.state.readDefaults(ctx, w.pdf, dict, inherited)
	if err != nil {
		return fmt.Errorf("field %s: %w", ref.PDFString(), err)
	}

	entry, entryErr := w.state.fieldEntry(ctx, w.pdf, dict, defaults)
	if entryErr != nil {
		return entryErr
	}

	w.state.defaults = append(w.state.defaults, entry)

	children, err := w.pdf.DereferenceArrayContext(ctx, dict["Kids"])
	if err != nil {
		return fmt.Errorf("field %s Kids: %w", ref.PDFString(), err)
	}

	if len(children) == 0 {
		return nil
	}

	return w.fields(ctx, children, &ref, defaults, depth+1)
}

func (s *formState) fieldEntry(ctx context.Context, pdf *model.Context, dict types.Dict, defaults formDefaults) (fieldDefaults, error) {
	entry := fieldDefaults{dict: dict, defaults: defaults}
	if (defaults.kind == "Tx" || defaults.kind == "Ch") && (defaults.appearance == nil || !defaults.appearance.hasFont) {
		return entry, fmt.Errorf("%w: field has no effective font-bearing default appearance", errFormState)
	}

	subtype, _, err := pdf.DereferenceNameEntryContext(ctx, dict, keySubtype)
	if err != nil {
		return entry, fmt.Errorf("%w: field subtype: %w", errFormState, err)
	}

	entry.widget = subtype != nil && *subtype == widgetSubtype
	if s.regenerate && entry.widget {
		if captureErr := entry.captureRegeneration(ctx, pdf, s.resources); captureErr != nil {
			return entry, captureErr
		}
	}

	return entry, nil
}

func (f *fieldDefaults) captureRegeneration(ctx context.Context, pdf *model.Context, resources map[string]types.Dict) error {
	if f.defaults.kind == buttonFieldType {
		plan, err := compileButtonAppearance(ctx, pdf, f.dict)
		f.button = plan

		return err
	}

	if f.defaults.kind != "Tx" && f.defaults.kind != "Ch" {
		return nil
	}

	appearance, err := pdf.DereferenceDictContext(ctx, f.dict["AP"])
	if err != nil {
		return fmt.Errorf("%w: variable widget appearance: %w", errFormState, err)
	}

	needed := false

	for mode, object := range appearance {
		if mode == "N" {
			continue
		}

		if mode != "D" && mode != "R" {
			return fmt.Errorf("%w: unsupported alternate appearance entry /%s", errFormState, mode)
		}

		if alternateErr := checkAlternateAppearance(ctx, pdf, object); alternateErr != nil {
			return alternateErr
		}

		needed = true
	}

	if !needed {
		return nil
	}

	plan, err := compileVariableAppearance(ctx, pdf, f.dict, f.defaults, resources)
	f.variable, f.variableAP = plan, appearance

	return err
}

func checkFormParent(dict types.Dict, parent *types.IndirectRef) error {
	object, found := dict.Find(keyParent)
	if parent == nil {
		if found {
			return fmt.Errorf("%w: root form field has an unexpected Parent: %v", errFormState, object)
		}

		return nil
	}

	ref, ok := object.(types.IndirectRef)
	if !found || !ok || ref != *parent {
		return fmt.Errorf("%w: form field Parent does not match its containing Kids array: %v", errFormState, object)
	}

	return nil
}

// normalize makes inherited form defaults explicit while scopes are separate. Hex strings preserve
// exact DA bytes without confusing PDF string escaping with the DA content syntax.
func (s *formState) normalize(ctx context.Context, pdf *model.Context, names map[string]map[string]string) error {
	for _, field := range s.defaults {
		if field.button != nil {
			if buttonErr := field.button.apply(ctx, pdf, field.dict); buttonErr != nil {
				return buttonErr
			}
		}

		if fieldErr := s.normalizeFieldDefaults(ctx, pdf, &field, names); fieldErr != nil {
			return fieldErr
		}
	}
	// The merged flag is global. Source requests have been materialized as button states or
	// scoped to missing variable-widget AP above.
	s.root["NeedAppearances"] = types.Boolean(false)

	return nil
}

func (f *fieldDefaults) applyVariableAppearance(ctx context.Context, pdf *model.Context) error {
	if f.variable == nil {
		f.dict.Delete("AP")
		return nil
	}

	normal, err := f.variable.stream(ctx, pdf)
	if err != nil {
		return err
	}

	appearance := types.Dict{"N": *normal}

	for mode, object := range f.variableAP {
		if mode != "N" {
			appearance[mode] = object.Clone()
		}
	}

	f.dict["AP"] = appearance

	return nil
}

func checkAlternateAppearance(ctx context.Context, pdf *model.Context, object types.Object) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("alternate appearance: %w", err)
	}

	value, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fmt.Errorf("%w: alternate appearance: %w", errFormState, err)
	}

	switch decoded := value.(type) {
	case types.StreamDict:
		return nil
	case types.Dict:
		for _, state := range decoded {
			entry, readErr := pdf.DereferenceContext(ctx, state)
			if readErr != nil {
				return fmt.Errorf("%w: alternate state: %w", errFormState, readErr)
			}

			if _, valid := entry.(types.StreamDict); !valid {
				return fmt.Errorf("%w: alternate state must be a real stream", errFormState)
			}
		}

		return nil
	default:
		return fmt.Errorf("%w: alternate appearance must be a real stream or state dictionary", errFormState)
	}
}

func (s *formState) normalizeFieldDefaults(
	ctx context.Context,
	pdf *model.Context,
	field *fieldDefaults,
	names map[string]map[string]string,
) error {
	defaults := field.defaults
	if defaults.appearance != nil {
		text := defaults.appearance.renamed(names)
		field.dict["DA"] = types.HexLiteral(hex.EncodeToString([]byte(text)))
	}

	if defaults.kind == "Tx" || defaults.kind == "Ch" {
		field.dict["Q"] = types.Integer(defaults.justification)
		// A source's explicit regeneration request applies only to its own variable widgets.
		// N-only widgets use ordinary missing-AP regeneration. Alternate modes retain their
		// source references beside a compiled normal appearance with independent resources.
		if s.regenerate && field.widget {
			if variableErr := field.applyVariableAppearance(ctx, pdf); variableErr != nil {
				return variableErr
			}
		}
	}

	return nil
}
