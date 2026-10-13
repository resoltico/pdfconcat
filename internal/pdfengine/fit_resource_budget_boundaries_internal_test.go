// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitResourceMemoizationRetainsPhysicalSheetErrorBudget(t *testing.T) {
	t.Parallel()
	i := newFitProgramInspector(guardContext(t))
	i.fit = PageFit{Original: PageSize{Width: 100, Height: 50}, Scale: .5}
	state := &fitGraphicsState{matrix: fitIdentityMatrix(), sourceMatrix: fitIdentityMatrix()}
	key := i.resultKey(fitInvocationKey{}, state)
	// A 50-by-25 point image permits 50 and 25 nanopoints of relative error.
	for axis, want := range []float64{5e-8, 2.5e-8} {
		if math.Abs(key.fitTolerance[axis]-want) > 1e-20 {
			t.Fatalf("axis %d memoized error budget=%g, want %g", axis, key.fitTolerance[axis], want)
		}
	}
	i.fit.Scale = 1
	if i.resultKey(fitInvocationKey{}, state) == key {
		t.Fatal("different physical sheet error budgets share a resource result")
	}
}

func TestFitResourceEdgeLabelsRetainExactBoundaryAndBoundLongLabels(t *testing.T) {
	t.Parallel()
	label := strings.Repeat("x", fitEdgeLabelByteLimit)
	if fitShortEdge(label) != label {
		t.Fatal("in-budget resource label acquired a truncation marker")
	}
	if fitShortEdge(label+"y") != label+"..." {
		t.Fatal("over-budget resource label did not retain the bounded prefix and marker")
	}
}

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

			if err := guardInspect(t, inspector, emptyAppearanceDrawing, nil); !errors.Is(err, errFitUnsupported) {
				t.Fatalf("exhausted aggregate source budget lost its policy refusal: %v", err)
			}

			if test.name != "execution-work" && len(inspector.parsed) != 0 {
				t.Fatal("exhausted source parser budget retained another program")
			}
		})
	}
}

func TestFitParsedSourceProgramsChargeWorkOnceAndRejectTheNextOperation(t *testing.T) {
	t.Parallel()
	i := newFitProgramInspector(guardContext(t))
	if _, err := i.parse(t.Context(), "move", []byte("1 2 m"), nil); err != nil {
		t.Fatal(err)
	}
	// The maintained parser counts the operator keyword as an object as well as its operands.
	if i.operations != 1 || i.tokens != 3 || i.objects != 3 {
		t.Fatalf("one move work: operations=%d tokens=%d objects=%d", i.operations, i.tokens, i.objects)
	}
	if _, err := i.parse(t.Context(), "line", []byte("3 4 l"), nil); err != nil || i.operations != 2 || i.tokens != 6 || i.objects != 6 {
		t.Fatalf("second source program did not add its work: %v", err)
	}
	if _, err := i.parse(t.Context(), "move", []byte("1 2 m"), nil); err != nil || i.operations != 2 || i.tokens != 6 || i.objects != 6 {
		t.Fatalf("cached source parse charged work again: %v", err)
	}
	i.operations = fitSourceOperationLimit - 1
	if _, err := i.parse(t.Context(), "last", []byte("n"), nil); err != nil || i.operations != fitSourceOperationLimit {
		t.Fatalf("last permitted operation failed or did not charge work: %v", err)
	}
	if _, err := i.parse(t.Context(), "excess", []byte("n"), nil); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("single additional operation lost its budget refusal: %v", err)
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
	if inspector.bytes != int64(len(first)) {
		t.Fatalf("decoded stream bytes not charged: bytes=%d decoded=%d", inspector.bytes, len(first))
	}

	retained := inspector.bytes
	independent := types.StreamDict{Dict: types.Dict{}, Content: []byte("n")}
	if _, err := inspector.programBytes(t.Context(), "independent-stream", &independent); err != nil || inspector.bytes != retained+1 {
		t.Fatalf("independent stream did not add its decoded byte: %v", err)
	}
	retained++

	second, err := inspector.programBytes(t.Context(), "shared-stream", &stream)
	if err != nil || !bytes.Equal(first, second) || inspector.bytes != retained {
		t.Fatalf("same source stream replay changed accounting: %q %d %v", second, inspector.bytes, err)
	}
}

func TestFitSourcePagesChargeDecodedBytesAndRejectRemainingBudgetOverflow(t *testing.T) {
	t.Parallel()
	i := newFitProgramInspector(guardContext(t))
	if err := guardInspect(t, i, "q Q", nil); err != nil || i.bytes != 3 || i.edges != 1 {
		t.Fatalf("source page byte charge: bytes=%d error=%v", i.bytes, err)
	}
	if err := guardInspect(t, i, "n", nil); err != nil || i.bytes != 4 || i.edges != 2 {
		t.Fatalf("independent source page did not add bytes or an invocation: %v", err)
	}
	if err := guardInspect(t, i, "q Q", nil); err != nil || i.bytes != 4 || i.edges != 3 {
		t.Fatalf("cached source page charged bytes again: bytes=%d error=%v", i.bytes, err)
	}
	for _, page := range []bool{false, true} {
		i := newFitProgramInspector(guardContext(t))
		i.bytes = fitSourceProgramByteLimit - 2
		var err error
		if page {
			err = guardInspect(t, i, "q Q", nil)
		} else {
			stream := types.StreamDict{Dict: types.Dict{}, Content: []byte("q Q")}
			_, err = i.programBytes(t.Context(), "excess", &stream)
		}
		if !errors.Is(err, errFitUnsupported) {
			t.Fatalf("source with only two bytes remaining accepted a three-byte program (page=%t): %v", page, err)
		}
	}
}

func TestFitParserRemainingSourceBudgetsAndExecutionWorkRefuseActualExcess(t *testing.T) {
	t.Parallel()
	for _, budget := range []string{"tokens", "objects", "inline-work"} {
		t.Run(budget, func(t *testing.T) {
			t.Parallel()
			i := newFitProgramInspector(guardContext(t))
			content := []byte("1 2 m")
			switch budget {
			case "tokens":
				i.tokens = fitSourceTokenLimit - 2
			case "objects":
				i.objects = fitSourceObjectLimit - 2
			case "inline-work":
				i.inlineWork = fitSourceInlineWorkByteLimit - 1
				content = []byte("BI /W 2 /H 1 /BPC 8 /CS /G ID xy EI")
			}
			if _, err := i.parse(t.Context(), "excess", content, nil); err == nil || len(i.parsed) != 0 {
				t.Fatalf("program exceeding remaining %s budget accepted or cached: %v", budget, err)
			}
		})
	}
	i := newFitProgramInspector(guardContext(t))
	i.inlineWork = fitSourceInlineWorkByteLimit - 1
	if _, err := i.parse(t.Context(), "pixel", []byte("BI /W 1 /H 1 /BPC 8 /CS /G ID x EI"), nil); err != nil || i.inlineWork != fitSourceInlineWorkByteLimit {
		t.Fatalf("last one-byte inline image did not charge work: %v", err)
	}
	i = newFitProgramInspector(guardContext(t))
	i.work = fitResourceWorkLimit - 1
	if err := guardInspect(t, i, "n", nil); err != nil || i.work != fitResourceWorkLimit {
		t.Fatalf("last executed work item refused or uncharged: %v", err)
	}
	if err := guardInspect(t, i, "n\n", nil); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("one additional executed operation lost its work refusal: %v", err)
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
