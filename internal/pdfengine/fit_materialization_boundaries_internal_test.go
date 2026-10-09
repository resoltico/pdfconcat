// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func materializedBoundaryFit(t *testing.T, pdf *model.Context, page types.Dict, target PageSize) PageFit {
	t.Helper()

	geometry, err := decomposePage(t.Context(), pdf, page, inheritedAttrs{})
	if err != nil {
		t.Fatal(err)
	}

	fit, err := fitGeometry(geometry, target)
	if err != nil {
		t.Fatal(err)
	}

	return fit
}

func TestFitPrintBoxesPreserveContainmentAndRejectContradictions(t *testing.T) {
	t.Parallel()

	for _, target := range []PageSize{{595.2755905511812, 841.8897637795276}, {612, 1008}} {
		t.Run(targetName(target), func(t *testing.T) {
			t.Parallel()

			checkFitPrintBoxPaper(t, target)
		})
	}

	for _, relation := range [][2]string{{keyTrimBox, keyBleedBox}, {keyArtBox, keyBleedBox}, {keyArtBox, keyTrimBox}} {
		boxes := map[string][4]float64{relation[0]: {0, 0, 30, 30}, relation[1]: {0, 0, 20, 20}}
		if err := consistentFitBoxes(boxes); err == nil {
			t.Fatalf("contradictory %s/%s admitted", relation[0], relation[1])
		}
	}
}

