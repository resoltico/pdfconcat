// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitDeepExecutedFormChainHasBoundedWorkWithoutRefusingUnusedChain(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	leaf := guardStream(t, pdf, "", nil)
	for range fitProgramDepthLimit + 1 {
		leaf = guardStream(t, pdf, "/Next Do", types.Dict{keyXObject: types.Dict{"Next": leaf}})
	}

	scope := types.Dict{keyXObject: types.Dict{"Deep": leaf}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "", scope); err != nil {
		t.Fatalf("unused acyclic chain refused: %v", err)
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), "/Deep Do", scope); err == nil {
		t.Fatal("executed form chain exceeded declared recursion budget")
	}
}

func TestFitAggregateResourceBudgetsRejectAnotherSourceProgram(t *testing.T) {
	t.Parallel()

	cases := []struct {
		seed func(*fitProgramInspector)
		name string
	}{
		{name: "tokens", seed: func(i *fitProgramInspector) { i.tokens = fitSourceTokenLimit }},
		{name: "objects", seed: func(i *fitProgramInspector) { i.objects = fitSourceObjectLimit }},
		{name: "inline-work", seed: func(i *fitProgramInspector) { i.inlineWork = fitSourceInlineWorkByteLimit }},
		{name: "source-operations", seed: func(i *fitProgramInspector) { i.operations = fitSourceOperationLimit }},
		{name: "execution-work", seed: func(i *fitProgramInspector) { i.work = fitResourceWorkLimit }},
		{name: "resource-edges", seed: func(i *fitProgramInspector) { i.edges = fitInvocationEdgeLimit }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			inspector := newFitProgramInspector(pdf)
			test.seed(inspector)

			if err := guardInspect(t, inspector, emptyAppearanceDrawing, nil); err == nil {
				t.Fatal("exhausted aggregate source budget accepted another actual program")
			}

			if test.name != "execution-work" && len(inspector.parsed) != 0 {
				t.Fatal("exhausted source parser budget retained another program")
			}
		})
	}
}

func TestFitUsedFunctionGraphChecksLastEdgeAndActiveDepth(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)

	inspector.edges = fitInvocationEdgeLimit - 1
	if err := inspector.boundedResource(t.Context(), types.Integer(1), 0, map[string]bool{}); err != nil {
		t.Fatalf("last permitted scalar edge refused: %v", err)
	}

	if err := inspector.boundedResource(t.Context(), types.Integer(1), 0, map[string]bool{}); err == nil {
		t.Fatal("used function edge budget+1 accepted")
	}

	inspector = newFitProgramInspector(pdf)
	if err := inspector.boundedResource(t.Context(), types.Array{types.Integer(1)}, fitProgramDepthLimit, map[string]bool{}); err == nil {
		t.Fatal("used function exceeded declared active depth")
	}
}

func TestFitGraphicsStackBoundAndArithmeticOverflowAreActualContentRefusals(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	program := strings.Repeat("q ", fitGraphicsStackLimit+1)
	if err := guardInspect(t, newFitProgramInspector(pdf), program, nil); err == nil {
		t.Fatal("graphics-state stack budget+1 accepted")
	}

	huge := "1" + strings.Repeat("0", 308) + ".0"

	program = huge + " 0 0 1 0 0 cm 2 0 0 1 0 0 cm"
	if err := guardInspect(t, newFitProgramInspector(pdf), program, nil); !errors.Is(err, errFitGeometry) {
		t.Fatalf("actual original CTM arithmetic overflow identity: %v", err)
	}
}

func TestFitSourceProgramCacheReplayPreservesDecodedByteAccounting(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	inspector := newFitProgramInspector(pdf)
	stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}

	first, err := inspector.programBytes(t.Context(), "shared-stream", &stream)
	if err != nil {
		t.Fatal(err)
	}

	retained := inspector.bytes

	second, err := inspector.programBytes(t.Context(), "shared-stream", &stream)
	if err != nil || !bytes.Equal(first, second) || inspector.bytes != retained {
		t.Fatalf("same source stream replay changed accounting: %q %d %v", second, inspector.bytes, err)
	}
}

func TestFitLastExecutionWorkItemCannotBypassResourceLookupBudget(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	leaf := guardStream(t, pdf, "", nil)
	scope := types.Dict{keyXObject: types.Dict{guardLeafName: leaf}}
	inspector := newFitProgramInspector(pdf)

	inspector.work = fitResourceWorkLimit - 1
	if err := guardInspect(t, inspector, guardPaintLeaf, scope); err == nil {
		t.Fatal("execution step consumed last work item but resource lookup still proceeded")
	}
}
