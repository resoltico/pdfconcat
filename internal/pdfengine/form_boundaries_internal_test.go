// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"maps"
	"math"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestFormPreparationCancellationPreservesUnnormalizedDefaults(t *testing.T) {
	t.Parallel()

	const checkpoints = 64

	completed := false

	for budget := range checkpoints {
		fixture := formTestContext()
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(int64(budget))

		_, err := prepareFormResources(ctx, fixture.pdf, 1)
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("checkpoint %d: %v", budget, err)
		}

		if _, changed := fixture.widget.Find("DA"); changed {
			t.Fatalf("canceled preparation changed widget defaults at checkpoint %d", budget)
		}
	}

	if !completed {
		t.Fatal("bounded cancellation controls never reached successful preparation")
	}
}

func TestFormMergeValidatesImportedAndDestinationResourceGraphs(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*formFixture, *formState){
		"destination form":     func(f *formFixture, _ *formState) { f.pdf.RootDict[keyAcroForm] = types.Integer(1) },
		"destination DR":       func(f *formFixture, _ *formState) { f.form["DR"] = types.Integer(1) },
		"imported DR":          func(_ *formFixture, s *formState) { s.root["DR"] = types.Integer(1) },
		"imported category":    func(_ *formFixture, s *formState) { s.root["DR"] = types.Dict{keyFont: types.Integer(1)} },
		"destination category": func(f *formFixture, _ *formState) { f.form["DR"] = types.Dict{keyFont: types.Integer(1)} },
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := formTestContext()
			source := &formState{
				root: types.Dict{"DR": types.Dict{keyFont: types.Dict{"Imported": types.Dict{keySubtype: types.Name(type1FontSubtype)}}}},
			}
			damage(fixture, source)

			if err := mergeFormResources(t.Context(), fixture.pdf, source); err == nil {
				t.Fatalf("invalid %s graph was accepted", name)
			}
		})
	}

	fixture := formTestContext()
	fixture.form.Delete("DR")

	source := &formState{
		root: types.Dict{
			"DR": types.Dict{
				keyFont:    types.Dict{"Imported": types.Dict{keySubtype: types.Name(type1FontSubtype)}},
				keyProcSet: types.Array{types.Name(procedurePDF)},
			},
		},
	}
	if err := mergeFormResources(t.Context(), fixture.pdf, source); err != nil {
		t.Fatal(err)
	}

	resources, err := fixture.pdf.DereferenceDict(fixture.form["DR"])
	if err != nil || resources[keyFont] == nil {
		t.Fatalf("new destination resources missing: %v %v", resources, err)
	}

	fixture.pdf.RootDict[keyAcroForm] = types.Dict{}
	if emptyErr := checkFormState(t.Context(), fixture.pdf); emptyErr != nil {
		t.Fatalf("empty optional form rejected: %v", emptyErr)
	}
}

func TestFormDereferenceFailuresPreserveTheirCause(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	stream := &types.ObjectStreamDict{}
	stream.Content = []byte("x")
	lazy := types.NewLazyObjectStreamObject(stream, 0, 1, func(context.Context, string) (types.Object, error) { return nil, errBoom })
	fixture.pdf.Table[3] = model.NewXRefTableEntryGen0(lazy)

	ref := *types.NewIndirectRef(3, 0)
	if err := checkFormResource(t.Context(), fixture.pdf, keyFont, ref); !errors.Is(err, errBoom) {
		t.Fatalf("compressed resource failure lost: %v", err)
	}

	fixture.parent["Q"] = ref
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errBoom) {
		t.Fatalf("compressed field default failure lost: %v", err)
	}

	fixture.parent.Delete("Q")

	fixture.form["DR"] = types.Dict{
		keyFont:    types.Dict{"F1": types.Dict{keySubtype: types.Name(type1FontSubtype)}},
		keyProcSet: types.Array{types.Name(procedurePDF), types.Name(procedureText)},
	}
	if _, err := prepareFormResources(t.Context(), fixture.pdf, 1); err != nil {
		t.Fatalf("valid ProcSet rejected: %v", err)
	}
}

