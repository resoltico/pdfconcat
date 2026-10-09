// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	fitNoZoomFlag           = 8
	fitNoRotateFlag         = 16
	fitKnownAnnotationFlags = 1023
	fitQuadEntries          = 8
)

func (i *fitInspector) annotations(ctx context.Context, page types.Dict, fit PageFit) error {
	annotations, err := i.pdf.DereferenceArrayContext(ctx, page["Annots"])
	if err != nil {
		return fmt.Errorf("/Annots: %w", err)
	}

	if ref, indirect := page["Annots"].(types.IndirectRef); indirect && len(annotations) > 0 {
		if i.annotationArrays[ref] {
			return fmt.Errorf("%w: /Annots array object %s is shared between pages", errFitUnsupported, ref.PDFString())
		}

		i.annotationArrays[ref] = true
	}

	for index, object := range annotations {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("fit annotation canceled: %w", err)
		}

		annotation, readErr := i.pdf.DereferenceDictContext(ctx, object)
		if readErr != nil {
			return fmt.Errorf("/Annots[%d] object %s: %w", index, fitObjectIdentity(object), readErr)
		}

		if err = i.annotationOwner(annotation, object); err != nil {
			return fmt.Errorf("/Annots[%d]: %w", index, err)
		}

		if err = i.link(ctx, annotation, fit); err != nil {
			return fmt.Errorf("/Annots[%d] object %s: %w", index, fitObjectIdentity(object), err)
		}
	}

	return nil
}

func (i *fitInspector) annotationOwner(annotation types.Dict, object types.Object) error {
	if ref, indirect := object.(types.IndirectRef); indirect {
		if i.annotationObjects[ref] {
			return fmt.Errorf("%w: annotation object %s has repeated ownership", errFitUnsupported, ref.PDFString())
		}

		i.annotationObjects[ref] = true
	}

	if parent, found := annotation.Find("P"); found && parent != nil && parent != i.owner {
		return fmt.Errorf("%w: annotation /P must refer to its source page", errFitUnsupported)
	}

	return nil
}

func fitObjectIdentity(object types.Object) string {
	if reference, indirect := object.(types.IndirectRef); indirect {
		return reference.PDFString()
	}

	return "direct object"
}

func (i *fitInspector) link(ctx context.Context, link types.Dict, fit PageFit) error {
	subtype, _, err := i.pdf.DereferenceNameEntryContext(ctx, link, keySubtype)
	if err != nil {
		return fmt.Errorf("link /Subtype: %w", err)
	}

	if subtype == nil || *subtype != "Link" {
		return fmt.Errorf("%w: /Subtype %v: only unpainted Link annotations are supported", errFitUnsupported, subtype)
	}

	for _, key := range []string{"AP", "AA", "Popup", "Measure", "PA", "OC"} {
		if featureErr := i.nonempty(ctx, link, key); featureErr != nil {
			return featureErr
		}
	}

	if flagsErr := i.linkFlags(ctx, link); flagsErr != nil {
		return flagsErr
	}

	if borderErr := i.linkBorder(ctx, link); borderErr != nil {
		return borderErr
	}

	if actionErr := i.linkAction(ctx, link); actionErr != nil {
		return actionErr
	}

	_, err = i.linkCoordinates(ctx, link, fit)

	return err
}

func (i *fitInspector) linkFlags(ctx context.Context, link types.Dict) error {
	flags := 0

	if object, found := link.Find("F"); found && object != nil {
		value, err := i.pdf.DereferenceNumberContext(ctx, object)
		if err != nil {
			return fmt.Errorf("link /F: %w", err)
		}

		if !finiteAppearanceNumber(value) || value != math.Trunc(value) || value < 0 || value > fitKnownAnnotationFlags {
			return fmt.Errorf("%w: Link /F flags must be valid integers", errFitUnsupported)
		}

		flags = int(value)
	}

	if flags&(fitNoZoomFlag|fitNoRotateFlag) != 0 {
		return fmt.Errorf("%w: Link /F NoZoom/NoRotate behavior cannot be fitted", errFitUnsupported)
	}

	return nil
}

func (i *fitInspector) linkAction(ctx context.Context, link types.Dict) error {
	action, err := i.pdf.DereferenceContext(ctx, link["A"])
	if err != nil {
		return fmt.Errorf("link /A: %w", err)
	}

	destination, err := i.pdf.DereferenceContext(ctx, link["Dest"])
	if err != nil {
		return fmt.Errorf("link /Dest: %w", err)
	}

	if action != nil && destination != nil {
		return fmt.Errorf("%w: Link mixes /A and /Dest", errFitUnsupported)
	}

	if destination != nil {
		return i.destination(ctx, link["Dest"], newFitGraphWalk(), 0)
	}

	return i.linkActionNode(ctx, link["A"], newFitGraphWalk(), 0)
}

