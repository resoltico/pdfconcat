// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func invocationBoundaryProgram(states int) string {
	var program strings.Builder
	for n := range states {
		fmt.Fprintf(&program, "q 1 0 0 1 %d 0 cm /Leaf Do Q\n", n)
	}

	return program.String()
}

func TestFitInvocationActualDispatcherExactStateCapacityAndCompletedReplay(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	leaf := guardStream(t, pdf, "", nil)
	scope := types.Dict{keyXObject: types.Dict{guardLeafName: leaf}}
	inspector := newFitProgramInspector(pdf)

	program := invocationBoundaryProgram(fitProgramStateLimit - 1)
	if err := guardInspect(t, inspector, program, scope); err != nil {
		t.Fatalf("exact real invocation state capacity: %v", err)
	}

	if inspector.contextCount != fitProgramStateLimit || len(inspector.complete) != fitProgramStateLimit {
		t.Fatalf("actual state authority count: contexts=%d complete=%d", inspector.contextCount, len(inspector.complete))
	}

	beforeContext, beforeOperations, beforeWork := inspector.contextCount, inspector.operations, inspector.work
	if err := guardInspect(t, inspector, program, scope); err != nil {
		t.Fatalf("completed root replay at state cap failed: %v", err)
	}

	if inspector.contextCount != beforeContext || inspector.operations != beforeOperations || inspector.work != beforeWork {
		t.Fatal("completed real dispatch replay initialized or executed another context")
	}
}

func TestFitInvocationActualDispatcherFirstRejectedStateIssuesNoIdentity(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	leaf := guardStream(t, pdf, "", nil)
	scope := types.Dict{keyXObject: types.Dict{guardLeafName: leaf}}

	inspector := newFitProgramInspector(pdf)
	if err := guardInspect(t, inspector, invocationBoundaryProgram(fitProgramStateLimit), scope); err == nil {
		t.Fatal("real dispatcher admitted state capacity+1")
	}

	if inspector.contextCount != fitProgramStateLimit {
		t.Fatalf("rejected state issued an identity: %d", inspector.contextCount)
	}

	if len(inspector.active) != 0 || len(inspector.chain) != 0 {
		t.Fatal("first-error unwind retained active invocations")
	}
}

func TestFitInvocationIDsStaySourceGlobalAcrossPagePrograms(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	inspector := newFitProgramInspector(pdf)
	if err := guardInspect(t, inspector, emptyAppearanceDrawing, nil); err != nil {
		t.Fatal(err)
	}

	if inspector.contextCount != 1 {
		t.Fatal("initial page did not initialize its entry")
	}

	if err := guardInspect(t, inspector, emptyAppearanceDrawing, nil); err != nil {
		t.Fatal(err)
	}

	if inspector.contextCount != 1 {
		t.Fatal("equivalent completed page initialized another entry")
	}

	if err := guardInspect(t, inspector, "q Q q Q", nil); err != nil {
		t.Fatal(err)
	}

	if inspector.contextCount != 2 {
		t.Fatal("distinct later page recycled entry identity")
	}

	scope := types.Dict{}
	if err := guardInspect(t, inspector, emptyAppearanceDrawing, scope); err != nil {
		t.Fatal(err)
	}

	if inspector.contextCount != 3 {
		t.Fatal("different page resource fallback reused prior entry identity")
	}
}

func TestFitDistinctEntryFontsSurviveSavedFillStrokeAndSharedCompletedPaint(t *testing.T) {
	t.Parallel()

	for _, paint := range []string{"f", "S"} {
		t.Run(paint, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			scope := types.Dict{}
			entryScope := types.Dict{}
			paintScope := types.Dict{}
			safeGlyph := guardStream(t, pdf, guardZeroGlyphMetrics, nil)
			unsafeGlyph := guardStream(t, pdf, "0 0 0 0 10 10 d1 1 0 0 rg", nil)
			safe := guardType3(t, pdf, types.Dict{"A": safeGlyph})
			unsafeFont := guardType3(t, pdf, types.Dict{"A": unsafeGlyph})
			scope[keyFont] = types.Dict{guardSafeFontName: safe, "Unsafe": unsafeFont, "Neutral": safe}
			pattern := guardPattern(t, pdf, guardShowA, types.Dict{})
			scope[fitPattern] = types.Dict{"P": pattern}
			painted := guardStream(t, pdf, "1 0 0 1 13 17 cm 0 0 10 10 re "+paint, paintScope)
			entry := guardStream(
				t,
				pdf,
				"/Pattern cs /P scn /Pattern CS /P SCN /Neutral 12 Tf q /Unsafe 12 Tf "+
					"/DeviceRGB cs 0 0 0 rg /DeviceRGB CS 0 0 0 RG Q /Paint Do",
				entryScope,
			)
			scope[keyXObject] = types.Dict{"Paint": painted, "Entry": entry}
			entryScope[keyFont] = scope[keyFont]
			entryScope[fitPattern] = scope[fitPattern]
			entryScope[keyXObject] = scope[keyXObject]
			inspector := newFitProgramInspector(pdf)

			err := guardInspect(t, inspector, "/Safe 12 Tf /Entry Do /Unsafe 12 Tf /Entry Do", scope)
			if err == nil || !strings.Contains(err.Error(), "color operation") {
				t.Fatalf("shared completed paint reused wrong immutable entry font: %v", err)
			}

			for key := range inspector.complete {
				if key.invocation.program == pattern.PDFString() &&
					(key.sourceMatrix != (Affine{1, 0, 0, 1, 2, 3}) || key.matrix != (Affine{1, 0, 0, 1, 2, 3})) {
					t.Fatalf("saved inherited %s selection used child paint-time anchor: %+v", paint, key)
				}
			}
		})
	}
}

func TestFitRealSourceTraversalStopsAtFirstFailedPageAfterCompletedSibling(t *testing.T) {
	t.Parallel()

	doc := rawDoc(catalog,
		"<< /Type /Pages /Count 2 /Kids [3 0 R 4 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Good 7 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 6 0 R >>",
		groupPresenceStream("", "/Good Do /FirstMissing Do"),
		groupPresenceStream("", "/SecondMissing Do"),
		groupPresenceStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources <<>>", ""))

	path := filepath.Join(t.TempDir(), "first-error.pdf")
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	target := PageSize{612, 1008}
	for range 2 {
		_, inspectErr := engine.Inspect(t.Context(), path, &target)
		if CodeOf(inspectErr) != CodeFitUnsupported || !strings.Contains(inspectErr.Error(), guardFirstSourcePage) ||
			!strings.Contains(inspectErr.Error(), "FirstMissing") ||
			strings.Contains(inspectErr.Error(), "SecondMissing") {
			t.Fatalf("source traversal continued/reused failed inspector: %v", inspectErr)
		}
	}
}
