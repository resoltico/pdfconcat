// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestPageWalkerRequiresTypedRootAndActualPagesChildren(t *testing.T) {
	t.Parallel()

	for _, role := range []types.Object{
		nil, types.Integer(1), types.StringLiteral(pageNodeBranch),
		types.Name("unknown node role"), types.Name(pageNodeLeaf),
	} {
		pdf, _, _ := fitLinkFixture(t)

		root, err := pdf.PagesContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		node, err := pdf.DereferenceDictContext(t.Context(), *root)
		if err != nil {
			t.Fatal(err)
		}

		node[keyType] = role

		err = walkPages(
			t.Context(),
			pdf,
			*root,
			func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil },
		)
		if err == nil || !strings.Contains(err.Error(), keyType) {
			t.Fatalf("untyped/wrong root admitted: %v", err)
		}
	}

	for _, children := range []types.Object{nil, types.Integer(1), *types.NewIndirectRef(999, 0), types.Array{nil}} {
		pdf, _, _ := fitLinkFixture(t)

		root, err := pdf.PagesContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		node, err := pdf.DereferenceDictContext(t.Context(), *root)
		if err != nil {
			t.Fatal(err)
		}

		node[keyKids] = children

		err = walkPages(t.Context(), pdf, *root, func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil })
		if err == nil {
			t.Fatal("missing/null/nonarray/unreferenced Pages children admitted")
		}
	}
}

func TestPageWalkerDoesNotGuessChildTypesAndHonorsLazyTypeCancellation(t *testing.T) {
	t.Parallel()

	for _, role := range []types.Object{nil, types.Integer(1), types.Name("unknown node role")} {
		pdf, page, _ := fitLinkFixture(t)

		root, err := pdf.PagesContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		page[keyType] = role

		err = walkPages(t.Context(), pdf, *root, func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil })
		if err == nil {
			t.Fatal("missing/malformed/unknown leaf role guessed")
		}
	}

	pdf, page, _ := fitLinkFixture(t)

	root, err := pdf.PagesContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	page[keyType] = fitMalformedLazyReference(t, pdf)

	err = walkPages(
		t.Context(),
		pdf,
		*root,
		func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), keyType) {
		t.Fatalf("malformed lazy type lost location: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = walkPages(
		ctx,
		pdf,
		*root,
		func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("typed page traversal ignored cancellation: %v", err)
	}
}

func TestTypedPageTraversalNeverUsesPassiveLeafKidsAsChildren(t *testing.T) {
	t.Parallel()

	pdf, page, _ := fitLinkFixture(t)

	root, err := pdf.PagesContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	page[keyKids] = types.Array{*types.NewIndirectRef(999, 0), *root}
	visited := 0

	err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, actual types.Dict, _ inheritedAttrs) error {
		visited++

		if actual[keyContents] == nil {
			t.Fatal("real leaf content hidden")
		}

		return nil
	})
	if err != nil || visited != 1 {
		t.Fatalf("passive leaf key created edges/suppressed leaf: visits%d err%v", visited, err)
	}

	if err = checkSourcePageRoles(t.Context(), pdf); !errors.Is(err, errPageKidsAmbiguity) {
		t.Fatalf("sourceadmissibilitymistakenforstructuraltraversal: %v", err)
	}
}
