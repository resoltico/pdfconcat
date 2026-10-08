// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

type (
	size = pdfengine.PageSize

	// geometryCase is a document whose first and last pages must have the given visible sizes.
	geometryCase struct {
		doc         *pdffixture.Doc
		name        string
		first, last size
	}
)

const letterBox = "[0 0 612 792]"

func boxes(media, crop, rotate, units string) pdffixture.PageBoxes {
	return pdffixture.PageBoxes{Media: media, Crop: crop, Rotate: rotate, UserUnit: units}
}

// lastPage is a two-page document whose first page declares nothing and whose last page declares last;
// everything else is inherited from the page-tree root, which declares nothing.
func lastPage(last pdffixture.PageBoxes) *pdffixture.Doc {
	return pdffixture.WithBoxes("P", boxes("", "", "", ""), last)
}

// sameSize is a case whose first and last pages have the same visible size.
func sameSize(name string, doc *pdffixture.Doc, want size) geometryCase {
	return geometryCase{doc: doc, name: name, first: want, last: want}
}

// geometryCases are documents with the visible sizes of their first and last pages, computed by hand.
func geometryCases() []geometryCase {
	return []geometryCase{
		sameSize("letter", pdffixture.Plain("P"), size{612, 792}),
		sameSize("nonzero origin", lastPage(boxes(croppedPageBox, "", "", "")), size{300, 400}),
		sameSize("reversed corners", lastPage(boxes("[310 420 10 20]", "", "", "")), size{300, 400}),
		sameSize("page crop inside media", lastPage(boxes(letterBox, "[50 60 500 700]", "", "")), size{450, 640}),
		sameSize("crop at nonzero origin", lastPage(boxes("[100 100 400 500]", "[150 120 350 300]", "", "")), size{200, 180}),
		sameSize("crop partly outside media", lastPage(boxes("[0 0 200 200]", "[100 100 300 300]", "", "")), size{100, 100}),
		sameSize("crop larger than media", lastPage(boxes("[0 0 200 300]", "[-50 -50 500 500]", "", "")), size{200, 300}),
		{doc: pdffixture.NestedBoxes("P"), name: "inherited media crop rotate", first: size{640, 450}, last: size{390, 290}},
		sameSize(
			"inherited crop own media",
			pdffixture.WithBoxes("P", boxes("", "[10 10 110 60]", "", ""), boxes("[0 0 300 300]", "", "", "")),
			size{100, 50},
		),
		{
			doc:   pdffixture.WithBoxes("P", boxes(letterBox, "", "90", ""), boxes("", "", "", ""), boxes("", "", "0", "")),
			name:  "inherited rotate overridden",
			first: size{792, 612},
			last:  size{612, 792},
		},
		sameSize("rotate 90", lastPage(boxes(smallPageBox, "", "90", "")), size{400, 300}),
		sameSize("rotate 180", lastPage(boxes(smallPageBox, "", "180", "")), size{300, 400}),
		sameSize("rotate 270", lastPage(boxes(smallPageBox, "", "270", "")), size{400, 300}),
		sameSize("rotate negative", lastPage(boxes(smallPageBox, "", "-90", "")), size{400, 300}),
		sameSize("rotate over a full turn", lastPage(boxes(smallPageBox, "", "450", "")), size{400, 300}),
		sameSize("user unit", lastPage(boxes(smallPageBox, "", "", "2")), size{600, 800}),
		sameSize(
			"user unit crop rotate origin",
			lastPage(boxes(croppedPageBox, "[20 30 200 300]", "270", "2.5")),
			size{675, 450},
		),
		{
			doc: pdffixture.WithBoxes("P", boxes("", "", "", ""),
				boxes(letterBox, "", "", ""), boxes("[0 0 595 842]", "", "90", ""), boxes("[0 0 400 300]", "", "", "")),
			name:  "mixed orientations",
			first: size{612, 792},
			last:  size{400, 300},
		},
		{
			doc: pdffixture.WithBoxes("P", boxes("", "", "", ""),
				boxes("[0 0 400 300]", "", "", ""), boxes(letterBox, "", "270", ""), boxes(letterBox, "", "", "")),
			name:  "mixed first landscape last portrait",
			first: size{400, 300},
			last:  size{612, 792},
		},
	}
}

// TestInspectVisibleGeometry checks the visible size of first and last pages against sizes computed by
// hand and against Poppler's rendering (CropBox, rotation) times the page's UserUnit.
func TestInspectVisibleGeometry(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)
	tools := pdforacle.RequireTools(t)

	for _, tc := range geometryCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path, info := inspectDoc(t, engine, "geometry", tc.doc)

			if info.First != tc.first || info.Last != tc.last {
				t.Fatalf("first %v last %v, want %v and %v", info.First, info.Last, tc.first, tc.last)
			}

			doc, err := pdforacle.Load(tools, path)
			if err != nil {
				t.Fatal(err)
			}

			requireRenderedSize(t, doc, 1, info.First)
			requireRenderedSize(t, doc, info.Pages, info.Last)
		})
	}
}

