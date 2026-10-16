// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestAppearanceInspectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, subtype            string
		rect                     []int
		field, ap, linked, issue bool
	}{
		{name: "absent Type", subtype: "Text", rect: []int{0, 0, 10, 10}, issue: true},
		{name: "Link", subtype: "Link", rect: []int{0, 0, 10, 10}},
		{name: "Popup", subtype: "Popup", rect: []int{0, 0, 10, 10}},
		{name: "Popup Parent", subtype: "Popup", rect: []int{0, 0, 10, 10}, linked: true, issue: true},
		{name: "Projection", subtype: "Projection", rect: []int{0, 0, 10, 10}},
		{name: "both zero", subtype: "Text", rect: []int{0, 0, 0, 0}},
		{name: "width zero", subtype: "Text", rect: []int{0, 0, 0, 10}, issue: true},
		{name: "height zero", subtype: "Text", rect: []int{0, 0, 10, 0}, issue: true},
		{name: "indirect AP", subtype: "Text", rect: []int{0, 0, 10, 10}, ap: true},
		{name: "field only widget", subtype: "Widget", rect: []int{0, 0, 10, 10}, field: true, issue: true},
		{name: "field widget linked annotation", subtype: "Widget", rect: []int{0, 0, 10, 10}, field: true, ap: true, linked: true, issue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := read(t.Context(), pdffixture.Plain("appearance").Bytes())
			if err != nil {
				t.Fatal(err)
			}
			// Use PDF numeric objects; RectForArray intentionally does not accept Go ints.
			rect := types.Array{}
			for _, v := range tc.rect {
				rect = append(rect, types.Integer(v))
			}
			subtype, err := s.IndRefForNewObject(types.Name(tc.subtype))
			if err != nil {
				t.Fatal(err)
			}
			ann := types.Dict{"Subtype": *subtype, "Rect": rect}
			if tc.ap {
				ap, err := s.IndRefForNewObject(types.Dict{})
				if err != nil {
					t.Fatal(err)
				}
				ann["AP"] = *ap
			}
			if tc.linked {
				key := "IRT"
				if tc.subtype == "Popup" {
					key = "Parent"
				}
				ann[key] = types.Dict{"Subtype": types.Name("Text"), "Rect": rect}
			}
			cat, err := s.Catalog()
			if err != nil {
				t.Fatal(err)
			}
			if tc.field {
				cat["AcroForm"] = types.Dict{"Fields": types.Array{ann}}
			} else {
				pages, err := rawPages(t.Context(), s)
				if err != nil {
					t.Fatal(err)
				}
				pages[0].dict["Annots"] = types.Array{ann}
			}
			header, root := s.HeaderVersion, s.RootVersion
			issue, err := appearanceIssue(t.Context(), s)
			if err != nil || (issue != "") != tc.issue {
				t.Fatalf("issue %q err %v", issue, err)
			}
			if s.HeaderVersion != header || s.RootVersion != root || s.XRefTable.Version() != model.V17 {
				t.Fatal("version was not restored")
			}
			if err := s.CheckAnnotationAppearance(ann); err != nil {
				t.Fatalf("inspection changed source version behavior: %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = appearanceIssue(ctx, s)
			if !errors.Is(err, context.Canceled) || s.HeaderVersion != header || s.RootVersion != root {
				t.Fatalf("cancellation or restore: %v", err)
			}
			// A malformed linked rectangle is a traversal/check failure, never a missing-AP fact.
			ann["IRT"] = types.Dict{"Subtype": types.Name("Text"), "Rect": types.Array{types.Integer(0)}}
			_, err = appearanceIssue(t.Context(), s)
			if err == nil || s.HeaderVersion != header || s.RootVersion != root {
				t.Fatalf("error path or restore: %v", err)
			}
			delete(ann, "IRT")
			if tc.linked {
				key := "IRT"
				if tc.subtype == "Popup" {
					key = "Parent"
				}
				ann[key] = types.Dict{"Subtype": types.Name("Text"), "Rect": rect}
				if err := cleanCatalog(s, model.V20, false); err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				err = api.WriteContext(t.Context(), s, &output)
				if err == nil || !strings.Contains(err.Error(), "requires an appearance dictionary") {
					t.Fatalf("linked annotation real writer: %v", err)
				}
			}

		})
	}
}

func TestUpstreamWriterRequiresPDF20Appearances(t *testing.T) {
	s, err := read(t.Context(), pdffixture.Form("form").Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := api.WriteContext(t.Context(), s, &output); err != nil {
		t.Fatalf("PDF 1.7 control: %v", err)
	}
	s, err = read(t.Context(), pdffixture.Form("form").Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanCatalog(s, model.V20, false); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	err = api.WriteContext(t.Context(), s, &output)
	if err == nil || !strings.Contains(err.Error(), "requires an appearance dictionary") {
		t.Fatalf("did not exercise real writer failure: %v", err)
	}
}