func (i *fitInspector) linkActionNode(ctx context.Context, object types.Object, walk *fitGraphWalk, depth int) error {
	if err := walk.enter(ctx, object, depth); err != nil {
		return err
	}
	defer walk.leave(object)

	dict, err := i.pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return fmt.Errorf("link /A object %s: %w", fitObjectIdentity(object), err)
	}

	if len(dict) == 0 {
		return fmt.Errorf(
			"%w: Link /A object %s must be a URI or local GoTo action dictionary",
			errFitUnsupported,
			fitObjectIdentity(object),
		)
	}

	if actionErr := i.singleLinkAction(ctx, dict, walk, depth); actionErr != nil {
		return actionErr
	}

	if nextErr := i.linkNext(ctx, dict[keyNext], walk, depth+1); nextErr != nil {
		return fmt.Errorf("/%s object %s: %w", keyNext, fitObjectIdentity(dict[keyNext]), nextErr)
	}

	return nil
}

func (i *fitInspector) singleLinkAction(ctx context.Context, dict types.Dict, walk *fitGraphWalk, depth int) error {
	if object := dict["Type"]; object != nil {
		value, err := i.pdf.DereferenceContext(ctx, object)
		if err != nil {
			return fmt.Errorf("link action /Type: %w", err)
		}

		if value != nil && value != types.Name("Action") {
			return fmt.Errorf("%w: Link action /Type must be /Action", errFitUnsupported)
		}
	}

	kind, _, err := i.pdf.DereferenceNameEntryContext(ctx, dict, "S")
	if err != nil {
		return fmt.Errorf("link action /S: %w", err)
	}

	if kind == nil {
		return fmt.Errorf("%w: Link /A /S is missing or malformed", errFitUnsupported)
	}

	switch *kind {
	case keyURI:
		return i.fitURI(ctx, dict)
	case "GoTo":
		if sdErr := i.nonempty(ctx, dict, "SD"); sdErr != nil {
			return sdErr
		}

		return i.destination(ctx, dict["D"], walk, depth+1)
	default:
		return fmt.Errorf("%w: Link /A /S /%s is unsupported", errFitUnsupported, *kind)
	}
}

func (i *fitInspector) linkNext(ctx context.Context, object types.Object, walk *fitGraphWalk, depth int) error {
	if err := walk.visit(ctx); err != nil {
		return err
	}

	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fmt.Errorf("fit action reference: %w", err)
	}

	switch next := value.(type) {
	case nil:
		return nil
	case types.Dict:
		if len(next) == 0 {
			return nil
		}

		return i.linkActionNode(ctx, object, walk, depth)
	case types.Array:
		return i.linkNextArray(ctx, object, next, walk, depth)
	default:
		return fmt.Errorf("%w: /Next must be an action dictionary or array", errFitUnsupported)
	}
}

func (i *fitInspector) linkNextArray(ctx context.Context, object types.Object, next types.Array, walk *fitGraphWalk, depth int) error {
	if len(next) == 0 {
		return nil
	}

	if enterErr := walk.enter(ctx, object, depth); enterErr != nil {
		return enterErr
	}
	defer walk.leave(object)

	for index, child := range next {
		if visitErr := walk.visit(ctx); visitErr != nil {
			return visitErr
		}

		if actionErr := i.linkActionNode(ctx, child, walk, depth+1); actionErr != nil {
			return fmt.Errorf("[%d] object %s: %w", index, fitObjectIdentity(child), actionErr)
		}
	}

	return nil
}

func (i *fitInspector) fitURI(ctx context.Context, action types.Dict) error {
	mapped, err := i.pdf.DereferenceContext(ctx, action["IsMap"])
	if err != nil {
		return fmt.Errorf("uri /IsMap: %w", err)
	}

	if mapped != nil && mapped != types.Boolean(false) {
		return fmt.Errorf("%w: URI /IsMap must be absent or false; click coordinates cannot be fitted", errFitUnsupported)
	}

	target, err := i.pdf.DereferenceStringEntryBytesContext(ctx, action, keyURI)
	if err != nil {
		return fmt.Errorf("uri target: %w", err)
	}

	if !absoluteURI(target) {
		return fmt.Errorf(
			"%w: /URI target must be stable and absolute; relative/base-dependent targets cannot be fitted",
			errFitUnsupported,
		)
	}

	return nil
}
