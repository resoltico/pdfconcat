// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestMultilineRegenerationWrapsLongWordsAndExplicitLineBreaks(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["V"] = types.StringLiteral("FIRST PARAGRAPH WITH   MANY WORDS\r\nSECOND\rTHIRD VERYLONGWORDTHATREQUIRESWRAPPING")
	fixture.widget["Rect"] = types.NewNumberArray(0, 0, 95, 120)

	plan := capturedVariablePlan(t, fixture)
	if len(plan.lines) <= 3 {
		t.Fatalf("long content did not wrap: %+v", plan.lines)
	}

	var words strings.Builder
	for _, line := range plan.lines {
		if _, err := words.Write(line.data); err != nil {
			t.Fatal(err)
		}

		if line.width*plan.fontSize > 95 && len(line.data) > 1 {
			t.Fatalf("wrapped line exceeds field: %+v", line)
		}
	}

	if !strings.Contains(words.String(), "SECOND") || !strings.Contains(words.String(), "THIRD") ||
		!strings.Contains(words.String(), "VERYLONGWORDTHATREQUIRESWRAPPING") {
		t.Fatalf("wrap lost content: %s", words.String())
	}
}

func TestMultilineAutosizeFitsHeightAndCombAutosizePreservesGlyphs(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["DA"] = types.StringLiteral(variableAutoDA)
	fixture.parent["V"] = types.StringLiteral("ONE\nTWO\nTHREE\nFOUR\nFIVE")

	plan := capturedVariablePlan(t, fixture)
	if plan.fontSize >= 20 || plan.fontSize <= 1 || plan.height-3-plan.fontSize*float64(len(plan.lines)) < .33*plan.fontSize {
		t.Fatalf("multiline autosize did not fit: %+v", plan)
	}

	fixture.parent["Ff"] = types.Integer(variableCombFlag)
	fixture.parent["MaxLen"] = types.Integer(8)
	fixture.parent["V"] = types.StringLiteral("ABC")

	plan = capturedVariablePlan(t, fixture)
	if len(plan.glyphs) != 3 || plan.fontSize != 35 {
		t.Fatalf("comb autosize/glyphs changed: %+v", plan)
	}
}

func TestEmptyAndNarrowMultilineFieldsTerminateWithoutLosingGlyphs(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["V"] = types.StringLiteral("")

	plan := capturedVariablePlan(t, fixture)
	if len(plan.lines) != 1 || len(plan.lines[0].data) != 0 {
		t.Fatal("empty multiline field gained text")
	}

	fixture.parent["V"] = types.StringLiteral("MMMM")
	fixture.widget["Rect"] = types.NewNumberArray(0, 0, 1, 40)

	plan = capturedVariablePlan(t, fixture)
	if len(plan.lines) != 4 {
		t.Fatalf("single-glyph overflow failed to advance: %+v", plan.lines)
	}
}

func TestMultilineMinimumAutosizeTerminates(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["DA"] = types.StringLiteral(variableAutoDA)
	fixture.parent["V"] = types.StringLiteral("ONE\nTWO\nTHREE")
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 40, 1)

	plan := capturedVariablePlan(t, fixture)
	if plan.fontSize != 1 {
		t.Fatalf("minimum font size did not terminate: %v", plan.fontSize)
	}
}

func TestMultilineWrappingSkipsOverflowingSpaceRun(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["V"] = types.StringLiteral("W   W")
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 20, 40)

	plan := capturedVariablePlan(t, fixture)
	if len(plan.lines) != 2 || string(plan.lines[0].data) != "W" || string(plan.lines[1].data) != "W" {
		t.Fatalf("overflowing space run produced invalid lines: %+v", plan.lines)
	}
}
