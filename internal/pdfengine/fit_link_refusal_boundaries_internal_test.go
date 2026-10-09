// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const fitInvalidAnnotationValue = "invalid annotation value"

func TestFitCatalogRejectsMalformedTreesAndEditableFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		root types.Dict
		want string
	}{
		{"form scalar", types.Dict{keyAcroForm: types.Integer(1)}, "/AcroForm"},
		{"fields scalar", types.Dict{keyAcroForm: types.Dict{keyFields: types.Integer(1)}}, "/Fields"},
		{"editable fields", types.Dict{keyAcroForm: types.Dict{keyFields: types.Array{types.Dict{}}}}, "editable"},
		{"legacy scalar", types.Dict{keyDests: types.Integer(1)}, "catalog /Dests"},
		{"names scalar", types.Dict{keyNames: types.Integer(1)}, "catalog /Names"},
		{"tree scalar", types.Dict{keyNames: types.Dict{keyDests: types.Integer(1)}}, "tree"},
		{"kids scalar", types.Dict{keyNames: types.Dict{keyDests: types.Dict{keyKids: types.Integer(1)}}}, "/Kids"},
		{"null kid", types.Dict{keyNames: types.Dict{keyDests: types.Dict{keyKids: types.Array{nil}}}}, "/Kids[0]"},
		{"pair scalar", types.Dict{keyNames: types.Dict{keyDests: types.Dict{keyNames: types.Integer(1)}}}, "/Names"},
		{
			"bad hex key",
			types.Dict{keyNames: types.Dict{keyDests: types.Dict{keyNames: types.Array{types.HexLiteral("zz"), types.Integer(1)}}}},
			"key",
		},
		{"missing destination body", types.Dict{keyDests: types.Dict{fitChapterFixtureName: types.Dict{}}}, "no /D"},
		{"scalar destination body", types.Dict{keyDests: types.Dict{fitChapterFixtureName: types.Integer(1)}}, "local"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf, _, _ := fitLinkFixture(t)
			maps.Copy(pdf.RootDict, test.root)

			_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("malformed catalog admitted or lost context: %v, want %q", err, test.want)
			}
		})
	}
}

func TestFitLinkRefusesMalformedAnnotationFieldsAndEffectiveGeometry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value     types.Object
		name, key string
	}{
		{types.Integer(1), "subtype number", keySubtype},
		{types.Integer(1), "border scalar", fitBorderKey},
		{types.Array{types.Integer(0)}, "border length", fitBorderKey},
		{types.Array{types.Name(fitInvalidAnnotationValue), types.Integer(0), types.Integer(0)}, "border coordinate", fitBorderKey},
		{types.Integer(1), "style scalar", "BS"},
		{nil, "rect absent", keyRect},
		{types.Integer(1), "rect scalar", keyRect},
		{types.Integer(1), "quad scalar", keyQuadPoints},
		{
			types.Array{
				types.Name(fitInvalidAnnotationValue),
				types.Integer(0),
				types.Integer(1),
				types.Integer(0),
				types.Integer(1),
				types.Integer(1),
				types.Integer(0),
				types.Integer(1),
			},
			"quad numeric type",
			keyQuadPoints,
		},
		{types.Name(fitInvalidAnnotationValue), "flags type", "F"},
		{types.Float(0.5), "flags fractional", "F"},
		{types.Integer(-1), "flags negative", "F"},
		{types.Integer(1024), "flags unrecognized", "F"},
		{types.Float(math.Inf(1)), "flags nonfinite", "F"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf, _, link := fitLinkFixture(t)
			link[test.key] = test.value

			if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); err == nil {
				t.Fatal("malformed annotation admitted")
			}
		})
	}
}

