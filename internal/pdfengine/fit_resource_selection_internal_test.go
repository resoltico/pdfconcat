// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitResourceLookupRejectsNilAndConsumesWork(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	scope := types.Dict{keyXObject: types.Dict{"Nil": nil, "Present": types.Integer(1)}}
	if _, err := inspector.resource(t.Context(), scope, keyXObject, "Nil"); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("nil resource accepted: %v", err)
	}
	inspector.work = fitResourceWorkLimit - 1
	if _, err := inspector.resource(t.Context(), scope, keyXObject, "Present"); err != nil {
		t.Fatalf("last permitted lookup refused: %v", err)
	}
	if _, err := inspector.resource(t.Context(), scope, keyXObject, "Present"); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("resource work exceeded the declared limit: %v", err)
	}
}

func TestFitColorSpaceSelectionHandlesEmptyAndScalarResources(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		object  types.Object
		pattern bool
	}{
		{types.Array{}, false},
		{types.Name("DeviceRGB"), false},
		{types.Array{types.Name(fitPattern)}, true},
	} {
		inspector := newFitProgramInspector(guardContext(t))
		scope := types.Dict{fitColorSpace: types.Dict{"Selected": fixture.object}}
		got, err := inspector.patternColorSpace(t.Context(), scope, "Selected")
		if err != nil || got != fixture.pattern {
			t.Fatalf("color space %v selected pattern=%t, want %t: %v", fixture.object, got, fixture.pattern, err)
		}
	}
}

func TestFitPaintingConsultsOnlySelectedPatternModes(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	for _, fixture := range []struct {
		state           *fitGraphicsState
		unused, painted fitPaintMode
	}{
		{&fitGraphicsState{fillPattern: true}, fitPaintStroke, fitPaintFill},
		{&fitGraphicsState{strokePattern: true}, fitPaintFill, fitPaintStroke},
	} {
		if err := inspector.paintPatterns(t.Context(), fixture.state, fixture.unused); err != nil {
			t.Fatalf("mode %d consulted an unused pattern: %v", fixture.unused, err)
		}
		if err := inspector.paintPatterns(t.Context(), fixture.state, fixture.painted); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("painted mode %d without pattern selection accepted: %v", fixture.painted, err)
		}
	}
}

func TestFitTextRenderingModeDomainFromActualContent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"0", "1", "2", "3", "4", "5", "6", "7"} {
		inspector := newFitProgramInspector(guardContext(t))
		if err := guardInspect(t, inspector, "BT "+mode+" Tr ET", nil); err != nil {
			t.Fatalf("supported text rendering mode %s refused: %v", mode, err)
		}
	}
	for _, mode := range []string{"-1", "8", "0.5"} {
		inspector := newFitProgramInspector(guardContext(t))
		if err := guardInspect(t, inspector, "BT "+mode+" Tr ET", nil); err == nil {
			t.Fatalf("unsupported text rendering mode %s accepted", mode)
		}
	}
}
