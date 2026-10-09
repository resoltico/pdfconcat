// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestFitDestinationNamespacesRemainDistinctAtIdenticalSpelling(t *testing.T) {
	t.Parallel()
	pdf, _, _ := fitLinkFixture(t)
	legacyPage, treePage := *types.NewIndirectRef(3, 0), *types.NewIndirectRef(20, 0)
	legacy := types.Array{legacyPage, types.Name(fitDestinationMode)}
	tree := types.Array{treePage, types.Name(fitDestinationMode)}
	pdf.RootDict[keyDests] = types.Dict{fitChapterFixtureName: legacy}
	pdf.RootDict[keyNames] = types.Dict{keyDests: types.Dict{keyNames: types.Array{types.StringLiteral(fitChapterFixtureName), tree}}}

	inspector := fitInspector{
		pdf:          pdf,
		pages:        map[types.IndirectRef]bool{legacyPage: true, treePage: true},
		destinations: map[fitDestinationKey]types.Object{},
	}
	if err := inspector.catalog(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name types.Object
		page types.IndirectRef
	}{
		{types.Name(fitChapterFixtureName), legacyPage},
		{types.StringLiteral(fitChapterFixtureName), treePage},
		{types.HexLiteral("63686170746572"), treePage},
	} {
		inspector.pages = map[types.IndirectRef]bool{test.page: true}
		if err := inspector.destination(t.Context(), test.name, newFitGraphWalk(), 0); err != nil {
			t.Fatalf("typed lookup resolved the wrong page: %v", err)
		}
	}
}

func TestFitDestinationRejectsWrongNamespaceMalformedKeysAndDuplicates(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{
		"name against tree", "string against legacy", "name tree key", "duplicate tree key", "odd pairs", "empty tree key",
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			pdf, _, link := fitLinkFixture(t)
			destination := fitLocalAction()["D"]
			entries := types.Array{types.StringLiteral(fitChapterFixtureName), destination}
			pdf.RootDict[keyNames] = types.Dict{keyDests: types.Dict{keyNames: entries}}
			link["A"] = types.Dict{"S": types.Name(fitGoToFixture), "D": types.Name(fitChapterFixtureName)}

			switch mode {
			case "string against legacy":
				delete(pdf.RootDict, keyNames)
				pdf.RootDict[keyDests] = types.Dict{fitChapterFixtureName: destination}
				link["A"] = types.Dict{"S": types.Name(fitGoToFixture), "D": types.StringLiteral(fitChapterFixtureName)}
			case "name tree key":
				entries[0] = types.Name(fitChapterFixtureName)
			case "duplicate tree key":
				pdf.RootDict[keyNames] = types.Dict{
					keyDests: types.Dict{keyNames: append(entries, types.HexLiteral("63686170746572"), destination)},
				}
			case "odd pairs":
				pdf.RootDict[keyNames] = types.Dict{keyDests: types.Dict{keyNames: entries[:1]}}
			case "empty tree key":
				entries[0] = types.StringLiteral("")
			default: // Name objects cannot resolve the string-indexed tree.
			}

			if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); !errors.Is(err, errFitUnsupported) {
				t.Fatalf("invalid namespace or tree passed: %v", err)
			}
		})
	}
}

func TestFitDestinationRefusesStructureAlternatesAndCycles(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"action SD", "named SD", "name cycle", "dictionary cycle", "tree cycle"} {
		pdf, _, link := fitLinkFixture(t)
		fit := fitLocalAction()["D"]
		root := types.Dict{"D": fit}

		switch mode {
		case "action SD":
			link["A"] = types.Dict{
				"S":  types.Name(fitGoToFixture),
				"D":  fit,
				"SD": types.Array{types.StringLiteral("structure"), types.Name(fitDestinationMode)},
			}
		case "named SD":
			root["SD"] = types.Array{types.StringLiteral("structure"), types.Name(fitDestinationMode)}
			pdf.RootDict[keyDests] = types.Dict{fitChapterFixtureName: root}
		case "name cycle":
			pdf.RootDict[keyDests] = types.Dict{fitChapterFixtureName: types.StringLiteral(fitChapterFixtureName)}
			pdf.RootDict[keyNames] = types.Dict{
				keyDests: types.Dict{keyNames: types.Array{types.StringLiteral(fitChapterFixtureName), types.Name(fitChapterFixtureName)}},
			}
		case "dictionary cycle":
			root["D"] = fitIndirect(t, pdf, root)
			pdf.RootDict[keyDests] = types.Dict{fitChapterFixtureName: root}
		case "tree cycle":
			root = types.Dict{}
			root["Kids"] = types.Array{fitIndirect(t, pdf, root)}
			pdf.RootDict[keyNames] = types.Dict{keyDests: root}
		default:
			t.Fatal("unknown structure/cycle control")
		}

		if _, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008}); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("structure alternate/cycle %s passed: %v", mode, err)
		}
	}
}

func TestFitAnnotationOwnershipAllowsOnlySharedEmptyArrays(t *testing.T) {
	t.Parallel()

	for _, nonempty := range []bool{false, true} {
		pdf := readUnvalidated(t, pdffixture.Pages("ownership", 2))

		array := types.Array{}
		if nonempty {
			array = types.Array{
				types.Dict{
					keySubtype:   types.Name("Link"),
					"Rect":       types.NewNumberArray(10, 10, 20, 20),
					fitBorderKey: types.NewNumberArray(0, 0, 0),
					"A":          fitURIAction(),
				},
			}
		}

		shared := fitIndirect(t, pdf, array)

		root, err := pdf.Pages()
		if err != nil {
			t.Fatal(err)
		}

		err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, page types.Dict, _ inheritedAttrs) error {
			page["Annots"] = shared
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
		if nonempty && (!errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), "shared between pages")) ||
			!nonempty && err != nil {
			t.Fatalf("empty=%t ownership judgment: %v", !nonempty, err)
		}
	}
}
