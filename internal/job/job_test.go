// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package job_test

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/job"
)

func TestValueGrammars(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want float64
	}{{"1in", 72}, {"25.4mm", 72}, {"2.54cm", 72}, {".5", .5}, {"-14400pt", -14400}, {"14400", 14400}} {
		got, err := job.Length(tc.s)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("length %q = %v %v", tc.s, got, err)
		}
	}
	for _, s := range []string{"1e2", " 1", "1 ", "1.", "+1", "NaN", "14400.01", "-14400.01", "1m"} {
		if _, err := job.Length(s); err == nil {
			t.Errorf("accepted length %q", s)
		}
	}
	for _, s := range []string{"A3", "A4", "A5", "B4", "B5", "Letter", "Legal", "Tabloid", "1x14400pt", "297x210mm", "inherit"} {
		if _, err := job.ParseSize(s, true); err != nil {
			t.Errorf("size %q: %v", s, err)
		}
	}
	for _, s := range []string{"a4", "0x1", "1x14400.1", "A4P", "1 x 2", "inherit"} {
		if _, err := job.ParseSize(s, false); err == nil {
			t.Errorf("accepted size %q", s)
		}
	}
	for _, s := range []string{"#abc", "#ABCDEF", "#012345", "none"} {
		if _, err := job.ParseColor(s, true); err != nil {
			t.Errorf("color %q: %v", s, err)
		}
	}
	for _, s := range []string{"abc", "#abcd", "#ggg", "#12", "none"} {
		if _, err := job.ParseColor(s, false); err == nil {
			t.Errorf("accepted color %q", s)
		}
	}
	for _, s := range []string{"", "a\x00b"} {
		if _, err := job.Path(".", s); err == nil {
			t.Errorf("accepted path %q", s)
		}
	}
	path, err := job.Path(".", "a/link/../b.pdf")
	want, _ := filepath.Abs("a/b.pdf")
	if err != nil || path != want {
		t.Fatalf("lexical path %q %v", path, err)
	}
}

func TestResolveCanvasesAndRanges(t *testing.T) {
	a, b := job.Size{Width: 200, Height: 300}, job.Size{Width: 400, Height: 500}
	explicit := job.Defaults()
	explicit.Size = job.Size{Width: 123, Height: 456}
	inherited := job.Defaults()
	parts := []job.Part{{Pointer: "lead", Page: &inherited, Count: 2}, {Pointer: "source", Source: "a"}, {Pointer: "after", Page: &inherited, Count: 3}, {Pointer: "explicit", Page: &explicit, Count: 1}}
	facts := map[string]job.SourceFacts{"a": {Pages: 4, FirstSize: a, LastSize: b}}
	j := job.Job{Parts: parts}
	layout, ds := job.Resolve(j, facts)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if layout.SourcePages != 4 || layout.GeneratedPages != 6 || layout.TotalPages != 10 || layout.Parts[0].Size != a || layout.Parts[2].Size != b || layout.Parts[3].Size != explicit.Size {
		t.Fatalf("layout %+v", layout)
	}
	for i, want := range [][2]int64{{1, 2}, {3, 6}, {7, 9}, {10, 10}} {
		p := layout.Parts[i]
		if p.FirstPage != want[0] || p.LastPage != want[1] {
			t.Fatalf("part %d: %+v", i, p)
		}
	}
	fit := job.Size{Width: 612, Height: 792}
	j.FitTo = &fit
	layout, ds = job.Resolve(j, facts)
	if len(ds) > 0 || layout.Parts[0].Size != fit || layout.Parts[2].Size != fit || layout.Parts[3].Size != explicit.Size {
		t.Fatalf("fit %+v %+v", layout, ds)
	}
	_, ds = job.Resolve(job.Job{Parts: []job.Part{{Page: &inherited, Count: 1}}}, nil)
	if len(ds) != 1 || ds[0].Code != "page_size_required" {
		t.Fatal(ds)
	}
	facts["a"] = job.SourceFacts{Pages: 1, FirstSize: job.Size{Width: 14401, Height: 300}, LastSize: b}
	_, ds = job.Resolve(job.Job{Parts: parts}, facts)
	if len(ds) != 1 || ds[0].Code != "page_size_too_large" {
		t.Fatal(ds)
	}
}

func TestRangesUseInt64(t *testing.T) {
	layout, ds := job.Resolve(job.Job{Parts: []job.Part{{Source: "a"}, {Source: "a"}}}, map[string]job.SourceFacts{"a": {Pages: 2_000_000_000}})
	if len(ds) > 0 || layout.TotalPages != 4_000_000_000 || layout.Parts[1].FirstPage != 2_000_000_001 {
		t.Fatalf("ranges %+v %+v", layout, ds)
	}
}
