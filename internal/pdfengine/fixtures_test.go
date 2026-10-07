// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

// fixture is a source document together with the facts a test knows about it independently of the
// engine: the text each page shows, where its links lead, and its page attributes.
type fixture struct {
	doc   *pdffixture.Doc
	texts func(page int) string
	fact  pdforacle.SourceFact
}

const (
	// resourcePages is the number of pages of the generated-pages fixture.
	resourcePages = 3

	// Names of the fixtures that several tests refer to.
	fixtureLinks  = "links"
	fixtureLegacy = "legacy"
	fixtureForm   = "form"
)

// letterGeometry is what the plain fixtures declare.
func letterGeometry() pdforacle.Geometry {
	return pdforacle.Geometry{MediaBox: []float64{0, 0, 612, 792}}
}

func uniformGeometry(pages int, geometry pdforacle.Geometry) map[int]pdforacle.Geometry {
	result := map[int]pdforacle.Geometry{}
	for page := 1; page <= pages; page++ {
		result[page] = geometry
	}

	return result
}

func marked(tag string) func(int) string { return func(int) string { return tag } }

func numbered(tag string) func(int) string {
	return func(page int) string { return fmt.Sprintf("%s p%d", tag, page) }
}

func twoPageLinks() map[int][]int { return map[int][]int{1: {2}, 2: {1}} }

// fixtureCatalog describes every source fixture used by the matrix tests, keyed by name.
func fixtureCatalog() map[string]fixture {
	cropGeometry := map[int]pdforacle.Geometry{
		1: {MediaBox: []float64{0, 0, 612, 792}, CropBox: []float64{50, 60, 500, 700}, Rotate: 90},
		2: {MediaBox: []float64{0, 0, 612, 792}, CropBox: []float64{10, 10, 300, 400}, Rotate: 90},
	}
	unitGeometry := map[int]pdforacle.Geometry{
		1: {MediaBox: []float64{10, 20, 310, 420}, Rotate: 270, UserUnit: 2.5},
	}
	plain := func(tag string, doc *pdffixture.Doc, version int) fixture {
		fact := pdforacle.SourceFact{Pages: 1, Version: version, Geometry: uniformGeometry(1, letterGeometry())}

		return fixture{doc: doc, texts: marked(tag), fact: fact}
	}
	twoPage := func(tag string, doc *pdffixture.Doc, links map[int][]int) fixture {
		fact := pdforacle.SourceFact{Pages: 2, Version: 17, Links: links, Geometry: uniformGeometry(2, letterGeometry())}

		return fixture{doc: doc, texts: numbered(tag), fact: fact}
	}

	return map[string]fixture{
		fixturePlainA: plain("PLAINA", pdffixture.Plain("PLAINA"), 17),
		fixturePlainB: plain("PLAINB", pdffixture.Plain("PLAINB"), 17),
		fixtureTitled: plain("TITLED", pdffixture.Titled("TITLED"), 17),
		fixtureMulti: {
			doc:   pdffixture.Pages("MULTI", 5),
			texts: numbered("MULTI"),
			fact:  pdforacle.SourceFact{Pages: 5, Version: 17, Geometry: uniformGeometry(5, letterGeometry())},
		},
		fixtureLinks:   twoPage(linkedPageMarker, pdffixture.Links(linkedPageMarker), twoPageLinks()),
		fixtureNamed:   twoPage("NAMED", pdffixture.NamedDests("NAMED"), twoPageLinks()),
		fixtureLegacy:  twoPage("OLDD", pdffixture.LegacyDests("OLDD"), twoPageLinks()),
		"legacy2":      twoPage("OLDE", pdffixture.LegacyDests("OLDE"), twoPageLinks()),
		fixtureForm:    twoPage("FORM", pdffixture.Form("FORM"), nil),
		"form2":        twoPage("FORMTWO", pdffixture.Form("FORMTWO"), nil),
		fixtureActions: twoPage("PAGEACT", pdffixture.PageActions("PAGEACT"), twoPageLinks()),
		fixtureOutline: twoPage("OUTL", pdffixture.Outlined("OUTL"), nil),
		fixtureCrop: {
			doc:   pdffixture.NestedBoxes("CROP"),
			texts: numbered("CROP"),
			fact:  pdforacle.SourceFact{Pages: 2, Version: 17, Geometry: cropGeometry},
		},
		fixtureUserUnit: {
			doc: pdffixture.WithBoxes(
				"UNIT",
				pdffixture.PageBoxes{},
				pdffixture.PageBoxes{Media: croppedPageBox, Rotate: "270", UserUnit: "2.5"},
			),
			texts: numbered("UNIT"),
			fact:  pdforacle.SourceFact{Pages: 1, Version: 17, Geometry: unitGeometry},
		},
		fixtureVersion14: plain("V14", pdffixture.Versioned("V14", pdfVersion14, ""), 14),
		"v16cat":         plain("V16CAT", pdffixture.Versioned("V16CAT", pdfVersion14, "1.6"), 16),
		fixtureVersion20: plain("V20", pdffixture.Versioned("V20", pdfVersion20, ""), 20),
		fixtureTagged:    plain("TAGGED", pdffixture.Tagged("TAGGED"), 17),
		fixtureAttached:  plain("ATTACH", pdffixture.Attachment("ATTACH"), 17),
	}
}

func generatedLabel(index int) string { return fmt.Sprintf("GEN %d", index) }

func generatedGeometry(index int) pdforacle.Geometry {
	if index%2 == 0 {
		return pdforacle.Geometry{MediaBox: []float64{0, 0, 595, 842}}
	}

	return letterGeometry()
}

// writeCatalog writes every fixture and the resource document into dir and returns their paths.
func writeCatalog(tb testing.TB, dir string) (map[string]string, string) {
	tb.Helper()

	paths := map[string]string{}
	for name, f := range fixtureCatalog() {
		paths[name] = writeDoc(tb, dir, name, f.doc)
	}

	resource := filepath.Join(dir, "resource.pdf")
	if err := pdffixture.Resource(resourcePages, generatedLabel).WriteFile(resource); err != nil {
		tb.Fatal(err)
	}

	return paths, resource
}
