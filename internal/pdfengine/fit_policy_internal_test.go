// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	fitChapterFixtureName = "chapter"
	fitAbsoluteTarget     = "https://example.invalid/annex.pdf"
	fitGoToFixture        = "GoTo"
)

func fitLinkFixture(t *testing.T) (*model.Context, types.Dict, types.Dict) {
	t.Helper()
	pdf := readUnvalidated(t, pdffixture.URILink("fit link", fitAbsoluteTarget, "", false))

	page, err := pdf.DereferenceDict(*types.NewIndirectRef(3, 0))
	if err != nil {
		t.Fatal(err)
	}

	annotations, err := pdf.DereferenceArray(page["Annots"])
	if err != nil {
		t.Fatal(err)
	}

	link, err := pdf.DereferenceDict(annotations[0])
	if err != nil {
		t.Fatal(err)
	}

	return pdf, page, link
}

func TestFitPolicySupportsAbsoluteURIAndCoordinateFreeLocalDestinations(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"URI", "direct", fitGoToFixture, "name tree", "legacy", "empty optional state"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			pdf, _, link := fitLinkFixture(t)
			destination := types.Array{*types.NewIndirectRef(3, 0), types.Name(fitDestinationMode)}

			switch mode {
			case "direct":
				delete(link, "A")
				link["Dest"] = destination
			case fitGoToFixture:
				link["A"] = types.Dict{"S": types.Name(fitGoToFixture), "D": destination}
			case "name tree":
				pdf.RootDict[keyNames] = types.Dict{
					keyDests: types.Dict{keyNames: types.Array{types.StringLiteral(fitChapterFixtureName), destination}},
				}
				link["A"] = types.Dict{"S": types.Name(fitGoToFixture), "D": types.StringLiteral(fitChapterFixtureName)}
			case "legacy":
				pdf.RootDict[keyDests] = types.Dict{fitChapterFixtureName: types.Dict{"D": destination}}
				link["A"] = types.Dict{"S": types.Name(fitGoToFixture), "D": types.Name(fitChapterFixtureName)}
			case "empty optional state":
				link["AP"], link["AA"], link["QuadPoints"] = types.Dict{}, types.Dict{}, types.Array{}
				link["A"] = types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget), keyNext: types.Array{}}
			default: // The URI fixture already has a stable absolute target.
			}

			ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
			if err != nil || len(ranges) != 1 || ranges[0].First != 1 || ranges[0].Last != 1 {
				t.Fatalf("supported fit %s: %v %v", mode, ranges, err)
			}
		})
	}
}