func TestFitAnnotationRejectsMalformedContainersAndRepeatedOwnership(t *testing.T) {
	t.Parallel()

	for _, object := range []types.Object{types.Integer(1), types.Array{types.Integer(1)}} {
		pdf, page, _ := fitLinkFixture(t)
		page["Annots"] = object

		if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); err == nil {
			t.Fatal("malformed annotation container admitted")
		}
	}

	pdf, page, link := fitLinkFixture(t)
	shared := fitIndirect(t, pdf, link)
	page["Annots"] = types.Array{shared, shared}

	if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("repeated annotation ownership admitted: %v", err)
	}

	pdf, _, link = fitLinkFixture(t)
	link["P"] = *types.NewIndirectRef(999, 0)

	if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); err == nil || !strings.Contains(err.Error(), "source page") {
		t.Fatalf("foreign annotation parent admitted: %v", err)
	}
}

func fitMalformedLazyReference(t *testing.T, pdf *model.Context) types.IndirectRef {
	t.Helper()

	stream := types.NewObjectStreamDict()
	stream.Content = []byte(nativeInvalidNumberBody)
	lazy := types.NewLazyObjectStreamObject(stream, 0, -1, func(ctx context.Context, body string) (types.Object, error) {
		return model.ParseObject(ctx, &body, 0)
	})

	return fitIndirect(t, pdf, lazy)
}

func TestFitLazyReferenceFailuresRetainNativeReaderErrors(t *testing.T) {
	t.Parallel()

	checks := []func(context.Context, *fitInspector, types.IndirectRef) error{
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.destination(ctx, ref, newFitGraphWalk(), 0)
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			_, err := inspector.destinationKey(ctx, ref)
			return err
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.namedDestination(ctx, ref, newFitGraphWalk(), 0)
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.fitDestinationArray(ctx, types.Array{*types.NewIndirectRef(3, 0), ref})
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.linkBorder(ctx, types.Dict{"BS": ref})
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			_, err := inspector.linkStyleWidth(ctx, types.Dict{"W": ref})
			return err
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.linkAction(ctx, types.Dict{"A": ref})
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.linkAction(ctx, types.Dict{"Dest": ref})
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.singleLinkAction(ctx, types.Dict{"Type": ref}, newFitGraphWalk(), 0)
		},
		func(ctx context.Context, inspector *fitInspector, ref types.IndirectRef) error {
			return inspector.singleLinkAction(ctx, types.Dict{"S": ref}, newFitGraphWalk(), 0)
		},
	}
	for _, check := range checks {
		pdf, _, _ := fitLinkFixture(t)

		inspector := &fitInspector{pdf: pdf, pages: map[types.IndirectRef]bool{*types.NewIndirectRef(3, 0): true}}

		err := check(t.Context(), inspector, fitMalformedLazyReference(t, pdf))
		if err == nil || !strings.Contains(err.Error(), nativeInvalidNumberDiagnostic) {
			t.Fatalf("native malformed packed object error lost: %v", err)
		}
	}
}

func TestFitLinkBoundaryCancellationRetainsOperationIdentity(t *testing.T) {
	t.Parallel()

	pdf, _, link := fitLinkFixture(t)
	inspector := &fitInspector{pdf: pdf}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	checks := []func() error{
		func() error { return inspector.catalog(ctx) },
		func() error {
			return inspector.destinationEntries(
				ctx,
				types.Dict{keyNames: types.Array{types.StringLiteral(fitChapterFixtureName), nil}},
				newFitGraphWalk(),
			)
		},
		func() error { _, err := inspector.linkCoordinates(ctx, link, PageFit{}); return err },
		func() error { _, err := inspector.fitNumbers(ctx, types.Array{types.Integer(1)}); return err },
		func() error { return inspector.linkFlags(ctx, types.Dict{"F": types.Integer(0)}) },
		func() error { return inspector.link(ctx, link, PageFit{}) },
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, context.Canceled) {
			t.Fatalf("operation cancellation became policy refusal: %v", err)
		}
	}
}