func checkFitPrintBoxPaper(t *testing.T, target PageSize) {
	t.Helper()
	pdf, page, _ := fitLinkFixture(t)
	inspector := fitInspector{pdf: pdf}

	fit := materializedBoundaryFit(t, pdf, page, target)
	for _, test := range []struct {
		box  types.Object
		want string
	}{
		{types.Integer(1), "BleedBox"},
		{types.NewNumberArray(-1, 0, 20, 20), "original /MediaBox"},
		{types.NewNumberArray(0, 0, 20, 20), ""},
	} {
		page[keyBleedBox] = test.box

		boxes, err := inspector.printBoxes(t.Context(), page, fit)
		if test.want == "" {
			if err != nil || len(boxes) != 1 {
				t.Fatalf("valid print box changed: %v %v", boxes, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("invalid print box admitted: %v", err)
		}
	}

	page[keyCropBox] = types.NewNumberArray(100, 100, 500, 700)
	page[keyBleedBox] = types.NewNumberArray(0, 0, 612, 792)

	fit = materializedBoundaryFit(t, pdf, page, target)
	if _, err := inspector.printBoxes(t.Context(), page, fit); err == nil || !strings.Contains(err.Error(), "target sheet") {
		t.Fatalf("native crop makes full-media print box off-sheet, but it was admitted: %v", err)
	}

	page[keyMediaBox] = types.NewNumberArray(0, 0, math.MaxFloat64, math.MaxFloat64)
	page[keyCropBox] = types.NewNumberArray(0, 0, 1, 1)
	page[keyBleedBox] = page[keyMediaBox].Clone()

	fit = materializedBoundaryFit(t, pdf, page, target)
	if _, err := inspector.printBoxes(t.Context(), page, fit); err == nil {
		t.Fatal("huge original print box overflows crop-based fitting, but it was admitted")
	}
}

func TestFitMaterializationRejectsMissingCapturedPagesAndChangedPageCount(t *testing.T) {
	t.Parallel()

	pdf := readUnvalidated(t, pdffixture.Pages("captured", 2))

	ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
	if err != nil {
		t.Fatal(err)
	}

	for _, captured := range [][]FitRange{nil, {{First: 2, Last: 2, Fit: ranges[0].Fit}}, {{First: 1, Last: 1, Fit: ranges[0].Fit}}} {
		fresh := readUnvalidated(t, pdffixture.Pages("captured", 2))
		if err = applyPageFits(t.Context(), fresh, captured); err == nil || !strings.Contains(err.Error(), "missing captured fit") {
			t.Fatalf("missing capture admitted: %v", err)
		}
	}

	onePage := readUnvalidated(t, pdffixture.Plain("changed"))
	if err = applyPageFits(t.Context(), onePage, ranges); err == nil || !strings.Contains(err.Error(), "page count changed") {
		t.Fatalf("stale page count admitted: %v", err)
	}

	pdf.RootDict["Pages"] = types.Integer(1)
	if err = applyPageFits(t.Context(), pdf, ranges); err == nil || !strings.Contains(err.Error(), "page tree") {
		t.Fatalf("malformed imported root admitted: %v", err)
	}
}

func TestFittedResourceBindingsCloneScopeHandleNullAndAvoidAllCollisions(t *testing.T) {
	t.Parallel()

	pdf, _, _ := fitLinkFixture(t)
	form := *types.NewIndirectRef(40, 0)
	original := types.Dict{
		keyXObject:    types.Dict{fitContentName: types.Name("source"), fitContentName + "1": types.Name("second")},
		fitColorSpace: types.Dict{"CS": types.Name(fitDeviceRGB)},
	}
	before := original.Clone()

	resources, name, err := fittedPageResources(t.Context(), pdf, original, form)
	if err != nil || name != fitContentName+"2" || !reflect.DeepEqual(before, original) {
		t.Fatalf("binding collision/scope: %q %v", name, err)
	}

	fittedBoundaryXObjects(t, pdf, resources)["private"] = types.Boolean(true)
	if fittedBoundaryXObjects(t, pdf, original)["private"] != nil {
		t.Fatal("wrapper mutated source XObjects")
	}

	for _, source := range []types.Object{nil, types.Dict{keyXObject: nil}} {
		output, binding, resourceErr := fittedPageResources(t.Context(), pdf, source, form)
		if resourceErr != nil || fittedBoundaryXObjects(t, pdf, output)[binding] != form {
			t.Fatalf("null resource scope refused: %v", resourceErr)
		}
	}

	for _, source := range []types.Object{types.Integer(1), types.Dict{keyXObject: types.Integer(1)}} {
		if _, _, resourceErr := fittedPageResources(t.Context(), pdf, source, form); resourceErr == nil {
			t.Fatal("malformed resource scope admitted")
		}
	}
}

func fittedBoundaryXObjects(t *testing.T, pdf *model.Context, resources types.Dict) types.Dict {
	t.Helper()

	objects, err := pdf.DereferenceDictContext(t.Context(), resources[keyXObject])
	if err != nil || objects == nil {
		t.Fatalf("fitted XObjects unavailable: %v", err)
	}

	return objects
}

func TestFittedSheetVerifierRejectsEffectiveGeometryAndIncorrectEdges(t *testing.T) {
	t.Parallel()

	for _, target := range []PageSize{{595.2755905511812, 841.8897637795276}, {612, 1008}} {
		for _, change := range []string{keyRotate, "UserUnit", keyCropBox, keyMediaBox, "malformed", "root"} {
			pdf, page, _ := fitLinkFixture(t)
			page[keyMediaBox] = types.NewNumberArray(0, 0, target.Width, target.Height)
			page[keyCropBox] = page[keyMediaBox].Clone()
			page[keyRotate], page["UserUnit"] = types.Integer(0), types.Integer(1)

			if err := verifyFittedSheets(t.Context(), pdf, target); err != nil {
				t.Fatal(err)
			}

			switch change {
			case keyRotate:
				page[keyRotate] = types.Integer(90)
			case "UserUnit":
				page["UserUnit"] = types.Integer(2)
			case keyCropBox:
				delete(page, keyCropBox)
			case keyMediaBox:
				page[keyMediaBox] = types.NewNumberArray(0, 0, target.Width-1, target.Height)
			case "malformed":
				page[keyMediaBox] = types.Integer(1)
			case "root":
				pdf.RootDict["Pages"] = types.Integer(1)
			default:
				t.Fatalf("unknown output geometry control %s", change)
			}

			if err := verifyFittedSheets(t.Context(), pdf, target); err == nil {
				t.Fatalf("incorrect fitted %s admitted", change)
			}
		}
	}
}

func TestFitMaterializationCancellationCannotPublishPageBindings(t *testing.T) {
	t.Parallel()

	completed := false

	for budget := range int64(150) {
		pdf, page, _ := fitLinkFixture(t)

		ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
		if err != nil {
			t.Fatal(err)
		}

		before := page.Clone()
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		err = fitPageContent(ctx, pdf, page, inheritedAttrs{}, ranges[0].Fit)
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(page, before) {
			t.Fatalf("canceled fitting published page scope at %d: %v", budget, err)
		}
	}

	if !completed {
		t.Fatal("uncancelled fitting never completed")
	}
}

func TestFitCapturedAnnotationReplayRefusesChangedGraphAndPreservesCancellation(t *testing.T) {
	t.Parallel()

	for _, change := range []string{"print box", "array", "annotation", "rectangle", "content"} {
		pdf, page, link := fitLinkFixture(t)

		ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
		if err != nil {
			t.Fatal(err)
		}

		switch change {
		case "print box":
			page[keyBleedBox] = types.NewNumberArray(-1, 0, 20, 20)
		case "array":
			page["Annots"] = types.Integer(1)
		case "annotation":
			page["Annots"] = types.Array{types.Integer(1)}
		case "rectangle":
			link[keyRect] = types.Integer(1)
		case "content":
			page[keyContents] = types.Integer(1)
		default:
			t.Fatalf("unknown captured graph control %s", change)
		}

		if err = applyPageFits(t.Context(), pdf, ranges); err == nil {
			t.Fatalf("captured graph drift %s admitted", change)
		}
	}
}

func TestFitPrintBoxCancellationPreservesOperationIdentity(t *testing.T) {
	t.Parallel()
	pdf, page, _ := fitLinkFixture(t)
	inspector := fitInspector{pdf: pdf}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := inspector.printBoxes(ctx, page, PageFit{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("printbox cancellation became refusal: %v", err)
	}
}

func TestFitAnnotationTransformHonorsEveryContextCheckpoint(t *testing.T) {
	t.Parallel()

	inspector := fitInspector{}
	completed := false

	for budget := range int64(150) {
		pdf, page, _ := fitLinkFixture(t)

		ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
		if err != nil {
			t.Fatal(err)
		}

		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		inspector.pdf = pdf

		err = inspector.transformObjects(ctx, page, ranges[0].Fit)
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("native annotation transform lost cancellation at%d: %v", budget, err)
		}
	}

	if !completed {
		t.Fatal("uncancelled annotation transform never completed")
	}
}

func TestFittingImportHonorsCancellationAtEveryNativeCheckpoint(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "fitting-cancel.pdf")
	if err = pdffixture.URILink("fitting cancellation", fitAbsoluteTarget, "", false).WriteFile(source); err != nil {
		t.Fatal(err)
	}

	target := PageSize{612, 1008}
	importer := pool{engine: engine}
	completed := false

	for budget := range int64(3000) {
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		pdf, importErr := importer.readImport(ctx, 1, source, &target)
		if importErr == nil {
			if err = verifyFittedSheets(t.Context(), pdf, target); err != nil {
				t.Fatal(err)
			}

			completed = true

			break
		}

		if CodeOf(importErr) != CodeCanceled || !errors.Is(importErr, context.Canceled) {
			t.Fatalf("fitting import budget%d: %v", budget, importErr)
		}
	}

	if !completed {
		t.Fatal("uncancelled fitting import never completed")
	}
}
