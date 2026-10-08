// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	// poolPage is one page of the pool with its inherited attributes written onto the dictionary.
	poolPage struct {
		dict types.Dict
		ref  types.IndirectRef
		used bool
	}

	// pool is the merged object graph that the final page order is cut from. The first imported document
	// is its base: pdfcpu merges later documents into the context of an already read one.
	pool struct {
		engine         *Engine
		pdf            *model.Context
		version        model.Version
		formOccurrence int
	}
)

var (
	errImportedPageCount = errors.New("the document changed since inspection")
	errMergeAddedNoTree  = errors.New("the merge did not append a page tree")
)

// importDocument merges the document at path into the pool and returns its pages in order. A document
// that has changed since inspection (a different page count) is an error, because the order was
// compiled against the inspected page count.
func (p *pool) importDocument(ctx context.Context, source int, path string, pages int) ([]*poolPage, error) {
	if err := canceled(ctx, source, path); err != nil {
		return nil, err
	}

	imported, err := p.engine.readContext(ctx, source, path)
	if err != nil {
		return nil, err
	}

	p.formOccurrence++

	form, err := prepareFormResources(ctx, imported, p.formOccurrence)
	if err != nil {
		if cancellation := canceled(ctx, source, path); cancellation != nil {
			return nil, cancellation
		}

		return nil, newError(CodeFormUnsupported, source, path, err)
	}

	// The tree to collect is the whole page tree of the base document, and for later documents the
	// subtree that the merge appended as the last kid of the pool's root.
	var top types.IndirectRef

	if p.pdf == nil {
		p.adopt(imported)
		top, _, err = p.root()
	} else {
		if err = p.merge(ctx, source, path, imported); err == nil {
			top, err = p.lastTree()
		}
	}

	if err != nil {
		return nil, assemblyError(ctx, source, path, err)
	}

	if resourceErr := mergeFormResources(p.pdf, form); resourceErr != nil {
		return nil, assemblyError(ctx, source, path, resourceErr)
	}

	var collected []*poolPage

	err = walkPages(ctx, p.pdf, top, func(ref types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		inherited.materialize(page)
		page.Delete("StructParents")

		collected = append(collected, &poolPage{ref: ref, dict: page})

		return nil
	})
	if err != nil {
		return nil, classifyWalk(ctx, path, err)
	}

	if len(collected) != pages {
		return nil, newError(CodeInvalid, source, path,
			fmt.Errorf("%w: it has %d pages, inspection found %d", errImportedPageCount, len(collected), pages))
	}

	return collected, nil
}

// adopt makes the first imported document the pool's base.
func (p *pool) adopt(base *model.Context) {
	p.pdf = base
	p.version = max(model.V17, base.XRefTable.Version())

	// pdfcpu replaces the catalog's name trees on merge unless the destination catalog already has a
	// /Names entry, which would lose the destinations of documents merged earlier.
	if _, found := base.RootDict.Find(keyNames); !found {
		base.RootDict[keyNames] = types.Dict{}
	}

	p.applyVersion()
}

func (p *pool) merge(ctx context.Context, source int, path string, imported *model.Context) error {
	// pdfcpu refuses to merge a PDF 2.0 document into a pre-2.0 destination, so raise the pool first.
	p.version = max(p.version, imported.XRefTable.Version())
	p.applyVersion()

	if err := pdfcpu.MergeXRefTables(ctx, path, imported, p.pdf, false, false); err != nil {
		if ctx.Err() != nil {
			return newError(CodeCanceled, source, path, ctx.Err())
		}

		return newError(CodeAssemblyFailed, source, path, fmt.Errorf("merge: %w", err))
	}

	return nil
}

func (p *pool) applyVersion() {
	version := p.version
	p.pdf.RootVersion = &version
}

// root returns the pool's page-tree root reference and dictionary.
func (p *pool) root() (types.IndirectRef, types.Dict, error) {
	ref, err := p.pdf.Pages()
	if err != nil {
		return types.IndirectRef{}, nil, fmt.Errorf(pageTreeRootFailureFormat, err)
	}

	dict, err := p.pdf.DereferenceDict(*ref)
	if err != nil {
		return types.IndirectRef{}, nil, fmt.Errorf(pageTreeRootFailureFormat, err)
	}

	return *ref, dict, nil
}

