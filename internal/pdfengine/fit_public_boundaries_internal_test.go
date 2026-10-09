// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func fitBoundaryDocument(t *testing.T, doc *pdffixture.Doc) (*Engine, string) {
	t.Helper()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), fitRenderSource)
	if err = doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}

	return engine, path
}

func TestFitPlanRejectsAlteredCapturedIntervals(t *testing.T) {
	t.Parallel()
	engine, path := fitBoundaryDocument(t, pdffixture.Pages("captured facts", 1))
	target := PageSize{100, 100}

	info, err := engine.Inspect(t.Context(), path, &target)
	if err != nil {
		t.Fatal(err)
	}

	plan := AssemblyPlan{
		FitTarget:     &target,
		Sources:       []SourceFile{{Path: path, Info: info}},
		Order:         []Run{SourcePages(0, 1, 1)},
		ExpectedPages: 1,
	}
	if err = plan.Validate(); err != nil {
		t.Fatalf("real captured facts refused: %v", err)
	}

	for _, change := range []string{"first", "last", "target"} {
		altered := info
		altered.Fits = append([]FitRange(nil), info.Fits...)

		switch change {
		case "first":
			altered.Fits[0].First = 2
		case "last":
			altered.Fits[0].Last = 2
		case "target":
			altered.Fits[0].Fit.Target = PageSize{50, 50}
		default:
			t.Fatalf("unknown captured-fact control %s", change)
		}

		plan.Sources[0].Info = altered
		if err = plan.Validate(); err == nil || !errors.Is(err, errFitUnsupported) {
			t.Fatalf("altered captured %s admitted: %v", change, err)
		}
	}
}

func TestFitInspectRetainsOriginalMalformedFeatureError(t *testing.T) {
	t.Parallel()
	engine, path := fitBoundaryDocument(t, rawDoc("<< /Type /Catalog /Pages 2 0 R /AA 1 >>", singlePageTreeBody, page))

	_, err := engine.Inspect(t.Context(), path, &PageSize{100, 100})
	if CodeOf(err) != CodeInvalid || !strings.Contains(err.Error(), "additional actions") {
		t.Fatalf("original feature reader failure lost: %v", err)
	}
}

func TestFitActualTinySourceRefusesOverflowingSheetScale(t *testing.T) {
	t.Parallel()

	tiny := strconv.FormatFloat(math.SmallestNonzeroFloat64, 'f', -1, 64)
	doc := rawDoc(catalog, singlePageTreeBody, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 "+tiny+" "+tiny+"] /Resources <<>> >>")
	engine, path := fitBoundaryDocument(t, doc)

	_, err := engine.Inspect(t.Context(), path, &PageSize{100, 100})
	if CodeOf(err) != CodeFitUnsupported || !strings.Contains(err.Error(), "scale overflows or underflows") {
		t.Fatalf("real unrepresentable source scale misclassified: %v", err)
	}
}

func TestFitWrittenSheetVerifierRejectsActualDifferentGeometry(t *testing.T) {
	t.Parallel()
	engine, path := fitBoundaryDocument(
		t,
		pdffixture.WithBoxes(
			"written sheet",
			pdffixture.PageBoxes{},
			pdffixture.PageBoxes{Media: guardHundredPointBox, Crop: guardHundredPointBox},
		),
	)

	request := AssembleRequest{Destination: path, ExpectedPages: 1, FitTarget: &PageSize{100, 100}}
	if err := engine.verifyOutput(t.Context(), &request); err != nil {
		t.Fatalf("actual matching sheet refused: %v", err)
	}

	request.FitTarget = &PageSize{200, 200}
	if err := engine.verifyOutput(t.Context(), &request); CodeOf(err) != CodeOutputInvalid || !errors.Is(err, errFitGeometry) {
		t.Fatalf("valid but wrong written sheet admitted: %v", err)
	}
}

func TestFitActualSubnormalFormCoefficientsPreserveDeclaredDomain(t *testing.T) {
	t.Parallel()

	small := strconv.FormatFloat(1e-308, 'f', -1, 64)
	doc := rawDoc(
		catalog,
		singlePageTreeBody,
		guardLeafResourcePage,
		string(fitFixtureStream("", []byte(guardPaintLeaf))),
		string(
			fitFixtureStream("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources <<>> /Matrix ["+small+" 0 0 "+small+" 0 0]", nil),
		),
	)

	engine, path := fitBoundaryDocument(t, doc)
	if _, err := engine.Inspect(t.Context(), path, &PageSize{50, 50}); err != nil {
		t.Fatalf("actual finite subnormal transform refused: %v", err)
	}
}