// requireRenderedSize fails unless Poppler's rendering of page, scaled by its UserUnit, matches got.
func requireRenderedSize(t *testing.T, doc *pdforacle.Document, page int, got size) {
	t.Helper()

	width, height, err := doc.RenderedSize(page)
	if err != nil {
		t.Fatal(err)
	}

	units := doc.UserUnit(page)

	// Poppler rounds rasterized sizes up to whole pixels and ignores UserUnit.
	if d := float64(width)*units - got.Width; d < -1e-9 || d > units {
		t.Errorf("page %d width %v but Poppler renders %d x %v", page, got.Width, width, units)
	}

	if d := float64(height)*units - got.Height; d < -1e-9 || d > units {
		t.Errorf("page %d height %v but Poppler renders %d x %v", page, got.Height, height, units)
	}
}

func TestInspectReportsFacts(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		doc  *pdffixture.Doc
		want pdfengine.SourceInfo
	}{
		"ordinary document": {pdffixture.Plain("P"), pdfengine.SourceInfo{Pages: 1, Version: pdfengine.Version17}},
		"many pages":        {pdffixture.Pages("P", 7), pdfengine.SourceInfo{Pages: 7, Version: pdfengine.Version17}},
		fixtureLinks:        {pdffixture.Links("P"), pdfengine.SourceInfo{Pages: 2, Version: pdfengine.Version17, PageLocal: true}},
		"page actions":      {pdffixture.PageActions("P"), pdfengine.SourceInfo{Pages: 2, Version: pdfengine.Version17, PageLocal: true}},
		"named destinations": {
			pdffixture.NamedDests("P"),
			pdfengine.SourceInfo{Pages: 2, Version: pdfengine.Version17, PageLocal: true, NamedDests: true},
		},
		"legacy destinations": {
			pdffixture.LegacyDests("P"),
			pdfengine.SourceInfo{Pages: 2, Version: pdfengine.Version17, PageLocal: true, LegacyDests: true},
		},
		fixtureForm: {
			pdffixture.Form("P"),
			pdfengine.SourceInfo{Pages: 2, Version: pdfengine.Version17, PageLocal: true, AcroForm: true},
		},
		"header 1.4":           {pdffixture.Versioned("P", pdfVersion14, ""), pdfengine.SourceInfo{Pages: 1, Version: 14}},
		"catalog version wins": {pdffixture.Versioned("P", pdfVersion14, "1.6"), pdfengine.SourceInfo{Pages: 1, Version: 16}},
		"header 2.0": {
			pdffixture.Versioned("P", pdfVersion20, ""),
			pdfengine.SourceInfo{Pages: 1, Version: pdfengine.Version20},
		},
		"outlines are not page-local": {pdffixture.Outlined("P"), removedFeatureFacts(2, pdfengine.FeatureBookmarks)},
		"attachment name tree":        {pdffixture.Attachment("P"), removedFeatureFacts(1, pdfengine.FeatureCatalogAttachments)},
	}

	engine := newEngine(t)

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, got := inspectDoc(t, engine, "facts", tc.doc)

			want := tc.want
			want.First, want.Last = size{612, 792}, size{612, 792}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestSourceInfoImportPerOccurrence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		info pdfengine.SourceInfo
		want bool
	}{
		{pdfengine.SourceInfo{}, false},
		{pdfengine.SourceInfo{PageLocal: true}, true},
		{pdfengine.SourceInfo{AcroForm: true}, true},
		{pdfengine.SourceInfo{NamedDests: true}, true},
		{pdfengine.SourceInfo{LegacyDests: true}, true},
	} {
		if got := tc.info.ImportPerOccurrence(); got != tc.want {
			t.Errorf("%+v: got %t, want %t", tc.info, got, tc.want)
		}
	}
}

func TestVersionString(t *testing.T) {
	t.Parallel()

	if got := pdfengine.Version20.String(); got != pdfVersion20 {
		t.Errorf("got %q", got)
	}

	if got := pdfengine.Version(14).String(); got != pdfVersion14 {
		t.Errorf("got %q", got)
	}
}

