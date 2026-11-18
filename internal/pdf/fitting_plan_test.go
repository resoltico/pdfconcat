// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func fitToForTest(c context.Context, s *model.Context, target *packet.FitPolicy) error {
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return err
	}
	origins, err := sourceOriginsFor(c, s)
	if err != nil {
		return err
	}
	plans, err := planFitting(geometries, target, 1, origins)
	if err != nil {
		return err
	}
	return fitContext(c, s, plans, origins)
}

func TestFittingFactsBindPolicyNativeGeometryAndCompleteRuns(t *testing.T) {
	data := pdffixture.Plain("geometry owner").Bytes()
	policy := packet.FitPolicy{Target: packet.Size{Width: 612, Height: 792}, Tolerance: 1}
	facts, views, ds := Inspect(t.Context(), data, "", []SelectionRequest{{Item: "source", Fitting: &policy}})
	if diag.Outcome(ds) != "ok" || len(views) != 1 || views[0] == nil {
		t.Fatal(ds)
	}
	a := Assembly{Fitting: &policy, Parts: []Part{{Part: packet.Part{Leaf: packet.Leaf{Kind: packet.PartSource, Ref: "source", Source: "source.pdf"}, Pages: 1}, Occurrence: &Occurrence{File: &facts, Facts: views[0]}, Data: data}}}
	var output bytes.Buffer
	if _, err := Assemble(t.Context(), a, &output); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssembly(t.Context(), output.Bytes(), 1, a); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"origin", "unit", "within allowance"} {
		s, err := readOutput(t.Context(), output.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		pages, err := rawPages(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "origin":
			pages[0].dict["MediaBox"] = types.NewRectangle(1, 0, 613, 792).Array()
			pages[0].dict["CropBox"] = types.NewRectangle(1, 0, 613, 792).Array()
		case "unit":
			pages[0].dict["MediaBox"] = types.NewRectangle(0, 0, 306, 396).Array()
			pages[0].dict["CropBox"] = types.NewRectangle(0, 0, 306, 396).Array()
			pages[0].dict["UserUnit"] = types.Float(2)
		case "within allowance":
			pages[0].dict["MediaBox"] = types.NewRectangle(0, 0, 612.5, 792).Array()
			pages[0].dict["CropBox"] = types.NewRectangle(0, 0, 612.5, 792).Array()
		}
		// The first two changes keep displayed dimensions; the last changes
		// width within the match allowance. All must fail native verification.
		if err = verifyPlannedGeometry(t.Context(), s, a); err == nil {
			t.Fatal("pre-write native change passed", kind)
		}
		var changed bytes.Buffer
		if err = serialize(t.Context(), s, &changed); err != nil {
			t.Fatal(err)
		}
		if err = VerifyAssembly(t.Context(), changed.Bytes(), 1, a); err == nil {
			t.Fatal("reopened native change passed", kind)
		}
	}
	original := views[0].PageSizes[0]
	views[0].PageSizes[0].OutputSize.Width++
	if diag.Outcome(CheckCompatibility(a).Diagnostics) != "failed" {
		t.Fatal("inconsistent run accepted")
	}
	views[0].PageSizes[0] = original
	for _, changed := range []*packet.FitPolicy{nil, packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400}), {Target: policy.Target, Tolerance: 0}} {
		a.Fitting = changed
		if diag.Outcome(CheckCompatibility(a).Diagnostics) != "failed" {
			t.Fatalf("changed policy accepted stale decisions: %+v", changed)
		}
		var refused bytes.Buffer
		if _, err := Assemble(t.Context(), a, &refused); err == nil || refused.Len() != 0 {
			t.Fatalf("assembly accepted stale decisions: %+v, %v", changed, err)
		}
	}
	policy.Tolerance = 0
	a.Fitting = &policy
	if diag.Outcome(CheckCompatibility(a).Diagnostics) != "failed" {
		t.Fatal("inspection retained the caller's mutable policy")
	}
}
func TestRepeatedGeneratedFittingPlansStayCompact(t *testing.T) {
	box := types.NewRectangle(0, 0, 100, 200)
	p := pageGeometry{box: *box, attrs: model.InheritedPageAttrs{MediaBox: box}, unit: 1, size: packet.Size{Width: 100, Height: 200}}
	pages := make([]pageGeometry, 100000)
	for i := range pages {
		pages[i] = p
	}
	plan, err := planFitting(pages, packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400}), len(pages), sourceOrigins{pages: map[types.IndirectRef]int{p.ref: 1}})
	if err != nil || plan.Count != 100000 || len(plan.Pages) != 1 || !plan.Repeated {
		t.Fatal("expanded repeated plan", len(plan.Pages), err)
	}
	if err = bindFitting(pages, plan); err != nil {
		t.Fatal(err)
	}
	pages[len(pages)-1].unit = 2
	if err = bindFitting(pages, plan); err == nil {
		t.Fatal("last repeated geometry mismatch was missed")
	}
}
func TestUnrepresentableFittingRefusesBeforeMutation(t *testing.T) {
	box := types.NewRectangle(0, 0, 1e-306, 1e-306)
	page := pageGeometry{box: *box, attrs: model.InheritedPageAttrs{MediaBox: box}, unit: 1e308, size: packet.Size{Width: 100, Height: 100}}
	_, err := planFitting([]pageGeometry{page}, packet.DefaultFitPolicy(packet.Size{Width: 14400, Height: 14400}), 1, sourceOrigins{pages: map[types.IndirectRef]int{page.ref: 7}})
	if err == nil || Diagnostic(err).Code != diag.FitGeometryUnsupported {
		t.Fatal("actual overflowing scale accepted", err)
	}

}