func TestFitWrittenSheetVerificationUsesRealCancellationAtEveryCheckpoint(t *testing.T) {
	t.Parallel()
	engine, path := fitBoundaryDocument(
		t,
		pdffixture.WithBoxes(
			"verified sheet",
			pdffixture.PageBoxes{},
			pdffixture.PageBoxes{Media: guardHundredPointBox, Crop: guardHundredPointBox},
		),
	)
	request := AssembleRequest{Destination: path, ExpectedPages: 1, FitTarget: &PageSize{100, 100}}

	assertFitResourceCancellationCheckpoints(t, func(ctx context.Context) error { return engine.verifyOutput(ctx, &request) })
}

func TestFitActualFormCompositionRefusesSourceAndFittedOverflow(t *testing.T) {
	t.Parallel()

	large := strconv.FormatFloat(1e308, 'f', -1, 64) + ".0"

	for _, tc := range []struct {
		name, program string
		target        float64
	}{{"source", "2 0 0 2 0 0 cm /Leaf Do", 50}, {"fitted", ".5 0 0 .5 0 0 cm /Leaf Do", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := rawDoc(
				catalog,
				singlePageTreeBody,
				guardLeafResourcePage,
				string(fitFixtureStream("", []byte(tc.program))),
				string(
					fitFixtureStream(
						"/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources <<>> /Matrix ["+large+" 0 0 "+large+" 0 0]",
						[]byte(emptyAppearanceDrawing),
					),
				),
			)
			engine, path := fitBoundaryDocument(t, doc)

			_, err := engine.Inspect(t.Context(), path, &PageSize{tc.target, tc.target})
			if CodeOf(err) != CodeFitUnsupported || !strings.Contains(err.Error(),
				"composed content matrix is not finite") {
				t.Fatalf("actual %s Form overflow not refused: %v", tc.name, err)
			}
		})
	}
}

func TestFitActualPatternEntrySourceAnchorCannotHideOverflow(t *testing.T) {
	t.Parallel()

	large := strconv.FormatFloat(1e308, 'f', -1, 64) + ".0"
	form := fitFixtureStream(
		"/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Matrix [2 0 0 2 0 0] /Resources << /Pattern << /P 6 0 R >> >>",
		[]byte(guardPaintPattern),
	)
	pattern := fitFixtureStream(
		"/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 1 1] /XStep 1 /YStep 1 "+
			"/Resources <<>> /Matrix ["+large+" 0 0 "+large+" 0 0]",
		[]byte(emptyAppearanceDrawing),
	)
	doc := rawDoc(
		catalog,
		singlePageTreeBody,
		guardLeafResourcePage,
		string(fitFixtureStream("", []byte(guardPaintLeaf))),
		string(form),
		string(pattern),
	)
	engine, path := fitBoundaryDocument(t, doc)

	_, err := engine.Inspect(t.Context(), path, &PageSize{50, 50})
	if CodeOf(err) != CodeFitUnsupported || !strings.Contains(err.Error(),
		"composed content matrix is not finite") {
		t.Fatalf("actual captured pattern source-anchor overflow not refused: %v", err)
	}
}

func TestFitActualCalibrationRequiresNonnullDictionary(t *testing.T) {
	t.Parallel()

	for _, parameters := range []string{"<< /WhitePoint [1 1 1] >>", nullPDFObject} {
		t.Run(parameters, func(t *testing.T) {
			t.Parallel()

			doc := rawDoc(
				catalog,
				singlePageTreeBody,
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> "+
					"/Group << /S /Transparency /CS [/CalGray "+parameters+"] >> >>",
			)
			engine, path := fitBoundaryDocument(t, doc)

			_, err := engine.Inspect(t.Context(), path, &PageSize{100, 100})
			if parameters == nullPDFObject {
				if CodeOf(err) != CodeFitUnsupported || !strings.Contains(err.Error(),
					"calibration parameters must be a dictionary") {
					t.Fatalf("actual null calibration accepted: %v", err)
				}
			} else if err != nil {
				t.Fatalf("real calibrated source group refused: %v", err)
			}
		})
	}
}

func TestFitPlanRejectsInvalidTargets(t *testing.T) {
	t.Parallel()

	plan := AssemblyPlan{GeneratedSpecs: 1, Order: []Run{GeneratedPages(0, 1)}, ExpectedPages: 1}
	for _, bad := range []PageSize{{0, 100}, {100, math.Inf(1)}, {math.NaN(), 100}} {
		plan.FitTarget = &bad
		if err := plan.Validate(); CodeOf(err) != CodeRequestInvalid || !errors.Is(err, errFitGeometry) {
			t.Fatalf("invalid public target admitted: %v", err)
		}
	}
}