func TestFitDestinationsRejectInvalidNameTypesAndNonlocalModes(t *testing.T) {
	t.Parallel()

	pdf, _, _ := fitLinkFixture(t)

	inspector := fitInspector{pdf: pdf, pages: map[types.IndirectRef]bool{*types.NewIndirectRef(3, 0): true}}
	if _, err := inspector.destinationKey(t.Context(), types.Integer(1)); err == nil {
		t.Fatal("numeric destination key admitted")
	}

	for _, destination := range []types.Array{
		{types.Integer(3), types.Name(fitDestinationMode)},
		{*types.NewIndirectRef(999, 0), types.Name(fitDestinationMode)},
		{*types.NewIndirectRef(3, 0), types.Name("XYZ")},
	} {
		if err := inspector.fitDestinationArray(t.Context(), destination); err == nil {
			t.Fatal("nonlocal or coordinate-bearing destination admitted")
		}
	}
}

func TestFitCatalogRefusalOrderUsesLegacyNamespaceThenLexicalName(t *testing.T) {
	t.Parallel()

	pdf, _, _ := fitLinkFixture(t)
	valid := fitLocalAction()["D"]
	pdf.RootDict[keyDests] = types.Dict{"z": valid, "m": valid, "a": types.Integer(1)}

	pdf.RootDict[keyNames] = types.Dict{keyDests: types.Dict{keyNames: types.Array{
		types.StringLiteral("0"), types.Integer(1), types.StringLiteral("b"), valid, types.StringLiteral("n"), valid,
	}}}

	_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
	if err == nil || !strings.Contains(err.Error(), `named destination "a" (legacy=true)`) {
		t.Fatalf("refusal attribution ignored destination namespace/order: %v", err)
	}
}

func TestFitHotspotsRejectNonfiniteCoordinatesAndTransformedOverflow(t *testing.T) {
	t.Parallel()

	pdf, _, link := fitLinkFixture(t)

	inspector := fitInspector{pdf: pdf}
	if _, err := inspector.fitNumbers(t.Context(), types.Array{types.Float(math.NaN())}); err == nil {
		t.Fatal("nonfinite materialized coordinate admitted")
	}

	matrix := Affine{math.MaxFloat64, 0, 0, 1, 0, 0}
	if _, err := inspector.linkCoordinates(t.Context(), link, PageFit{Visible: [4]float64{0, 0, 1000, 1000}, Matrix: matrix}); err == nil {
		t.Fatal("overflowed transformed rectangle admitted")
	}

	quad := fitNumberArray([]float64{1, 1, 3, 1, 3, 3, 1, 3})
	if _, err := inspector.quadPoints(t.Context(), quad, [4]float64{0, 0, 4, 4}, matrix); err == nil {
		t.Fatal("overflowed serialized quad vertex admitted")
	}
}

func fitCancellationBoundaryAtBudget(t *testing.T, action string, budget int64) error {
	t.Helper()
	pdf, page, _ := fitLinkFixture(t)

	ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
	if err != nil {
		t.Fatal(err)
	}

	inspector := fitInspector{
		pdf:               pdf,
		owner:             *types.NewIndirectRef(3, 0),
		annotationObjects: map[types.IndirectRef]bool{},
		annotationArrays:  map[types.IndirectRef]bool{},
		destinations:      map[fitDestinationKey]types.Object{},
	}
	ctx := newFormCheckpointContext(t.Context(), t)
	ctx.remaining.Store(budget)

	if action == "annotations" {
		return inspector.annotations(ctx, page, ranges[0].Fit)
	}

	return inspector.destinationEntries(
		ctx,
		types.Dict{keyNames: types.Array{types.StringLiteral(fitChapterFixtureName), fitLocalAction()["D"]}},
		newFitGraphWalk(),
	)
}

func TestFitAnnotationAndDestinationEntriesHonorEveryCancellationCheckpoint(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"annotations", "destination entries"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			for budget := range int64(100) {
				err := fitCancellationBoundaryAtBudget(t, action, budget)
				if err == nil {
					return
				}

				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation became a policy error at %d: %v", budget, err)
				}
			}

			t.Fatal("uncancelled traversal never completed")
		})
	}
}
