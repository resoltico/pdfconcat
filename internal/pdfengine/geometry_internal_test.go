// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	tenBox       = "[0 0 10 10]"
	multipleOf90 = "multiple of 90"
	userUnitKey  = "UserUnit"
)

// TestVisibleSizeRejectsWhatValidationAdmits reads documents without pdfcpu's validation, so the
// geometry rules themselves are tested rather than the validator in front of them.
func TestVisibleSizeRejectsWhatValidationAdmits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		boxes   pdffixture.PageBoxes
		wantErr string
	}{
		{"rotate 45", pdffixture.PageBoxes{Media: tenBox, Rotate: "45"}, multipleOf90},
		{"rotate fraction", pdffixture.PageBoxes{Media: tenBox, Rotate: "90.5"}, multipleOf90},
		{"rotate name", pdffixture.PageBoxes{Media: tenBox, Rotate: "/Left"}, "Rotate"},
		{"user unit zero", pdffixture.PageBoxes{Media: tenBox, UserUnit: "0"}, userUnitKey},
		{"user unit negative", pdffixture.PageBoxes{Media: tenBox, UserUnit: "-1"}, userUnitKey},
		{"user unit name", pdffixture.PageBoxes{Media: tenBox, UserUnit: "/Two"}, userUnitKey},
		{"media missing", pdffixture.PageBoxes{}, "MediaBox is missing"},
		{"media null", pdffixture.PageBoxes{Media: nullPDFObject}, "MediaBox is missing"},
		{"media short", pdffixture.PageBoxes{Media: "[0 0 10]"}, "want 4"},
		{"media text", pdffixture.PageBoxes{Media: "[0 0 (x) 10]"}, keyMediaBox},
		{"media not an array", pdffixture.PageBoxes{Media: "5"}, "MediaBox"},
		{"media empty", pdffixture.PageBoxes{Media: "[0 0 0 10]"}, "empty"},
		{"crop empty", pdffixture.PageBoxes{Media: tenBox, Crop: "[0 5 10 5]"}, "CropBox is empty"},
		{"crop disjoint", pdffixture.PageBoxes{Media: tenBox, Crop: "[20 20 30 30]"}, "does not overlap"},
		{"crop malformed", pdffixture.PageBoxes{Media: tenBox, Crop: "[0 0 10]"}, keyCropBox},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pdf := readUnvalidated(t, pdffixture.WithBoxes("P", pdffixture.PageBoxes{}, tc.boxes))

			root, err := pdf.Pages()
			if err != nil {
				t.Fatal(err)
			}

			var got error

			err = walkPages(context.Background(), pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
				_, got = visibleSize(pdf, page, inherited)

				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			if got == nil || !strings.Contains(got.Error(), tc.wantErr) {
				t.Fatalf(errorContainingFormat, got, tc.wantErr)
			}
		})
	}
}

// TestVisibleSizeRejectsValuesPDFSyntaxCannotExpress builds page dictionaries holding NaN, infinite and
// astronomically large numbers, which PDF text cannot spell but a directly constructed value can.
func TestVisibleSizeRejectsValuesPDFSyntaxCannotExpress(t *testing.T) {
	t.Parallel()

	nan, inf, huge := types.Float(math.NaN()), types.Float(math.Inf(1)), types.Float(math.MaxFloat64)
	box := func(a, b, c, d types.Object) types.Array { return types.Array{a, b, c, d} }

	cases := []struct {
		name    string
		patch   func(page types.Dict)
		wantErr string
	}{
		{"NaN corner", func(p types.Dict) { p["MediaBox"] = box(types.Integer(0), types.Integer(0), nan, types.Integer(5)) }, "non-finite"},
		{
			"infinite corner",
			func(p types.Dict) { p["MediaBox"] = box(types.Integer(0), types.Integer(0), types.Integer(5), inf) },
			"non-finite",
		},
		{"NaN user unit", func(p types.Dict) { p[userUnitKey] = nan }, userUnitKey},
		{"infinite user unit", func(p types.Dict) { p[userUnitKey] = inf }, userUnitKey},
		{"huge rotation", func(p types.Dict) { p["Rotate"] = huge }, multipleOf90},
		{"product overflows", func(p types.Dict) {
			p["MediaBox"] = box(types.Integer(0), types.Integer(0), huge, huge)
			p[userUnitKey] = types.Float(4)
		}, "overflows"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pdf := readUnvalidated(t, pdffixture.Plain("P"))

			root, err := pdf.Pages()
			if err != nil {
				t.Fatal(err)
			}

			var got error

			err = walkPages(context.Background(), pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
				tc.patch(page)
				_, got = visibleSize(pdf, page, inherited)

				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			if got == nil || !strings.Contains(got.Error(), tc.wantErr) {
				t.Fatalf(errorContainingFormat, got, tc.wantErr)
			}
		})
	}
}

// TestVisibleSizeTreatsAReferenceToNullAsAbsent covers a box that is an indirect reference to null.
func TestVisibleSizeTreatsAReferenceToNullAsAbsent(t *testing.T) {
	t.Parallel()

	doc := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte(singlePageTreeBody),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /CropBox 4 0 R >>"),
		[]byte(nullPDFObject),
	}}

	pdf := readUnvalidated(t, doc)

	root, err := pdf.Pages()
	if err != nil {
		t.Fatal(err)
	}

	err = walkPages(context.Background(), pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		got, sizeErr := visibleSize(pdf, page, inherited)
		if sizeErr != nil || got != (PageSize{Width: 10, Height: 10}) {
			t.Errorf("got %v, %v", got, sizeErr)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
