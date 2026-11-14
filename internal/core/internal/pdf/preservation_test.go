// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func parsedFixture(t *testing.T, doc *pdffixture.Doc) *model.Context {
	t.Helper()
	conf, err := configuration()
	if err != nil {
		t.Fatal(err)
	}
	conf.ValidationMode = model.ValidationStrict
	s, err := api.ReadContext(t.Context(), bytes.NewReader(doc.Bytes()), conf)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPageContentWitnessDetectsReplacementAndOrdering(t *testing.T) {
	for _, change := range []string{"page order", "page removed", "stream removed", "stream replaced", "stream bytes", "stream order"} {
		t.Run(change, func(t *testing.T) {
			s := parsedFixture(t, pdffixture.NamedDests("preserve"))
			pages, err := rawPages(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			first, second := pages[0].dict["Contents"], pages[1].dict["Contents"]
			if change == "stream order" {
				pages[0].dict["Contents"] = types.Array{first, second}
			}
			before, err := capturePageContent(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "page order", "page removed":
				cat, e := s.Catalog()
				if e != nil {
					t.Fatal(e)
				}
				root, e := s.DereferenceDict(cat["Pages"])
				if e != nil {
					t.Fatal(e)
				}
				root["Kids"] = types.Array{pages[1].ref, pages[0].ref}
				if change == "page removed" {
					root["Kids"], root["Count"] = types.Array{pages[0].ref}, types.Integer(1)
				}
			case "stream removed":
				delete(pages[0].dict, "Contents")
			case "stream replaced":
				pages[0].dict["Contents"] = second
			case "stream order":
				pages[0].dict["Contents"] = types.Array{second, first}
			case "stream bytes":
				ref, ok := first.(types.IndirectRef)
				if !ok {
					t.Fatal("fixture content is not an indirect reference")
				}
				object, e := s.Dereference(ref)
				if e != nil {
					t.Fatal(e)
				}
				changed, ok := object.(types.StreamDict)
				if !ok {
					t.Fatal("fixture content is not a stream")
				}
				changed.Raw = bytes.Clone(changed.Raw)
				if len(changed.Raw) == 0 {
					t.Fatal("fixture no longer has encoded page content")
				}
				changed.Raw[0] ^= 1 // Keep its size: structural validation alone cannot detect this edit.
				s.Table[ref.ObjectNumber.Value()].Object = changed
			}
			if err := before.verify(t.Context(), s); err == nil || diagnostic(err, diag.SourceInvalid).Code != diag.SourceDamaged {
				t.Fatalf("page/content change was not refused: %v", err)
			}
		})
	}
}

func TestPageContentAdmissionRejectsMissingAndMalformedStreams(t *testing.T) {
	for _, contents := range []types.Object{
		*types.NewIndirectRef(999, 0), *types.NewIndirectRef(4, 1),
		*types.NewIndirectRef(0, 65535),
		types.Array{*types.NewIndirectRef(4, 0), *types.NewIndirectRef(999, 0)},
		types.Array{nil}, types.Integer(42), types.StringLiteral(""), types.Dict{},
	} {
		s := parsedFixture(t, pdffixture.Plain("content"))
		pages, err := rawPages(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		pages[0].dict["Contents"] = contents
		_, err = capturePageContent(t.Context(), s)
		if err == nil || diagnostic(err, diag.SourceInvalid).Code != diag.SourceDamaged {
			t.Fatalf("malformed/missing content was admitted: %T %v", contents, err)
		}
	}
	s := parsedFixture(t, pdffixture.Plain("free content"))
	s.Table[4].Free = true
	if _, err := capturePageContent(t.Context(), s); err == nil {
		t.Fatal("a free content-stream object was admitted")
	}
}

func TestPageContentPreservationAllowsBlankAndSharedStreams(t *testing.T) {
	for _, contents := range []types.Object{nil, types.Array{}, types.Array{*types.NewIndirectRef(4, 0)}, types.Array{*types.NewIndirectRef(4, 0), *types.NewIndirectRef(4, 0)}} {
		s := parsedFixture(t, pdffixture.Plain("content"))
		pages, err := rawPages(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		pages[0].dict["Contents"] = contents
		s.Configuration.ValidationMode = model.ValidationRelaxed
		s.XRefTable.ValidationMode = model.ValidationRelaxed
		if _, err = validatePreservingPages(t.Context(), s); err != nil {
			t.Fatalf("valid blank or shared streams refused: %v", err)
		}
	}
	s := parsedFixture(t, pdffixture.Plain("content"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := capturePageContent(ctx, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ref, err := s.IndRefForNewObject(nil)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	pages[0].dict["Contents"] = *ref
	if _, err := validatePreservingPages(t.Context(), s); err != nil {
		t.Fatalf("a live indirect null was not treated as a blank page: %v", err)
	}
}

func TestCompatibilityValidationMayNormalizeFormsWithoutChangingPageContent(t *testing.T) {
	s := parsedFixture(t, pdffixture.Form("form"))
	s.Configuration.ValidationMode = model.ValidationRelaxed
	s.XRefTable.ValidationMode = model.ValidationRelaxed
	if _, err := validatePreservingPages(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

func TestSourceParsingRejectsDuplicateKeysBeforeCompatibilityValidation(t *testing.T) {
	doc := pdffixture.Plain("duplicate font key")
	doc.Objs[4] = []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /BaseFont /Courier >>")
	data := doc.Bytes()
	// This control establishes that relaxed parsing can mask this defect.
	conf, err := configuration()
	if err != nil {
		t.Fatal(err)
	}
	relaxed, err := api.ReadContext(t.Context(), bytes.NewReader(data), conf)
	if err != nil {
		t.Fatalf("invalid negative-control setup: %v", err)
	}
	if err = api.ValidateContext(t.Context(), relaxed); err != nil {
		t.Fatalf("control did not isolate parsing: %v", err)
	}
	_, _, ds := Inspect(t.Context(), data, nil)
	if len(ds) != 1 || ds[0].Code != diag.SourceInvalid || ds[0].PDFLocation == nil || ds[0].PDFLocation.Object != 5 {
		t.Fatalf("strict parsing was bypassed: %+v", ds)
	}
}

func TestFieldAndOutlineWalksRespectObjectGenerations(t *testing.T) {
	s := parsedFixture(t, pdffixture.Form("fields"))
	cat, err := s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		t.Fatal(err)
	}
	roots, err := s.DereferenceArray(form["Fields"])
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := roots[0].(types.IndirectRef)
	if !ok {
		t.Fatal("fixture field root is not an indirect reference")
	}
	ref.GenerationNumber++
	form["Fields"] = types.Array{ref}
	if _, err = fields(t.Context(), s); err == nil {
		t.Fatal("form field resolved through a wrong generation")
	}
	s = parsedFixture(t, pdffixture.Outlined("outlines"))
	cat, err = s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.DereferenceDict(cat["Outlines"])
	if err != nil {
		t.Fatal(err)
	}
	ref, ok = root["First"].(types.IndirectRef)
	if !ok {
		t.Fatal("fixture first outline is not an indirect reference")
	}
	ref.GenerationNumber++
	root["First"] = ref
	if _, err = readOutlineForest(t.Context(), s); err == nil {
		t.Fatal("outline resolved through a wrong generation")
	}
}
