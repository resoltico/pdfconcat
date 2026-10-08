// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const errorNamesOverflow = "overflows"

// pageTreeChain is a document whose single page sits below the given number of nested /Pages nodes.
func pageTreeChain(nodes int) *pdffixture.Doc {
	objects := make([]string, 1, nodes+2)
	objects[0] = catalog

	for level := range nodes {
		objects = append(objects, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", level+3))
	}

	return rawDoc(append(objects, page)...)
}

func TestWalkPagesAcceptsTheDeepestAllowedTreeOnly(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		nodes   int
		wantErr bool
	}{
		"page at the deepest allowed level":    {maxPageTreeDepth, false},
		"page one level below the deepest":     {maxPageTreeDepth + 1, true},
		"page one level above the deepest one": {maxPageTreeDepth - 1, false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pdf := readUnvalidated(t, pageTreeChain(test.nodes))

			root, err := pdf.Pages()
			if err != nil {
				t.Fatal(err)
			}

			visited := 0

			err = walkPages(context.Background(), pdf, *root, func(types.IndirectRef, types.Dict, inheritedAttrs) error {
				visited++

				return nil
			})

			if test.wantErr != (err != nil) {
				t.Fatalf("walkPages() = %v, want an error: %v", err, test.wantErr)
			}

			if !test.wantErr && visited != 1 {
				t.Fatalf("visited %d pages, want 1", visited)
			}
		})
	}
}

func TestRotationIsNormalizedToOneTurn(t *testing.T) {
	t.Parallel()

	pdf := readUnvalidated(t, pdffixture.Plain("P"))

	tests := map[int]int{0: 0, 90: 90, 180: 180, 270: 270, 360: 0, 450: 90, 720: 0, -90: 270, -180: 180, -270: 90, -360: 0, -450: 270}
	for given, want := range tests {
		got, err := rotationOf(pdf, types.Dict{}, types.Integer(given))
		if err != nil || got != want {
			t.Errorf("rotation %d = %d, %v; want %d", given, got, err, want)
		}
	}
}

func TestVisibleSizeCropMustOverlapMediaInBothDimensions(t *testing.T) {
	t.Parallel()

	pdf := readUnvalidated(t, pdffixture.Plain("P"))
	box := func(x0, y0, x1, y1 int) types.Array {
		return types.Array{types.Integer(x0), types.Integer(y0), types.Integer(x1), types.Integer(y1)}
	}

	rejected := map[string]types.Array{
		"touching on the right":      box(10, 0, 20, 10),
		"touching at the top":        box(0, 10, 10, 20),
		"touching on the left":       box(-10, 0, 0, 10),
		"touching at the bottom":     box(0, -10, 10, 0),
		"beyond the right edge only": box(20, 0, 30, 10),
		"beyond the top edge only":   box(0, 20, 10, 30),
	}
	for name, crop := range rejected {
		page := types.Dict{keyMediaBox: box(0, 0, 10, 10), keyCropBox: crop}

		_, err := visibleSize(pdf, page, inheritedAttrs{})
		if err == nil || !strings.Contains(err.Error(), "does not overlap") {
			t.Errorf("%s: visibleSize() = %v", name, err)
		}
	}

	page := types.Dict{keyMediaBox: box(0, 0, 10, 10), keyCropBox: box(9, 9, 20, 20)}

	got, err := visibleSize(pdf, page, inheritedAttrs{})
	if err != nil || got != (PageSize{Width: 1, Height: 1}) {
		t.Errorf("a one-point overlap: %v, %v", got, err)
	}
}

func TestVisibleSizeReportsOverflowOfEitherDimension(t *testing.T) {
	t.Parallel()

	pdf := readUnvalidated(t, pdffixture.Plain("P"))
	huge := types.Float(math.MaxFloat64)
	box := func(width, height types.Object) types.Array {
		return types.Array{types.Integer(0), types.Integer(0), width, height}
	}

	tests := map[string]types.Dict{
		"width only":  {keyMediaBox: box(huge, types.Integer(5)), userUnitKey: types.Float(4)},
		"height only": {keyMediaBox: box(types.Integer(5), huge), userUnitKey: types.Float(4)},
		"rotated":     {keyMediaBox: box(huge, types.Integer(5)), userUnitKey: types.Float(4), "Rotate": types.Integer(90)},
	}
	for name, page := range tests {
		_, err := visibleSize(pdf, page, inheritedAttrs{})
		if err == nil || !strings.Contains(err.Error(), errorNamesOverflow) {
			t.Errorf("%s: visibleSize() = %v", name, err)
		}
	}
}

func TestPlaceChecksCancellationOnEachIntervalBoundary(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		placed       int
		count        int
		wantCanceled bool
	}{
		"one page":                             {0, 1, false},
		"one short of the interval":            {0, cancelCheckInterval - 1, false},
		"exactly the interval":                 {0, cancelCheckInterval, true},
		"past the interval":                    {0, cancelCheckInterval + 1, true},
		"two intervals":                        {0, 2 * cancelCheckInterval, true},
		"offset one short of the interval":     {1, cancelCheckInterval - 2, false},
		"offset reaching the interval":         {1, cancelCheckInterval - 1, true},
		"already past the interval, one short": {cancelCheckInterval, cancelCheckInterval - 1, false},
		"already past the interval, reaching":  {cancelCheckInterval, cancelCheckInterval, true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			built := pool{pdf: readUnvalidated(t, pdffixture.Plain("P"))}
			pages := []*poolPage{{dict: types.Dict{keyType: types.Name("Page")}}}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			final, err := built.place(ctx, "out.pdf", make([]*poolPage, test.placed), pages, test.count)

			if got := CodeOf(err) == CodeCanceled; got != test.wantCanceled {
				t.Fatalf("place() canceled = %v (%v), want %v", got, err, test.wantCanceled)
			}

			if !test.wantCanceled && len(final) != test.placed+test.count {
				t.Fatalf("placed %d pages, want %d", len(final), test.placed+test.count)
			}
		})
	}
}

func TestUseClonesPageDictionariesWithoutSharingValues(t *testing.T) {
	t.Parallel()

	built := pool{pdf: readUnvalidated(t, pdffixture.Plain("P"))}
	original := &poolPage{dict: types.Dict{"Nested": types.Array{types.Integer(1)}, "Empty": nil}}

	if first := built.use(original); first != original {
		t.Fatal("the first use must return the page itself")
	}

	clone := built.use(original)
	if clone == original || clone.ref == original.ref {
		t.Fatal("a repeated use must return a distinct page")
	}

	value, kept := clone.dict["Empty"]
	if !kept || value != nil {
		t.Fatalf("a null entry was not carried over: %v, %v", value, kept)
	}

	nested, isArray := clone.dict["Nested"].(types.Array)
	if !isArray {
		t.Fatalf("clone entry is %T", clone.dict["Nested"])
	}

	nested[0] = types.Integer(2)

	untouched, isArray := original.dict["Nested"].(types.Array)
	if !isArray || untouched[0] != types.Integer(1) {
		t.Fatal("the clone shares its nested values with the original")
	}
}