func TestFitPolicyRejectsInteractiveAndMetadataShapesWithSourcePageAndKey(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		patch func(*model.Context, types.Dict, types.Dict)
		key   string
	}{
		{key: "Subtype", patch: func(_ *model.Context, _, link types.Dict) { link[keySubtype] = types.Name(widgetSubtype) }},
		{key: "AP", patch: func(_ *model.Context, _, link types.Dict) {
			link["AP"] = types.Dict{"N": types.StreamDict{Content: []byte(emptyAppearanceDrawing)}}
		}},
		{key: "NoZoom", patch: func(_ *model.Context, _, link types.Dict) { link["F"] = types.Integer(fitNoZoomFlag) }},
		{key: "NoRotate", patch: func(_ *model.Context, _, link types.Dict) { link["F"] = types.Integer(fitNoRotateFlag) }},
		{key: "Border", patch: func(_ *model.Context, _, link types.Dict) { delete(link, "Border") }},
		{key: "BS", patch: func(_ *model.Context, _, link types.Dict) { link["BS"] = types.Dict{"W": types.Integer(1)} }},
		{key: keyNext, patch: func(_ *model.Context, _, link types.Dict) {
			link["A"] = types.Dict{
				"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget),
				keyNext: types.Dict{"S": types.Name("JavaScript")},
			}
		}},
		{key: keyNext, patch: func(_ *model.Context, _, link types.Dict) {
			link["A"] = types.Dict{
				"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget),
				keyNext: types.Array{types.Dict{"S": types.Name("Launch")}},
			}
		}},
		{key: "IsMap", patch: func(_ *model.Context, _, link types.Dict) {
			link["A"] = types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral(fitAbsoluteTarget), "IsMap": types.Boolean(true)}
		}},
		{key: "absolute", patch: func(_ *model.Context, _, link types.Dict) {
			link["A"] = types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral("annex.pdf")}
		}},
		{key: "GoToR", patch: func(_ *model.Context, _, link types.Dict) {
			link["A"] = types.Dict{"S": types.Name("GoToR")}
		}},
		{key: "mixes", patch: func(_ *model.Context, _, link types.Dict) {
			link["Dest"] = types.Array{*types.NewIndirectRef(3, 0), types.Name(fitDestinationMode)}
		}},
		{key: "Rect", patch: func(_ *model.Context, _, link types.Dict) {
			link["Rect"] = types.NewNumberArray(-1, 600, 50, 620)
		}},
		{key: "QuadPoints", patch: func(_ *model.Context, _, link types.Dict) { link["QuadPoints"] = types.NewNumberArray(0, 0) }},
	} {
		t.Run(test.key, func(t *testing.T) {
			t.Parallel()
			pdf, page, link := fitLinkFixture(t)
			test.patch(pdf, page, link)

			_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
			if !errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), test.key) ||
				!strings.Contains(err.Error(), "source page 1") {
				t.Fatalf("unsupported fit lacks source-page/key facts: %v", err)
			}
		})
	}
}

func TestFitPolicyRejectsActivePageCoordinateMetadata(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		patch func(*model.Context, types.Dict, types.Dict)
		key   string
	}{
		{key: "AA", patch: func(_ *model.Context, page, _ types.Dict) {
			page["AA"] = types.Dict{"O": types.Dict{"S": types.Name(fitGoToFixture)}}
		}},
		{key: "B", patch: func(_ *model.Context, page, _ types.Dict) { page["B"] = types.Array{types.Integer(1)} }},
		{key: "VP", patch: func(_ *model.Context, page, _ types.Dict) {
			page["VP"] = types.Array{types.Dict{keyBBox: types.NewNumberArray(0, 0, 10, 10)}}
		}},
		{key: "Measure", patch: func(_ *model.Context, page, _ types.Dict) {
			page["Measure"] = types.Dict{"Subtype": types.Name("GEO")}
		}},
		{key: "LGIDict", patch: func(_ *model.Context, page, _ types.Dict) {
			page["LGIDict"] = types.Dict{"Version": types.StringLiteral("2.1")}
		}},
		{key: "Thumb", patch: func(_ *model.Context, page, _ types.Dict) {
			page["Thumb"] = types.StreamDict{Content: []byte("thumbnail")}
		}},
	} {
		t.Run(test.key, func(t *testing.T) {
			t.Parallel()
			pdf, page, link := fitLinkFixture(t)
			test.patch(pdf, page, link)

			_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
			if !errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("active coordinate metadata accepted: %v", err)
			}
		})
	}
}

func TestFitPolicyChecksInteriorGeometryAndCompressesRepeatedFacts(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.WithBoxes("interior", pdffixture.PageBoxes{Media: guardHundredPointBox},
		pdffixture.PageBoxes{}, pdffixture.PageBoxes{Crop: "[0 0 0 100]"}, pdffixture.PageBoxes{}))

	_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
	if !errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), "source page 2") || !strings.Contains(err.Error(), "CropBox") {
		t.Fatalf("interior geometry was ignored: %v", err)
	}

	pdf = readUnvalidated(t, pdffixture.Pages("same geometry", 1000))

	ranges, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
	if err != nil || len(ranges) != 1 || ranges[0].First != 1 || ranges[0].Last != 1000 {
		t.Fatalf("source geometry was not compactly captured: %v %v", len(ranges), err)
	}
}
