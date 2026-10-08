// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	choiceFirstValue = "OptionOne"
	choicePairValue  = "EXPORT"
	choiceLastValue  = "OptionLast"
)

func choiceTestFixture() *formFixture {
	fixture := variableTestFixture()
	fixture.parent["FT"] = types.Name("Ch")
	fixture.parent["Opt"] = types.Array{
		types.StringLiteral(choiceFirstValue),
		types.Array{types.StringLiteral(choicePairValue), types.StringLiteral("Display label")},
		types.StringLiteral(choiceLastValue),
	}
	fixture.parent["V"] = types.StringLiteral(choicePairValue)

	return fixture
}

func capturedVariablePlan(t *testing.T, fixture *formFixture) *variableAppearancePlan {
	t.Helper()

	state, err := analyzeForm(t.Context(), fixture.pdf)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range state.defaults {
		if entry.variable != nil {
			return entry.variable
		}
	}

	t.Fatal("requested alternate-mode regeneration had no captured plan")

	return nil
}

func TestChoiceRegenerationUsesExportValuesDisplayLabelsAndSelection(t *testing.T) {
	t.Parallel()

	fixture := choiceTestFixture()
	fixture.parent["Ff"] = types.Integer(variableComboFlag)

	plan := capturedVariablePlan(t, fixture)
	if len(plan.lines) != 1 || string(plan.lines[0].data) != "Display label" {
		t.Fatalf("combo rendered export instead of label: %+v", plan.lines)
	}

	fixture.parent.Delete("V")

	plan = capturedVariablePlan(t, fixture)
	if len(plan.lines[0].data) != 0 {
		t.Fatal("absent combo value selected an option")
	}

	fixture.parent["V"] = types.StringLiteral("Free edit")
	fixture.parent["Ff"] = types.Integer(variableComboFlag | variableEditFlag)

	plan = capturedVariablePlan(t, fixture)
	if string(plan.lines[0].data) != "Free edit" {
		t.Fatal("editable combo value replaced")
	}
}

func TestChoiceRegenerationPreservesInheritedIndicesAndViewport(t *testing.T) {
	t.Parallel()

	fixture := choiceTestFixture()

	plan := capturedVariablePlan(t, fixture)
	if plan.kind != "list" || len(plan.selected) != 3 || !plan.selected[1] || plan.selected[0] {
		t.Fatalf("export selection incorrect: %+v", plan)
	}

	fixture.parent["I"] = types.Array{types.Integer(0), types.Integer(2)}
	fixture.parent["TI"] = types.Integer(1)
	fixture.parent["Ff"] = types.Integer(variableMultiSelectFlag)
	fixture.parent["V"] = types.Array{types.StringLiteral(choiceFirstValue), types.StringLiteral(choiceLastValue)}

	plan = capturedVariablePlan(t, fixture)
	if !plan.selected[0] || plan.selected[1] || !plan.selected[2] || plan.topIndex != 1 {
		t.Fatalf("inherited indices/viewport changed: %+v", plan)
	}
}

func TestChoiceRegenerationRejectsInvalidOptionsValuesAndIndices(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*formFixture){
		"scalar options":    func(f *formFixture) { f.parent["Opt"] = types.Integer(1) },
		"invalid option":    func(f *formFixture) { f.parent["Opt"] = types.Array{types.Integer(1)} },
		"wrong pair length": func(f *formFixture) { f.parent["Opt"] = types.Array{types.Array{types.StringLiteral("ONLY")}} },
		"invalid pair display": func(f *formFixture) {
			f.parent["Opt"] = types.Array{types.Array{types.StringLiteral(choicePairValue), types.Integer(1)}}
		},
		"invalid combo value": func(f *formFixture) {
			f.parent["Ff"] = types.Integer(variableComboFlag)
			f.parent["V"] = types.StringLiteral("UNDECLARED")
		},
		"multiple combo values": func(f *formFixture) {
			f.parent["Ff"] = types.Integer(variableComboFlag)
			f.parent["V"] = types.Array{types.StringLiteral(choiceFirstValue), types.StringLiteral(choiceLastValue)}
		},
		"invalid indices":  func(f *formFixture) { f.parent["I"] = types.Integer(1) },
		"negative index":   func(f *formFixture) { f.parent["I"] = types.Array{types.Integer(-1)} },
		"large index":      func(f *formFixture) { f.parent["I"] = types.Array{types.Integer(3)} },
		"null index":       func(f *formFixture) { f.parent["I"] = types.Array{nil} },
		"outside viewport": func(f *formFixture) { f.parent["TI"] = types.Integer(4) },
		"invalid glyph":    func(f *formFixture) { f.parent["Opt"] = types.Array{types.StringLiteral("\u4e0d\u5b58\u5728")} },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := choiceTestFixture()
			corrupt(fixture)

			if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
				t.Fatalf("invalid choice accepted: %v", err)
			}
		})
	}
}
