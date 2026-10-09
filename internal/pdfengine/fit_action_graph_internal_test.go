// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	fitRemoteGoToFixture      = "GoToR"
	fitActionFirstPageContext = "source page 1"
)

func fitURIAction() types.Dict {
	return types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget)}
}

func fitLocalAction() types.Dict {
	return types.Dict{"S": types.Name(fitGoToFixture), "D": types.Array{*types.NewIndirectRef(3, 0), types.Name(fitDestinationMode)}}
}

func fitIndirect(t *testing.T, pdf *model.Context, object types.Object) types.IndirectRef {
	t.Helper()

	number, err := pdf.InsertObject(object)
	if err != nil {
		t.Fatal(err)
	}

	return *types.NewIndirectRef(number, 0)
}

func TestFitActionGraphPreservesSafeOrderedTreesAndSharedAcyclicNodes(t *testing.T) {
	t.Parallel()

	for _, next := range []types.Object{
		nil,
		types.Dict{},
		types.Array{},
		fitURIAction(), fitLocalAction(),
		types.Dict{"S": types.Name(fitGoToFixture), "D": fitLocalAction()["D"], "SD": types.Array{}},
		types.Array{fitURIAction(), fitLocalAction()},
		types.Array{
			types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget), keyNext: fitLocalAction()},
			fitURIAction(),
		},
	} {
		pdf, _, link := fitLinkFixture(t)
		root := fitURIAction()
		root[keyNext], root[keyType] = next, types.Name("Action")
		shared := fitIndirect(t, pdf, root)
		link["A"] = types.Dict{
			"S":     types.Name(keyURI),
			keyURI:  types.StringLiteral(fitAbsoluteTarget),
			keyNext: types.Array{shared, shared},
		}
		before := link.PDFString()

		if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); err != nil {
			t.Fatalf("safe ordered graph refused: %v", err)
		}

		if link.PDFString() != before {
			t.Fatal("admission walk rewrote action order, objects or target bytes")
		}
	}
}

func TestFitActionGraphRejectsMaterialMalformedAndUnsupportedSecondaryActions(t *testing.T) {
	t.Parallel()

	for _, next := range []types.Object{
		types.Integer(0), types.StringLiteral(""), types.Name("URI"),
		types.Array{nil},
		types.Array{types.Dict{}},
		types.Array{types.Array{}},
		types.Dict{keyNext: fitURIAction()},
		types.Dict{"S": types.Name("JavaScript")},
		types.Array{fitURIAction(), types.Dict{"S": types.Name("Launch")}},
		types.Dict{"S": types.Name(fitRemoteGoToFixture), "D": types.Array{types.Integer(0), types.Name(fitDestinationMode)}},
		types.Dict{"S": types.Name(keyURI)},
		types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget), keyType: types.Name("Wrong")},
		types.Dict{
			"S": types.Name(fitGoToFixture),
			"D": types.Array{*types.NewIndirectRef(3, 0), types.Name("XYZ"), types.Integer(0), types.Integer(0), types.Integer(1)},
		},
	} {
		pdf, _, link := fitLinkFixture(t)
		root := fitURIAction()
		root[keyNext] = next
		link["A"] = root

		_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
		if !errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), "/Next") ||
			!strings.Contains(err.Error(), fitActionFirstPageContext) {
			t.Fatalf("unsupported secondary graph lost context or passed: %v", err)
		}
	}
}

func TestFitActionGraphRejectsCyclesDepthAndRepeatedExpansionLimits(t *testing.T) {
	t.Parallel()
	pdf, _, link := fitLinkFixture(t)
	root := fitURIAction()
	root[keyNext] = fitIndirect(t, pdf, root)
	link["A"] = root

	assertFitGraphRefused(t, pdf, "cyclic")

	root = fitURIAction()
	for range maxFeatureDepth + 1 {
		parent := fitURIAction()
		parent[keyNext] = root
		root = parent
	}

	link["A"] = root

	assertFitGraphRefused(t, pdf, "levels")

	root = fitURIAction()
	children := make(types.Array, maxFitGraphVisits)

	shared := fitIndirect(t, pdf, fitURIAction())
	for index := range children {
		children[index] = shared
	}

	root[keyNext], link["A"] = children, root

	assertFitGraphRefused(t, pdf, "expanded nodes/edges")
}

func assertFitGraphRefused(t *testing.T, pdf *model.Context, reason string) {
	t.Helper()

	_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
	if !errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), reason) {
		t.Fatalf("graph guard did not reject %s: %v", reason, err)
	}
}

func TestFitActionGraphCancellationInterruptsMaterialBranches(t *testing.T) {
	t.Parallel()
	pdf, _, _ := fitLinkFixture(t)
	inspector := fitInspector{pdf: pdf}
	root := fitURIAction()
	root[keyNext] = types.Array{fitURIAction(), fitURIAction(), fitURIAction()}
	probe := newFormCheckpointContext(t.Context(), t)
	probe.remaining.Store(math.MaxInt64)

	if err := inspector.linkActionNode(probe, root, newFitGraphWalk(), 0); err != nil {
		t.Fatalf("uncancelled graph probe failed: %v", err)
	}

	checkpoints := math.MaxInt64 - probe.remaining.Load()
	completed := false

	for budget := range checkpoints + 1 {
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		err := inspector.linkActionNode(ctx, root, newFitGraphWalk(), 0)
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("graph cancellation became a policy failure: %v", err)
		}
	}

	if !completed {
		t.Fatal("uncancelled action graph did not finish within its measured checkpoints")
	}
}

func TestFitActionGraphHonorsCancellationAtEachVisit(t *testing.T) {
	t.Parallel()
	pdf, _, _ := fitLinkFixture(t)
	inspector := fitInspector{pdf: pdf}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, check := range []func() error{
		func() error { return inspector.linkActionNode(ctx, fitURIAction(), newFitGraphWalk(), 0) },
		func() error { return inspector.linkNext(ctx, types.Array{fitURIAction()}, newFitGraphWalk(), 0) },
		func() error { return inspector.destination(ctx, fitLocalAction()["D"], newFitGraphWalk(), 0) },
	} {
		if err := check(); !errors.Is(err, context.Canceled) {
			t.Fatalf("graph traversal ignored cancellation: %v", err)
		}
	}
}