func TestPoolFormPreparationCancellationRetainsSourceIdentity(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "form-source.pdf")
	if err = pdffixture.Form("FORM").WriteFile(path); err != nil {
		t.Fatal(err)
	}

	const probeBudget = 1 << 20

	probe := newFormCheckpointContext(t.Context(), t)
	probe.remaining.Store(probeBudget)

	initial := &pool{engine: engine}
	if _, err = initial.importDocument(probe, 0, path, 2, nil); err != nil {
		t.Fatal(err)
	}

	checks := int64(probeBudget) - probe.remaining.Load()
	// Exercise the final read/normalization/collection checkpoints, rather than a timing race.
	for budget := max(int64(0), checks-64); budget < checks; budget++ {
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		current := &pool{engine: engine}

		_, failure := current.importDocument(ctx, 0, path, 2, nil)
		if failure == nil {
			continue
		}

		if !errors.Is(failure, context.Canceled) || CodeOf(failure) != CodeCanceled {
			t.Fatalf("checkpoint %d changed cancellation classification: %v", budget, failure)
		}
	}
}

func TestFormRegenerationFlagsAndWidgetSubtypeDecodeFailures(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	stream := &types.ObjectStreamDict{}
	stream.Content = []byte("x")
	lazy := types.NewLazyObjectStreamObject(stream, 0, 1, func(context.Context, string) (types.Object, error) { return nil, errBoom })

	fixture.pdf.Table[3] = model.NewXRefTableEntryGen0(lazy)
	ref := *types.NewIndirectRef(3, 0)

	fixture.form["NeedAppearances"] = ref
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errBoom) {
		t.Fatalf("regeneration flag decode failure lost: %v", err)
	}

	fixture.form["NeedAppearances"] = *types.NewIndirectRef(4, 0)
	if err := checkFormState(t.Context(), fixture.pdf); err != nil {
		t.Fatalf("null flag did not use false default: %v", err)
	}

	fixture.widget[keySubtype] = ref
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errBoom) {
		t.Fatalf("widget subtype decode failure lost: %v", err)
	}
}

func TestSourceButtonRegenerationUsesCapturedPlanAndRetainsLogicalState(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	fixture.form["NeedAppearances"] = types.Boolean(true)
	fixture.parent["FT"] = types.Name(buttonFieldType)

	fixture.parent.Delete("DA")
	fixture.form.Delete("DA")
	maps.Copy(fixture.widget, buttonTestDictionary())

	size, offset, generation := 3, int64(0), 65535
	fixture.pdf.Size = &size
	fixture.pdf.Table[0] = &model.XRefTableEntry{Free: true, Offset: &offset, Generation: &generation}

	state, err := analyzeForm(t.Context(), fixture.pdf)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err = state.normalize(ctx, fixture.pdf, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("captured button cancellation lost: %v", err)
	}

	if err = state.normalize(t.Context(), fixture.pdf, nil); err != nil {
		t.Fatal(err)
	}

	if fixture.widget["V"] != fixture.widget["AS"] {
		t.Fatal("button logical state changed during source normalization")
	}

	if fixture.form["NeedAppearances"] != types.Boolean(false) {
		t.Fatal("button source regeneration leaked into global merged flag")
	}

	fixture.widget["Rect"] = types.NewNumberArray(0, 0, 20, 10)

	fixture.form["NeedAppearances"] = types.Boolean(true)
	if err = checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
		t.Fatalf("unsupported source button shape accepted: %v", err)
	}
}

func TestButtonPreparationCancellationRemainsVisible(t *testing.T) {
	t.Parallel()

	probe := newFormCheckpointContext(t.Context(), t)
	probe.remaining.Store(math.MaxInt64)

	if _, err := prepareFormResources(probe, buttonPreparationContext().pdf, 1); err != nil {
		t.Fatalf("uncancelled button preparation refused: %v", err)
	}

	checkpoints := math.MaxInt64 - probe.remaining.Load() + 1

	completed := false

	for budget := range checkpoints {
		fixture := buttonPreparationContext()
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		_, err := prepareFormResources(ctx, fixture.pdf, 1)
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("button preparation checkpoint %d: %v", budget, err)
		}
	}

	if !completed {
		t.Fatal("bounded button cancellation controls never reached success")
	}
}

func buttonPreparationContext() *formFixture {
	fixture := formTestContext()
	fixture.form["NeedAppearances"] = types.Boolean(true)
	fixture.parent["FT"] = types.Name(buttonFieldType)
	fixture.form.Delete("DA")
	maps.Copy(fixture.widget, buttonTestDictionary())

	size, offset, generation := 3, int64(0), 65535
	fixture.pdf.Size = &size
	fixture.pdf.Table[0] = &model.XRefTableEntry{Free: true, Offset: &offset, Generation: &generation}

	return fixture
}
