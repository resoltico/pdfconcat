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

type signatureWalker struct {
	pdf  *model.Context
	seen map[int]bool
}

// maxFieldDepth bounds malformed form/parent recursion without expanding signature content.
const (
	maxFieldDepth   = 64
	signatureType   = "Sig"
	buttonFieldType = "Btn"
)

var errSignatureState = errors.New("signature state cannot be preserved by assembly")

func signatureFailure(detail string) error {
	return fmt.Errorf("%w: %s; assembly rewrites signed bytes. Provide suitable unsigned inputs and sign the final document externally",
		errSignatureState, detail)
}

// checkSignatureState inspects reachable signature relationships before validator repairs.
// Empty unsigned signature fields are allowed; a present value or malformed signature state is not.
func checkSignatureState(ctx context.Context, pdf *model.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inspect signatures: %w", err)
	}

	if err := checkCatalogSignatures(pdf); err != nil {
		return err
	}

	form, err := pdf.DereferenceDict(pdf.RootDict[keyAcroForm])
	if err != nil {
		return fmt.Errorf("signature form lookup: %w", err)
	}

	if form != nil {
		walker := signatureWalker{pdf: pdf, seen: map[int]bool{}}
		if fieldErr := walker.fields(ctx, form[keyFields], "", 0); fieldErr != nil {
			return fieldErr
		}
	}

	root, err := pdf.Pages()
	if err != nil {
		return fmt.Errorf("signature page lookup: %w", err)
	}

	return walkPages(ctx, pdf, *root, func(_ types.IndirectRef, page types.Dict, _ inheritedAttrs) error {
		return checkWidgetSignatures(ctx, pdf, page)
	})
}

func checkCatalogSignatures(pdf *model.Context) error {
	permissions, err := pdf.DereferenceDict(pdf.RootDict["Perms"])
	if err != nil {
		return signatureFailure("malformed catalog /Perms")
	}

	for _, key := range []string{"DocMDP", "UR", "UR3"} {
		if _, present := permissions[key]; present {
			return signatureFailure("catalog /Perms /" + key)
		}
	}

	return nil
}

func (w *signatureWalker) fields(ctx context.Context, object types.Object, inherited string, depth int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inspect signature fields: %w", err)
	}

	fields, err := w.pdf.DereferenceArray(object)
	if err != nil {
		return fmt.Errorf("signature field array: %w", err)
	}

	if len(fields) > 0 && depth > maxFieldDepth {
		return fmt.Errorf("%w: field tree exceeds its supported depth", errFormState)
	}

	for _, field := range fields {
		if fieldErr := w.field(ctx, field, inherited, depth); fieldErr != nil {
			return fieldErr
		}
	}

	return nil
}

func (w *signatureWalker) field(ctx context.Context, object types.Object, inherited string, depth int) error {
	if reference, ok := object.(types.IndirectRef); ok {
		number := reference.ObjectNumber.Value()
		if w.seen[number] {
			return fmt.Errorf("%w: cyclic or shared field references", errFormState)
		}

		w.seen[number] = true
	}

	field, err := w.pdf.DereferenceDict(object)
	if err != nil {
		return fmt.Errorf("signature field lookup: %w", err)
	}

	if field == nil {
		return fmt.Errorf("%w: missing field dictionary", errFormState)
	}

	kind, kindErr := fieldKind(w.pdf, field, inherited)
	if valueErr := checkSignatureValue(w.pdf, field, kind); valueErr != nil {
		return valueErr
	}

	if kindErr != nil {
		return kindErr
	}

	return w.fields(ctx, field["Kids"], kind, depth+1)
}

func fieldKind(pdf *model.Context, field types.Dict, inherited string) (string, error) {
	_, present := field.Find("FT")
	if !present {
		return inherited, nil
	}

	value, _, err := pdf.DereferenceNameEntry(field, "FT")
	if err != nil {
		return "", fmt.Errorf("%w: malformed field /FT: %w", errFormState, err)
	}

	if value == nil {
		return inherited, nil
	}

	return string(*value), nil
}