// TestInspectRejectsMalformedGeometry covers boxes that pdfcpu's own validation rejects (reported as
// invalid documents) and boxes it accepts but whose visible size is undefined (reported as geometry
// errors). Rotation and UserUnit values that pdfcpu rejects are exercised on the geometry code directly
// in geometry_internal_test.go.
func TestInspectRejectsMalformedGeometry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  *pdffixture.Doc
		code pdfengine.Code
	}{
		{"media missing", pdffixture.WithBoxes("P", boxes("", "", "", "")), pdfengine.CodeInvalid},
		{"media three numbers", pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes("[0 0 612]", "", "", "")), pdfengine.CodeInvalid},
		{"media not numbers", pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes("[0 0 (a) 792]", "", "", "")), pdfengine.CodeInvalid},
		{"media empty", pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes("[0 0 0 792]", "", "", "")), pdfengine.CodePageGeometry},
		{
			"crop three numbers",
			pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes(letterBox, "[0 0 612]", "", "")),
			pdfengine.CodeInvalid,
		},
		{
			"crop empty",
			pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes(letterBox, "[10 10 10 50]", "", "")),
			pdfengine.CodePageGeometry,
		},
		{
			"crop outside media",
			pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes("[0 0 100 100]", "[200 200 300 300]", "", "")),
			pdfengine.CodePageGeometry,
		},
		{"rotate 45", pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes(letterBox, "", "45", "")), pdfengine.CodeInvalid},
		{"user unit zero", pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes(letterBox, "", "", "0")), pdfengine.CodeInvalid},
		{"user unit negative", pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes(letterBox, "", "", "-2")), pdfengine.CodeInvalid},
	}

	engine := newEngine(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeDoc(t, t.TempDir(), "bad", tc.doc)

			_, err := engine.Inspect(context.Background(), path)
			failure := requireFailure(t, err, tc.code)

			if failure.Path != path || failure.Source != pdfengine.NoSource {
				t.Errorf("error identifies %q source %d", failure.Path, failure.Source)
			}
		})
	}
}

func TestInspectRejectsGeometryOnLastPageOnly(t *testing.T) {
	t.Parallel()

	doc := pdffixture.WithBoxes("P", boxes("", "", "", ""),
		boxes(sourceLetterBox, "", "", ""), boxes(sourceLetterBox, "[10 10 10 50]", "", ""))

	path := writeDoc(t, t.TempDir(), "bad-last", doc)

	_, err := newEngine(t).Inspect(context.Background(), path)
	failure := requireFailure(t, err, pdfengine.CodePageGeometry)

	if got := failure.Error(); !strings.Contains(got, "last page") {
		t.Errorf("error does not say which page: %s", got)
	}
}

func TestInspectRejectsEncrypted(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)
	engine := newEngine(t)

	for name, password := range map[string]string{"empty user password": "", "user password": "open-sesame"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := writeDoc(t, t.TempDir(), "encrypted", pdffixture.Plain("SECRET"))
			encrypt(t, tools, path, password)

			_, err := engine.Inspect(context.Background(), path)
			failure := requireFailure(t, err, pdfengine.CodeEncrypted)

			if failure.Path != path {
				t.Errorf("error names %q", failure.Path)
			}
		})
	}
}

func TestInspectFileErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := newEngine(t)

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		_, err := engine.Inspect(context.Background(), filepath.Join(dir, absentFilename))
		requireCode(t, err, pdfengine.CodeUnreadable)
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		_, err := engine.Inspect(context.Background(), dir)
		requireCode(t, err, pdfengine.CodeUnreadable)
	})

	t.Run("not a PDF", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(dir, "text.pdf")
		if err := os.WriteFile(path, []byte("hello, this is not a PDF"), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := engine.Inspect(context.Background(), path)
		requireCode(t, err, pdfengine.CodeInvalid)
	})

	t.Run("empty file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(dir, "empty.pdf")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := engine.Inspect(context.Background(), path)
		requireCode(t, err, pdfengine.CodeInvalid)
	})

	t.Run("canceled", func(t *testing.T) {
		t.Parallel()

		path := writeDoc(t, dir, "ok", pdffixture.Plain("P"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := engine.Inspect(ctx, path)
		requireCode(t, err, pdfengine.CodeCanceled)

		if !isCanceled(err) {
			t.Errorf("%v does not wrap context.Canceled", err)
		}
	})
}

func TestInspectAcceptsFloatAndIndirectBoxes(t *testing.T) {
	t.Parallel()

	_, info := inspectDoc(t, newEngine(t), "floats",
		pdffixture.WithBoxes("P", boxes("", "", "", ""), boxes("[0.5 0.5 300.75 400.25]", "", "90", "1.5")))

	want := size{400.25 - 0.5, 300.75 - 0.5}
	want.Width *= 1.5
	want.Height *= 1.5

	if math.Abs(info.First.Width-want.Width) > 1e-9 || math.Abs(info.First.Height-want.Height) > 1e-9 {
		t.Fatalf("got %v, want %v", info.First, want)
	}
}

// TestInspectRejectsMalformedFieldTree refuses a hostile field reference before backend validation
// can repair it or panic. The exact source path and structured policy failure remain available.
func TestInspectRejectsMalformedFieldTree(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "hostile", "malformed-form-fields-panic.pdf")
	_, err := newEngine(t).Inspect(context.Background(), path)

	failure := requireFailure(t, err, pdfengine.CodeFormUnsupported)
	if failure.Path != path || !strings.Contains(failure.Error(), "missing field dictionary") {
		t.Fatalf("malformed field failure: %+v", failure)
	}
}

func removedFeatureFacts(pages int, kind pdfengine.FeatureKind) pdfengine.SourceInfo {
	return pdfengine.SourceInfo{
		Pages:    pages,
		Version:  pdfengine.Version17,
		Features: []pdfengine.SourceFeature{{Kind: kind, Disposition: pdfengine.FeatureRemoved}},
	}
}
