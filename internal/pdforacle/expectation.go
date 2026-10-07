// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdforacle

type (
	// Geometry is the page attributes an output page must have in effect.
	Geometry struct {
		// MediaBox is the effective MediaBox.
		MediaBox []float64
		// CropBox is the effective CropBox; nil requires that none is in effect.
		CropBox []float64
		// Rotate is the effective rotation in [0, 360).
		Rotate int
		// UserUnit is the page's UserUnit; 0 means the default 1.
		UserUnit float64
	}

	// SourceFact is what the test knows about a source document independently of the engine.
	SourceFact struct {
		// Links maps a source page to the source pages its link annotations and actions must reach.
		Links map[int][]int
		// Geometry maps a source page to its expected attributes; absent pages are not checked.
		Geometry map[int]Geometry
		// Pages is the page count.
		Pages int
		// Version is the effective version as major*10+minor.
		Version int
	}

	// ExpectedPage is one output page.
	ExpectedPage struct {
		// Geometry, when set, is the expected geometry (used for generated pages).
		Geometry *Geometry
		// Text is the first text line the page must show.
		Text string
		// Source names the source in Expectation.Sources, or "" for a generated page.
		Source string
		// Occurrence numbers the occurrence of the source, from 1, so links must stay within it.
		Occurrence int
		// SourcePage is the 1-based source page.
		SourcePage int
	}

	// Expectation is everything Verify checks the output against.
	Expectation struct {
		// Sources holds the facts of each named source.
		Sources map[string]SourceFact
		// Pages lists the output pages in order.
		Pages []ExpectedPage
	}

	// Finding is one failed check.
	Finding struct {
		// Check is one of the Check constants.
		Check string
		// Detail says what was wrong.
		Detail string
	}
)

// Names of the checks Verify runs; Finding.Check holds one of them.
const (
	CheckStructure         = "qpdf-check"
	CheckPageCount         = "page-count"
	CheckPageText          = "page-text-order"
	CheckDistinctPages     = "distinct-page-dicts"
	CheckAnnotMembership   = "annot-membership-per-page"
	CheckAnnotParent       = "annot-P-is-own-page"
	CheckLinkDangling      = "link-dest-not-dangling"
	CheckLinkInTree        = "link-dest-in-output-page-tree"
	CheckLinkOccurrence    = "link-dest-same-occurrence"
	CheckLinkPage          = "link-dest-right-source-page"
	CheckNamedDests        = "named-dests-reach-output-pages"
	CheckFormRoots         = "form-widgets-reach-AcroForm-Fields"
	CheckFormParents       = "form-parent-refs-resolve"
	CheckFormWidgets       = "form-fields-have-page-widgets"
	CheckFormWidgetsShared = "form-widget-objects-not-shared"
	CheckGeometry          = "geometry-effective-attrs"
	CheckVersion           = "output-version>=max-input"
	CheckOutlines          = "outline-dests-reach-output-pages"
	CheckOrphanPages       = "no-orphan-page-objects"
)

// Failed returns the names of the checks that failed.
func Failed(findings []Finding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Check)
	}

	return names
}
