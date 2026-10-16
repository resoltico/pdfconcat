// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdffixture_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// fixtureCase is a fixture with the page count and the text its last page must show.
type fixtureCase struct {
	doc   *pdffixture.Doc
	text  string
	pages int
}

// fixtureCases are normal valid constructors; refusal placeholders are separate.
func fixtureCases() map[string]fixtureCase {
	return map[string]fixtureCase{
		"URI":             {pdffixture.URILink("URI", "https://example.invalid/fixture", "", false), "URI", 1},
		"fit":             {pdffixture.FitMarks(""), "q Q /q /Q", 1},
		"fit streams":     {pdffixture.FitMarkStreams(), "q Q /q /Q", 1},
		"empty signature": {pdffixture.EmptySignatureField("UNSIGNED"), "UNSIGNED", 1},
		"plain":           {pdffixture.Plain("PLAIN"), "PLAIN", 1},
		"escaped marker":  {pdffixture.Plain(`A(B)C\D`), `A(B)C\D`, 1},
		"pages":           {pdffixture.Pages("PG", 3), "PG p3", 3},
		"links":           {pdffixture.Links("LK"), "LK p2", 2},
		"named":           {pdffixture.NamedDests("NM"), "NM p2", 2},
		"legacy":          {pdffixture.LegacyDests("LG"), "LG p2", 2},
		"form":            {pdffixture.Form("FM"), "FM p2", 2},
		"actions":         {pdffixture.PageActions("PA"), "PA p2", 2},
		"boxes inherited": {
			pdffixture.WithBoxes(
				"BX",
				pdffixture.PageBoxes{Media: "[10 20 310 420]", Rotate: "90"},
				pdffixture.PageBoxes{},
				pdffixture.PageBoxes{Crop: "[20 30 100 100]"},
			),
			"BX p2", 2,
		},
		"boxes own, no entries": {pdffixture.WithBoxes("BO", pdffixture.PageBoxes{Media: "[0 0 612 792]"}), "BO p1", 1},
		"boxes with spacing the marker placement does not parse": {
			pdffixture.WithBoxes("BU", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 612 792 ]", UserUnit: "2"}),
			"BU p1", 1,
		},
		"nested boxes":      {pdffixture.NestedBoxes("NB"), "NB p2", 2},
		"versioned header":  {pdffixture.Versioned("VH", "1.4", ""), "VH", 1},
		"versioned catalog": {pdffixture.Versioned("VC", "1.4", "1.6"), "VC", 1},
		"outlined":          {pdffixture.Outlined("OL"), "OL p2", 2},
		"tagged":            {pdffixture.Tagged("TG"), "TG", 1},
		"attachment":        {pdffixture.Attachment("AT"), "AT", 1},
		"titled":            {pdffixture.Titled("TT"), "TT", 1},
		"resource": {
			pdffixture.Resource(3, func(index int) string { return fmt.Sprintf("R%d", index) }), "R2", 3,
		},
		"image rich":      {pdffixture.ImageRich("IM", 4096), "IM", 1},
		"image rich tiny": {pdffixture.ImageRich("IT", 1), "IT", 1},
	}
}

// TestFixturesAreValidPDFsThatShowTheirMarkers checks the normal constructor table with qpdf and
// pdftotext (the marker each page must show), so the fixtures other tests rely on are themselves
// independently confirmed.
func TestFixturesAreValidPDFsThatShowTheirMarkers(t *testing.T) {
	t.Parallel()

	tools := oracle.RequireTools(t)

	for name, tc := range fixtureCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			requireValidFixture(t, tools, tc)
		})
	}
}

// requireValidFixture writes the fixture and requires qpdf to accept it and Poppler to read its page count
// and last-page marker.
func requireValidFixture(t *testing.T, tools oracle.Tools, tc fixtureCase) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.pdf")
	if err := tc.doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}

	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}

	if doc.PageCount() != tc.pages {
		t.Fatalf("%d pages, want %d", doc.PageCount(), tc.pages)
	}

	if got := doc.FirstLine(tc.pages); got != tc.text {
		t.Errorf("last page shows %q, want %q", got, tc.text)
	}

	if err = tools.Check(path); err != nil {
		t.Error(err)
	}
}

func TestFixtureSerializationDetails(t *testing.T) {
	t.Parallel()

	if got := pdffixture.Versioned("V", "2.0", "").Bytes(); !strings.HasPrefix(string(got), "%PDF-2.0\n") {
		t.Errorf("header %q", got[:12])
	}

	titled := string(pdffixture.Titled("T").Bytes())
	if !strings.Contains(titled, "/Info 6 0 R") || !strings.Contains(titled, "SOURCE TITLE") {
		t.Error("the info dictionary is not referenced from the trailer")
	}

	a := pdffixture.ImageRich("SAME", 2048).Bytes()
	b := pdffixture.ImageRich("SAME", 2048).Bytes()
	c := pdffixture.ImageRich("DIFFERENT", 2048).Bytes()

	if !bytes.Equal(a, b) || bytes.Equal(a, c) {
		t.Error("image payload must be deterministic per tag and differ between tags")
	}
}

func TestWriteFileReportsFailure(t *testing.T) {
	t.Parallel()

	err := pdffixture.Plain("P").WriteFile(filepath.Join(t.TempDir(), "no-such-directory", "x.pdf"))
	if err == nil {
		t.Fatal("no error writing into a missing directory")
	}
}

func TestImageConstructorPaintsItsResource(t *testing.T) {
	tools := oracle.RequireTools(t)
	painted := filepath.Join(t.TempDir(), "painted.pdf")
	plain := filepath.Join(t.TempDir(), "plain.pdf")
	if err := pdffixture.ImageRich("IMAGE", 4096).WriteFile(painted); err != nil {
		t.Fatal(err)
	}
	if err := pdffixture.Plain("IMAGE").WriteFile(plain); err != nil {
		t.Fatal(err)
	}
	actual, err := oracle.Load(tools, painted)
	if err != nil {
		t.Fatal(err)
	}
	blank, err := oracle.Load(tools, plain)
	if err != nil {
		t.Fatal(err)
	}
	difference, err := actual.AppearanceDifference(1, blank, 1, 5)
	if err != nil || difference < .01 {
		t.Fatalf("image is not independently painted: difference=%v %v", difference, err)
	}
	// Negative control: text and structure alone do not satisfy the image witness.
	difference, err = blank.AppearanceDifference(1, blank, 1, 5)
	if err != nil || difference != 0 {
		t.Fatalf("blank-image negative control: %v %v", difference, err)
	}
}
