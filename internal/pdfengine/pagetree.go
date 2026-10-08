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

type (
	// inheritedAttrs are the page attributes a page-tree node passes to its descendants (ISO 32000-2, 7.7.3.4).
	// The values are the raw, possibly indirect, objects.
	inheritedAttrs struct {
		resources, mediaBox, cropBox, rotate types.Object
	}

	// pageVisitor receives each leaf page in document order.
	pageVisitor func(ref types.IndirectRef, page types.Dict, inherited inheritedAttrs) error

	// pageWalker walks a page tree once, in document order, using only exported dictionary primitives.
	// Its cost is linear in the number of nodes; it never asks pdfcpu to look a page up by number, which
	// would be quadratic.
	pageWalker struct {
		pdf   *model.Context
		seen  map[int]struct{}
		visit pageVisitor
	}
)

const (
	// maxPageTreeDepth bounds page-tree recursion. Real documents are a few levels deep; the bound turns a
	// hostile or corrupt tree into an error instead of a stack overflow.
	maxPageTreeDepth = 64

	// Dictionary keys the engine reads or keeps; Dests is both a catalog key and a /Names tree key.
	keyResources = "Resources"
	keyMediaBox  = "MediaBox"
	keyCropBox   = "CropBox"
	keyRotate    = "Rotate"
	keyDests     = "Dests"
)

var (
	errTreeTooDeep     = errors.New("page tree is nested too deeply")
	errNodeRepeated    = errors.New("page tree node is reachable twice")
	errNodeMissing     = errors.New("page tree node is missing")
	errKidNotReference = errors.New("page tree node has a kid that is not an indirect reference")
)

// override returns the attributes in effect below node.
func (a inheritedAttrs) override(node types.Dict) inheritedAttrs {
	for _, entry := range []struct {
		target *types.Object
		key    string
	}{
		{&a.resources, keyResources}, {&a.mediaBox, keyMediaBox}, {&a.cropBox, keyCropBox}, {&a.rotate, keyRotate},
	} {
		if value, found := node.Find(entry.key); found {
			*entry.target = value
		}
	}

	return a
}

// materialize copies every inherited attribute the page lacks onto the page dictionary.
func (a inheritedAttrs) materialize(page types.Dict) {
	for _, entry := range []struct {
		value types.Object
		key   string
	}{
		{a.resources, keyResources}, {a.mediaBox, keyMediaBox}, {a.cropBox, keyCropBox}, {a.rotate, keyRotate},
	} {
		if _, found := page.Find(entry.key); !found && entry.value != nil {
			page[entry.key] = entry.value.Clone()
		}
	}
}

// walkPages calls visit for every leaf page below root. A node reachable twice, a node that is not a
// dictionary, a kid that is not an indirect reference, or excessive depth is an error.
func walkPages(ctx context.Context, pdf *model.Context, root types.IndirectRef, visit pageVisitor) error {
	walker := pageWalker{pdf: pdf, seen: map[int]struct{}{}, visit: visit}

	return walker.walk(ctx, root, inheritedAttrs{}, 0)
}

func (w *pageWalker) walk(ctx context.Context, ref types.IndirectRef, inherited inheritedAttrs, depth int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("walk page tree: %w", err)
	}

	if depth > maxPageTreeDepth {
		return errTreeTooDeep
	}

	number := ref.ObjectNumber.Value()
	if _, repeated := w.seen[number]; repeated {
		return fmt.Errorf(causeNumberFormat, errNodeRepeated, number)
	}

	w.seen[number] = struct{}{}

	node, err := w.pdf.DereferenceDict(ref)
	if err != nil {
		return fmt.Errorf("page tree node %d: %w", number, err)
	}

	if node == nil {
		return fmt.Errorf(causeNumberFormat, errNodeMissing, number)
	}

	kidsObject, isNode := node.Find("Kids")
	if !isNode {
		return w.visit(ref, node, inherited)
	}

	kids, err := w.pdf.DereferenceArray(kidsObject)
	if err != nil {
		return fmt.Errorf("page tree node %d kids: %w", number, err)
	}

	below := inherited.override(node)

	for _, kid := range kids {
		kidRef, isRef := kid.(types.IndirectRef)
		if !isRef {
			return fmt.Errorf(causeNumberFormat, errKidNotReference, number)
		}

		if walkErr := w.walk(ctx, kidRef, below, depth+1); walkErr != nil {
			return walkErr
		}
	}

	return nil
}
