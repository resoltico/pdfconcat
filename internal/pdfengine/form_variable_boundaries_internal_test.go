// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func failedVariableObject(fixture *formFixture) types.IndirectRef {
	stream := &types.ObjectStreamDict{}
	stream.Content = []byte("x")
	lazy := types.NewLazyObjectStreamObject(stream, 0, 1, func(context.Context, string) (types.Object, error) { return nil, errBoom })
	fixture.pdf.Table[90] = model.NewXRefTableEntryGen0(lazy)

	return *types.NewIndirectRef(90, 0)
}

func TestChoiceCompilationRetainsReferencedObjectDecodeFailures(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"V", "I", "TI", "Opt", "option", "export", "display"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			fixture := choiceTestFixture()
			ref := failedVariableObject(fixture)

			switch target {
			case "option":
				fixture.parent["Opt"] = types.Array{ref}
			case "export":
				fixture.parent["Opt"] = types.Array{types.Array{ref, types.StringLiteral("LABEL")}}
			case "display":
				fixture.parent["Opt"] = types.Array{types.Array{types.StringLiteral("VALUE"), ref}}
			default:
				fixture.parent[target] = ref
			}

			if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errBoom) {
				t.Fatalf("decode failure %s lost: %v", target, err)
			}
		})
	}
}

func TestAlternateAppearanceStateDictionariesRejectMissingOrUndecodableStreams(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()

	ref := failedVariableObject(fixture)
	for _, object := range []types.Object{ref, types.Dict{"On": ref}, types.Dict{"On": types.Integer(1)}} {
		fixture.widget["AP"] = types.Dict{"R": object}
		if err := checkFormState(t.Context(), fixture.pdf); err == nil {
			t.Fatal("malformed alternate state accepted")
		}
	}

	fixture.widget["AP"] = types.Dict{"R": types.Dict{"On": types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}}}
	if err := checkFormState(t.Context(), fixture.pdf); err != nil {
		t.Fatalf("real alternate state stream refused: %v", err)
	}
}

func TestVariableNumericAppearanceAndBorderDefaultsRemainFaithful(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["DA"] = types.StringLiteral("/F1 13.25 Tf 0.2 g")
	fixture.widget["Border"] = types.Array{types.Integer(0), types.Integer(0), types.Float(1.25), types.NewNumberArray(3, 2)}

	plan := capturedVariablePlan(t, fixture)
	if plan.fontSize != 13.25 || plan.border != 1.25 || plan.borderStyle != "D" || len(plan.dash) != 2 {
		t.Fatalf("source numeric defaults changed: %+v", plan)
	}

	fixture.widget["BS"] = nil

	plan = capturedVariablePlan(t, fixture)
	if plan.border != 0 {
		t.Fatal("null BS did not override Border as absent")
	}

	fixture.widget["BS"] = types.Dict{"S": types.Name("D")}

	plan = capturedVariablePlan(t, fixture)
	if plan.border != 1 || len(plan.dash) != 1 || plan.dash[0] != 3 {
		t.Fatal("default solid width/dash changed")
	}
}

func TestChoiceOptionAndIndexReadersHonorCancellation(t *testing.T) {
	t.Parallel()

	const budget = 2048

	completed := false

	for count := range budget {
		fixture := choiceTestFixture()
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(int64(count))

		_, err := analyzeForm(ctx, fixture.pdf)
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("choice checkpoint%d: %v", count, err)
		}
	}

	if !completed {
		t.Fatal("choice cancellation controls never reached completion")
	}
}

func TestVariableValueArrayDecodeErrorsAndTextAdvanceOverflowRemainVisible(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	ref := failedVariableObject(fixture)

	fixture.parent["V"] = types.Array{ref}
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errBoom) {
		t.Fatalf("array value decode failure lost: %v", err)
	}

	fixture.parent["V"] = types.StringLiteral("WIDE TEXT")

	fixture.parent["DA"] = types.StringLiteral("/F1 1" + strings.Repeat("0", 308) + ".0 Tf")
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
		plan := capturedVariablePlan(t, fixture)
		t.Fatalf("overflowing glyph advance accepted: %v fontsize%g width%g", err, plan.fontSize, plan.lines[0].width)
	}
}

func TestMultilineAndCombCompilationCancellationRetainsClassification(t *testing.T) {
	t.Parallel()

	for _, flags := range []int{variableCombFlag, variableMultilineFlag} {
		completed := false

		for count := range 2048 {
			fixture := variableTestFixture()
			fixture.parent["Ff"] = types.Integer(flags)
			fixture.parent[keyMaxLen] = types.Integer(8)
			fixture.parent["V"] = types.StringLiteral("AAA   BBB\nCCC")
			fixture.parent["DA"] = types.StringLiteral(variableAutoDA)
			ctx := newFormCheckpointContext(t.Context(), t)
			ctx.remaining.Store(int64(count))

			_, err := analyzeForm(ctx, fixture.pdf)
			if err == nil {
				completed = true
				break
			}

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("flag%d checkpoint%d: %v", flags, count, err)
			}
		}

		if !completed {
			t.Fatal("multiline/comb cancellation never completed")
		}
	}
}

func TestVariableNormalizationCancellationDoesNotInstallPartialNormalState(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()

	state, err := analyzeForm(t.Context(), fixture.pdf)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err = state.normalize(ctx, fixture.pdf, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("normalization cancellation lost: %v", err)
	}

	appearance, err := fixture.pdf.DereferenceDict(fixture.widget["AP"])
	if err != nil {
		t.Fatal(err)
	}

	if _, found := appearance.Find("N"); found {
		t.Fatal("canceled normal generation installed partial state")
	}

	if _, found := appearance.Find("D"); !found {
		t.Fatal("cancellation lost alternate appearance")
	}
}

func TestVariableCompilerRequiresAnExplicitFontBearingSourceDefault(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	if _, err := compileVariableAppearance(t.Context(), fixture.pdf, fixture.widget, formDefaults{}, nil); !errors.Is(err, errFormState) {
		t.Fatalf("fontless standalone source plan accepted: %v", err)
	}
}

func TestDefaultAppearanceRejectsNonfiniteDecimalToken(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()

	fixture.parent["DA"] = types.StringLiteral("/F1 1" + strings.Repeat("0", 400) + ".0 Tf")
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
		t.Fatalf("nonfinite decimal token accepted: %v", err)
	}
}
