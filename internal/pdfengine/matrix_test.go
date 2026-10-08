// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

// matrixScenario is one row of the adoption matrix: an order of parts and the PDF version the output
// must have at least.
type matrixScenario struct {
	name    string
	parts   []part
	version pdfengine.Version
}

// linkedScenarios repeat sources whose links, destinations, forms and actions tie to their own pages.
func linkedScenarios() []matrixScenario {
	return []matrixScenario{
		{"links forward backward repeat", []part{whole(fixturePlainA), whole(fixtureLinks), gen(0, 1), whole(fixtureLinks)}, 17},
		{"links adjacent", []part{whole(fixtureLinks), whole(fixtureLinks), whole(fixtureLinks)}, 17},
		{"named destinations repeated", []part{whole(fixtureNamed), whole(fixtureNamed), whole(fixturePlainA)}, 17},
		{"forms with duplicate field names", []part{whole(fixtureForm), whole("form2"), whole(fixtureForm)}, 17},
		{"page-level actions", []part{whole(fixtureActions), whole(fixtureActions)}, 17},
		{"single legacy dests occurrence", []part{whole(fixturePlainA), whole(fixtureLegacy), whole(fixturePlainB)}, 17},
	}
}

// geometryScenarios carry page boxes, rotation and UserUnit through assembly.
func geometryScenarios() []matrixScenario {
	return []matrixScenario{
		{
			"geometry cropbox rotation userunit",
			[]part{whole(fixtureCrop), whole(fixtureUserUnit), gen(0, 1), whole(fixtureCrop), whole(fixtureUserUnit), gen(1, 3)},
			17,
		},
		{"crop first without resource", []part{whole(fixtureCrop), whole(fixtureUserUnit), whole(fixtureLinks), whole(fixtureCrop)}, 17},
	}
}

// versionScenarios mix PDF versions; the output is 1.7 unless an input is 2.0.
func versionScenarios() []matrixScenario {
	return []matrixScenario{
		{"version 1.4 then 2.0", []part{whole(fixtureVersion14), whole(fixtureVersion20)}, 20},
		{"version 2.0 first", []part{whole(fixtureVersion20), whole(fixtureVersion14), whole(fixtureVersion20)}, 20},
		{"version 2.0 after generated", []part{gen(0, 1), whole(fixtureVersion14), whole(fixtureVersion20)}, 20},
		{"catalog version 1.6 is raised to 1.7", []part{whole(fixtureVersion14), whole("v16cat"), whole(fixtureVersion14)}, 17},
		{"low versions are written as 1.7", []part{whole(fixtureVersion14), gen(0, 1), whole(fixtureVersion14)}, 17},
	}
}

// droppedScenarios use sources whose document-level parts the policy removes.
func droppedScenarios() []matrixScenario {
	return []matrixScenario{
		{"outlines dropped", []part{whole(fixtureOutline), gen(2, 1), whole(fixtureOutline)}, 17},
		{"outlines dropped when first", []part{whole(fixtureOutline), whole(fixturePlainA)}, 17},
		{"structure tree dropped", []part{whole(fixtureTagged), whole(fixtureTagged)}, 17},
		{"attachments dropped", []part{whole(fixtureAttached), whole(fixtureAttached), whole(fixturePlainA)}, 17},
	}
}

// orderScenarios rearrange and repeat plain sources and generated pages.
func orderScenarios() []matrixScenario {
	return []matrixScenario{
		{"generated only", []part{gen(2, 1), gen(0, 3), gen(1, 1), gen(2, 2)}, 17},
		{
			"reordered ranges of an unannotated source",
			[]part{pages(fixtureMulti, 4, 2), pages(fixtureMulti, 1, 2), pages(fixtureMulti, 2, 2), whole(fixtureMulti)},
			17,
		},
		{
			"unannotated source repeated",
			[]part{whole(fixturePlainA), whole(fixturePlainA), gen(1, 2), whole(fixturePlainA), whole(fixturePlainB), whole(fixturePlainA)},
			17,
		},
		{
			"mixed",
			[]part{
				whole(fixturePlainA),
				gen(1, 1),
				whole(fixtureLinks),
				whole(fixtureForm),
				gen(0, 2),
				gen(1, 2),
				whole(fixtureNamed),
				whole(
					fixtureCrop,
				),
				whole(fixtureUserUnit),
				whole(fixtureLinks),
				whole(fixtureForm),
				whole(fixtureVersion20),
				whole(fixtureOutline),
				gen(0, 1),
				whole(fixturePlainB),
				pages(fixtureMulti, 2, 3),
				whole(fixtureActions),
			},
			20,
		},
	}
}

// TestAssembleMatrix assembles every scenario of the adoption matrix and has the independent oracle
// judge page order, text, link and destination identity per occurrence, form membership, geometry,
// version and orphans.
func TestAssembleMatrix(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	scenarios := slices.Concat(
		linkedScenarios(), geometryScenarios(), versionScenarios(), droppedScenarios(), orderScenarios(),
	)

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			c, doc := env.assemble(t, scenario.parts)

			requireClean(t, doc, c.expectation)
			requireMatrixOutput(t, doc, c.request.ExpectedPages, scenario.version)
		})
	}
}

// requireMatrixOutput checks the output properties every matrix scenario shares: version, page count, and
// a catalog and attachment set that keep nothing the policy drops.
func requireMatrixOutput(t *testing.T, doc *pdforacle.Document, pages int, input pdfengine.Version) {
	t.Helper()

	if got, want := doc.EffectiveVersion(), int(max(pdfengine.Version17, input)); got != want {
		t.Errorf("output version %d, want %d", got, want)
	}

	if got := doc.PageCount(); got != pages {
		t.Errorf("output has %d pages, want %d", got, pages)
	}

	allowed := []string{"/Type", "/Pages", "/Names", "/Dests", "/AcroForm", "/Version"}
	for _, key := range doc.CatalogKeys() {
		if !slices.Contains(allowed, key) {
			t.Errorf("catalog keeps %s, which the policy drops", key)
		}
	}

	if n := doc.Attachments(); n != 0 {
		t.Errorf("output has %d attachments, want none", n)
	}
}