func TestSerializedFiniteModelScalarsReopenExactly(t *testing.T) {
	for _, n := range []float64{math.SmallestNonzeroFloat64, 1e-13, math.MaxFloat64, -math.MaxFloat64, math.Copysign(0, -1)} {
		s, _, _, err := readSource(t.Context(), pdffixture.Plain("numeric").Bytes(), "")
		if err != nil {
			t.Fatal(err)
		}
		cat, err := s.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		cat["NumericWitness"] = types.Float(n)
		var output bytes.Buffer
		if err = serialize(t.Context(), s, &output); err != nil {
			t.Fatal(err)
		}
		conf, e := configuration()
		if e != nil {
			t.Fatal(e)
		}
		conf.ValidationMode = model.ValidationStrict
		reopened, err := api.ReadContext(t.Context(), bytes.NewReader(output.Bytes()), conf)
		if err != nil {
			t.Fatalf("%g: %v", n, err)
		}
		cat, err = reopened.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		got, err := reopened.DereferenceNumber(cat["NumericWitness"])
		if err != nil || got != n {
			t.Fatalf("%g => %g %v", n, got, err)
		}
	}
}

func TestMixedFittingPreservesSharedStructureClassOnKeptPages(t *testing.T) {
	d := taggedSelectionFixture()
	d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/MediaBox [0 0 600 800]"), []byte("/MediaBox [0 0 1200 1600]"))
	s, origins, _, err := readSource(t.Context(), d.Bytes(), "")
	if err != nil {
		t.Fatal(err)
	}
	geometries, err := pageGeometries(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planFitting(geometries, packet.DefaultFitPolicy(packet.Size{Width: 600, Height: 800}), 1, origins)
	if err != nil {
		t.Fatal(err)
	}
	if err = fitContext(t.Context(), s, plan, origins); err != nil {
		t.Fatal(err)
	}
	root, err := structureRoot(s)
	if err != nil {
		t.Fatal(err)
	}
	classes, err := s.DereferenceDict(root["ClassMap"])
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	err = structureElements(t.Context(), s, root["K"], nil, func(d types.Dict, page types.Object) error {
		if d["C"] == nil {
			return nil
		}
		seen++
		_, e := mapAttributeClasses(s, d["C"], func(name types.Name) (types.Object, error) {
			attrs, e := s.DereferenceDict(classes[string(name)])
			if e != nil {
				return nil, e
			}
			box, e := s.DereferenceArray(attrs["BBox"])
			if e != nil {
				return nil, e
			}
			want := [4]float64{10, 20, 100, 120}
			if sameRef(page, geometries[1].ref) {
				want = [4]float64{5, 10, 50, 60}
			}
			for i, v := range box {
				actual, e := s.DereferenceNumber(v)
				if e != nil {
					return nil, e
				}
				if actual != want[i] {
					return nil, fmt.Errorf("shared class changed: %v", box)
				}
			}
			return name, nil
		}, 0)
		return e
	}, 0)
	if err != nil || seen != 3 {
		t.Fatal(seen, err)
	}
}

func TestSerializerRejectsNonfiniteScalarsBeforeWriting(t *testing.T) {
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		s, _, _, err := readSource(t.Context(), pdffixture.Plain("numeric").Bytes(), "")
		if err != nil {
			t.Fatal(err)
		}
		cat, err := s.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		cat["NumericWitness"] = types.Float(n)
		var output bytes.Buffer
		if err = serialize(t.Context(), s, &output); err == nil || output.Len() != 0 {
			t.Fatal("nonfinite scalar reached output", err, output.Len())
		}
	}
}

func TestChangedPreparedSelectionCannotReuseSameSizedPageOwner(t *testing.T) {
	d := pdffixture.NavigationPages("identity", true, false)
	d.Objs[3] = bytes.ReplaceAll(d.Objs[3], []byte(" /Annots [8 0 R]"), nil)
	warningEntry(d, 6, "/Annots [8 0 R]")
	d.Objs[7] = bytes.ReplaceAll(d.Objs[7], []byte("/P 4 0 R"), []byte("/P 6 0 R"))
	target := packet.Size{Width: 300, Height: 400}
	facts, views, ds := Inspect(t.Context(), d.Bytes(), "", []SelectionRequest{{Item: "source", Pages: []packet.PageRange{{First: 1, Last: 1}}, Fitting: packet.DefaultFitPolicy(target)}})
	if diag.Outcome(ds) != "ok" || views[0] == nil {
		t.Fatal(ds)
	}
	// The frames match exactly, but page two has a retained numeric self-target.
	// It must never inherit page one's earlier coordinate/ownership admission.
	views[0].Selection[0] = packet.PageRange{First: 2, Last: 2}
	a := Assembly{Fitting: packet.DefaultFitPolicy(target), Parts: []Part{{Part: packet.Part{Leaf: packet.Leaf{Kind: packet.PartSource, Ref: "source", Source: "source.pdf"}, Pages: 1, FirstPage: 1, LastPage: 1}, Occurrence: &Occurrence{File: &facts, Facts: views[0]}, Data: d.Bytes()}}}
	var output bytes.Buffer
	_, err := Assemble(t.Context(), a, &output)
	if err == nil || Diagnostic(err).Code != diag.SourceProcessingFailed || output.Len() != 0 {
		t.Fatalf("changed selection reused stale facts: %v, %d bytes", err, output.Len())
	}
}