// lastTree returns the page-tree node that the latest merge appended to the pool's root.
func (p *pool) lastTree() (types.IndirectRef, error) {
	_, root, err := p.root()
	if err != nil {
		return types.IndirectRef{}, err
	}

	kids, err := p.pdf.DereferenceArray(root["Kids"])
	if err != nil {
		return types.IndirectRef{}, fmt.Errorf("page tree root kids: %w", err)
	}

	if len(kids) > 0 {
		if last, isRef := kids[len(kids)-1].(types.IndirectRef); isRef {
			return last, nil
		}
	}

	return types.IndirectRef{}, errMergeAddedNoTree
}

// assemblyError wraps a pool failure, reporting cancellation as such.
func assemblyError(ctx context.Context, source int, path string, err error) *Error {
	if ctx.Err() != nil {
		return newError(CodeCanceled, source, path, ctx.Err())
	}

	return newError(CodeAssemblyFailed, source, path, err)
}

// use returns the page for placing in the output: the pool page itself the first time, and for each
// further occurrence a clone of the page dictionary. Only the dictionary is cloned; contents, fonts
// and images stay shared. Sources whose pages hold annotations, actions or forms are imported per
// occurrence, so a repeated page never shares such objects with its original.
func (p *pool) use(page *poolPage) *poolPage {
	if !page.used {
		page.used = true

		return page
	}

	clone := make(types.Dict, len(page.dict))

	for key, value := range page.dict {
		if value != nil {
			value = value.Clone()
		}

		clone[key] = value
	}

	// InsertNew, unlike InsertObject, has no error return: the object table only appends the next number.
	entry := model.NewXRefTableEntryGen0(clone)
	entry.RefCount = 1
	number := p.pdf.InsertNew(*entry)

	return &poolPage{ref: *types.NewIndirectRef(number, 0), dict: clone, used: true}
}

// keepsCatalogKey reports whether a catalog entry survives assembly: the page tree, destinations, names
// and the form. Everything else (outlines, structure tree, metadata, page labels, open action,
// additional actions, permissions and so on) belongs to one source
// document and would describe the wrong pages. Rendering-affecting optional content and output intents
// are rejected at the read boundary, so dropping their catalog state cannot expose source marks.
func keepsCatalogKey(key string) bool {
	switch key {
	case keyType, "Pages", keyNames, keyDests, keyAcroForm:
		return true
	default:
		return false
	}
}

// reorder replaces the pool's page tree with one flat node holding final, in order, and reduces the
// document to what the assembly policy keeps.
//
// The root keeps no inheritable attributes: every page carries its own, and a stale /Rotate or /CropBox
// on the base document's root would otherwise apply to pages that have none.
func (p *pool) reorder(final []*poolPage) error {
	rootRef, root, err := p.root()
	if err != nil {
		return err
	}

	kids := make(types.Array, len(final))
	for index, page := range final {
		page.dict["Parent"] = rootRef
		kids[index] = page.ref
	}

	for _, key := range [...]string{keyResources, keyMediaBox, keyCropBox, keyRotate} {
		root.Delete(key)
	}

	root["Kids"] = kids
	root["Count"] = types.Integer(len(kids))
	p.pdf.PageCount = len(kids)

	for key := range p.pdf.RootDict {
		if !keepsCatalogKey(key) {
			p.pdf.RootDict.Delete(key)
		}
	}

	if namesErr := p.keepOnlyDestinationNames(); namesErr != nil {
		return namesErr
	}

	p.pdf.Info = nil

	return nil
}

// keepOnlyDestinationNames drops every name tree except the destinations: attachments, JavaScript and the
// other trees collide by name across documents and are not carried over. The catalog always has a /Names
// entry because adopt seeds one.
func (p *pool) keepOnlyDestinationNames() error {
	for tree := range p.pdf.Names {
		if tree != keyDests {
			delete(p.pdf.Names, tree)
		}
	}

	names, err := p.pdf.DereferenceDict(p.pdf.RootDict[keyNames])
	if err != nil {
		return fmt.Errorf("catalog /Names: %w", err)
	}

	for key := range names {
		if key != keyDests {
			names.Delete(key)
		}
	}

	return nil
}