func checkSignatureValue(pdf *model.Context, field types.Dict, kind string) error {
	object, present := field.Find("V")
	if !present {
		return nil
	}

	if kind == signatureType {
		return checkPopulatedSignature(pdf, object)
	}

	value, err := pdf.Dereference(object)
	if err != nil {
		return fmt.Errorf("signature field value: %w", err)
	}

	if dictionary, ok := value.(types.Dict); ok {
		return checkSignatureDictionary(pdf, dictionary)
	}

	return nil
}

func checkPopulatedSignature(pdf *model.Context, object types.Object) error {
	if reference, ok := object.(types.IndirectRef); ok {
		entry, found := pdf.FindTableEntryForIndRef(&reference)
		if !found || entry == nil || entry.Free {
			return signatureFailure("signature field /V references a missing or free object")
		}
	}

	value, err := pdf.Dereference(object)
	if err != nil || value != nil {
		return signatureFailure("signature field has a signature value or malformed /V")
	}

	return nil
}

func checkSignatureDictionary(pdf *model.Context, value types.Dict) error {
	for _, key := range []string{"ByteRange", "Contents"} {
		if _, found := value.Find(key); found {
			return signatureFailure("field value contains signature /" + key + " state")
		}
	}

	typeName, _, err := pdf.DereferenceNameEntry(value, keyType)
	if err != nil {
		return fmt.Errorf("signature value type: %w", err)
	}

	if typeName != nil && (*typeName == signatureType || *typeName == "DocTimeStamp") {
		return signatureFailure("field value references a signature dictionary")
	}

	return nil
}

func checkWidgetSignatures(ctx context.Context, pdf *model.Context, page types.Dict) error {
	annotations, err := pdf.DereferenceArray(page["Annots"])
	if err != nil {
		return fmt.Errorf("signature annotations: %w", err)
	}

	for _, object := range annotations {
		widget, widgetErr := pdf.DereferenceDict(object)
		if widgetErr != nil {
			return fmt.Errorf("signature widget: %w", widgetErr)
		}

		subtype, _, readErr := pdf.DereferenceNameEntry(widget, keySubtype)
		if readErr != nil {
			return fmt.Errorf("signature widget subtype: %w", readErr)
		}

		if subtype == nil || *subtype != widgetSubtype {
			continue
		}

		if parentErr := checkWidgetParents(ctx, pdf, widget); parentErr != nil {
			return parentErr
		}
	}

	return nil
}

func checkWidgetParents(ctx context.Context, pdf *model.Context, field types.Dict) error {
	if err := checkSignatureValue(pdf, field, ""); err != nil {
		return err
	}

	kind, err := effectiveWidgetKind(ctx, pdf, field)
	if err != nil {
		return err
	}

	for depth := 0; field != nil; depth++ {
		if signalErr := ctx.Err(); signalErr != nil {
			return fmt.Errorf("inspect signature widget: %w", signalErr)
		}

		if depth > maxFieldDepth {
			return fmt.Errorf("%w: widget parent chain is cyclic or too deep", errFormState)
		}

		kind, err = fieldKind(pdf, field, kind)
		if err != nil {
			return err
		}

		if valueErr := checkSignatureValue(pdf, field, kind); valueErr != nil {
			return valueErr
		}

		parent, parentErr := pdf.DereferenceDict(field[keyParent])
		if parentErr != nil {
			return fmt.Errorf("signature widget parent: %w", parentErr)
		}

		field = parent
	}

	return nil
}

func effectiveWidgetKind(ctx context.Context, pdf *model.Context, field types.Dict) (string, error) {
	for depth := 0; field != nil; depth++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("inspect inherited signature type: %w", err)
		}

		if depth > maxFieldDepth {
			return "", fmt.Errorf("%w: cyclic or deep widget ancestry", errFormState)
		}

		if _, found := field.Find("FT"); found {
			return fieldKind(pdf, field, "")
		}

		parent, err := pdf.DereferenceDict(field[keyParent])
		if err != nil {
			return "", fmt.Errorf("inherited signature parent: %w", err)
		}

		field = parent
	}

	return "", nil
}
