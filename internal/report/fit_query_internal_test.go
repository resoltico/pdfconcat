// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import "testing"

func TestCapturedSourcePageQueryIncludesEachGeometryRangeEndpoint(t *testing.T) {
	t.Parallel()
	saved := capturedFitReport()
	second, third := saved.Geometries[0], saved.Geometries[0]
	second.Scale, second.Matrix = 3.06, [6]float64{3.06, 0, 0, 3.06, 153, 198}
	third.Scale, third.Matrix = 2, [6]float64{2, 0, 0, 2, 206, 304}
	saved.Geometries = append(saved.Geometries, second, third)
	saved.Sources[0].Geometries = []GeometryRange{
		{First: 1, Last: 2, Geometry: 0},
		{First: 3, Last: 5, Geometry: 1},
		{First: 6, Last: 7, Geometry: 2},
	}
	saved.Parts[0].Pages = new(int64(7))
	saved.Parts[0].Range.End = 7
	saved.Counts.SourcePages, saved.Counts.TotalPages = new(int64(7)), new(int64(7))
	if err := saved.Validate(); err != nil {
		t.Fatalf("source geometry fixture is not a supported report: %v", err)
	}

	// Adjacent intervals make each page outside one interval belong to a
	// different captured geometry. Page 4 also exercises a strict interior.
	for index, wantGeometry := range []int{0, 0, 1, 1, 1, 2, 2} {
		page := int64(index + 1)
		response, err := saved.Query(Request{Page: &page})
		if err != nil {
			t.Fatalf("source page %d: %v", page, err)
		}
		selected, ok := ContentOf[PageResponse](response)
		if !ok || selected.Geometry == nil || *selected.Geometry != saved.Geometries[wantGeometry] {
			t.Fatalf("source page %d did not select geometry %d: %+v", page, wantGeometry, selected)
		}
		if selected.Page != page || selected.PageInPart != page || selected.Part.Style != nil {
			t.Fatalf("source page %d resolved through the wrong part or style: %+v", page, selected)
		}
		original := saved.Geometries[wantGeometry]
		selected.Geometry.Matrix[0] = -999
		if saved.Geometries[wantGeometry] != original {
			t.Fatalf("source page %d query borrowed the captured geometry", page)
		}
		again, err := saved.Query(Request{Page: &page, Details: true})
		if err != nil {
			t.Fatal(err)
		}
		detailed, ok := ContentOf[PageResponse](again)
		if !ok || detailed.Geometry == nil || *detailed.Geometry != original {
			t.Fatalf("source page %d query mutation leaked into another response: %+v", page, detailed)
		}
	}
	for _, page := range []int64{0, 8} {
		if _, err := saved.Query(Request{Page: &page}); err == nil {
			t.Fatalf("page %d outside the captured source layout was accepted", page)
		}
	}
}
