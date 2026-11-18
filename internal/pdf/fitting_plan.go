// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// nativeGeometry records numeric input values, not serializer token spelling.
// A missing CropBox has the MediaBox value. Signed zero compares numerically.
type nativeGeometry struct {
	Media, Crop [4]float64
	Rotate      int
	Unit        float64
	Size        packet.Size
}

func nativeOf(p pageGeometry) nativeGeometry {
	rect := func(r *types.Rectangle) [4]float64 { return [4]float64{r.LL.X, r.LL.Y, r.UR.X, r.UR.Y} }
	crop := p.attrs.CropBox
	if crop == nil {
		crop = p.attrs.MediaBox
	}
	return nativeGeometry{Media: rect(p.attrs.MediaBox), Crop: rect(crop), Rotate: p.attrs.Rotate, Unit: p.unit, Size: p.size}
}
func canvasGeometry(size packet.Size) nativeGeometry {
	box := [4]float64{0, 0, size.Width, size.Height}
	return nativeGeometry{Media: box, Crop: box, Unit: 1, Size: size}
}

type pageFitting struct {
	Original         int
	Native, Expected nativeGeometry
	Decision         packet.FitDecision
	Matrix           matrix.Matrix
	Scale            float64
}

// fittingPlan binds its actual count explicitly. Only declared generated
// repetitions may share one record; source selections never use this form.
type fittingPlan struct {
	Pages    []pageFitting
	Count    int
	Repeated bool
}

func (p fittingPlan) page(index int) pageFitting {
	if index < 0 || index >= p.Count {
		panic("unowned fitting page")
	}
	if p.Repeated {
		if len(p.Pages) != 1 {
			panic("invalid repeated fitting owner")
		}
		value := p.Pages[0]
		value.Original = index + 1
		return value
	}
	return p.Pages[index]
}
func planFitting(geometries []pageGeometry, policy *packet.FitPolicy, repeat int, origins sourceOrigins) (fittingPlan, error) {
	count := len(geometries)
	if repeat > 1 {
		if repeat != count {
			return fittingPlan{}, errors.New("generated repetition count does not own its geometry")
		}
		for _, p := range geometries {
			if nativeOf(p) != nativeOf(geometries[0]) {
				return fittingPlan{}, errors.New("generated repetition geometry differs")
			}
		}
		geometries = geometries[:1]
	}
	plans := make([]pageFitting, len(geometries))
	for i, p := range geometries {
		original, known := origins.pages[p.ref]
		if !known || original < 1 {
			return fittingPlan{}, refuse(diag.SourceProcessingFailed, diag.SourceOccurrenceGeometryIsInconsistent())
		}
		d, err := packet.DecideFit(p.size, policy)
		if err != nil {
			return fittingPlan{}, err
		}
		plan := pageFitting{Original: original, Native: nativeOf(p), Expected: nativeOf(p), Decision: d, Matrix: matrix.IdentMatrix, Scale: 1}
		if d.Transform {
			plan.Expected = canvasGeometry(d.Output)
			plan.Matrix, plan.Scale = fitMatrix(p.box, p.attrs.Rotate, &d.Output)
			usable := validPhysicalDimension(plan.Scale)
			for _, row := range plan.Matrix {
				for _, n := range row {
					usable = usable && !math.IsNaN(n) && !math.IsInf(n, 0)
				}
			}
			box := fitRectangle(p.box, plan.Matrix)
			usable = usable && validPhysicalDimension(box.Width()) && validPhysicalDimension(box.Height())
			if usable {
				usable = accurateForwardFit(p.box, p.attrs.Rotate, d.Output, plan.Scale, plan.Matrix)
			}
			if !usable {
				finding := diag.Report(diag.FitGeometryUnsupported, diag.FittingAffineCannotBeRepresented())
				finding.PDFLocation = &diag.PDFLocation{Page: original, Object: p.ref.ObjectNumber.Value(), Path: "Pages.EffectiveGeometry"}
				return fittingPlan{}, &policyError{finding: finding}
			}
		}
		plans[i] = plan
	}
	return fittingPlan{Pages: plans, Count: count, Repeated: repeat > 1}, nil
}
func fittingRuns(plans fittingPlan) []packet.PageSizeRun {
	var runs []packet.PageSizeRun
	for _, p := range plans.Pages {
		runs = packet.AppendPageSize(runs, p.Original, p.Decision)
	}
	return runs
}
func anyTransform(plans fittingPlan) bool {
	for _, p := range plans.Pages {
		if p.Decision.Transform {
			return true
		}
	}
	return false
}
func bindFitting(geometries []pageGeometry, plans fittingPlan) error {
	if len(geometries) != plans.Count {
		return errors.New("fitting plan page ownership does not match actual selected pages")
	}
	for i, p := range geometries {
		if nativeOf(p) != plans.page(i).Native {
			return errors.New("actual selected geometry does not match its admitted fitting plan")
		}
	}
	return nil
}
func fittingPrintWarnings(s *model.Context, pages []pageGeometry, plans fittingPlan) ([]diag.Diagnostic, error) {
	var ds []diag.Diagnostic
	for i, p := range pages {
		if plans.page(i).Decision.Transform {
			for _, key := range []string{"BleedBox", "TrimBox", "ArtBox"} {
				value, err := s.Dereference(p.dict[key])
				if err != nil {
					return nil, err
				}
				if value == nil {
					continue
				}
				d := diag.Report(diag.FitPrintBoxOmitted, diag.FittingPrintBoxOmission(key))
				d.PDFLocation = &diag.PDFLocation{Page: plans.page(i).Original, Object: p.ref.ObjectNumber.Value(), Path: key}
				ds = append(ds, d)
			}
		}
	}
	return ds, nil
}

// verifyPlannedGeometry observes independent current values before/after writing.
func verifyPlannedGeometry(c context.Context, s *model.Context, a Assembly) error {
	pages, err := pageGeometries(c, s)
	if err != nil {
		return err
	}
	offset := 0
	for _, part := range a.Parts {
		for i := range part.Pages {
			if offset >= len(pages) {
				return errors.New("output geometry page count is incomplete")
			}
			var expected nativeGeometry
			if part.Source != "" {
				if part.Occurrence == nil || part.Occurrence.Facts == nil || i >= part.Occurrence.Facts.fitting.Count {
					return errors.New("output source geometry ownership is unavailable")
				}
				expected = part.Occurrence.Facts.fitting.page(i).Expected
			} else {
				index := i
				if part.Repeat > 1 {
					index = 0
				}
				if index >= len(part.Generated) {
					return errors.New("generated geometry ownership is unavailable")
				}
				expected = canvasGeometry(part.OutputSize)
			}
			if nativeOf(pages[offset]) != expected {
				return errors.New("output native or displayed geometry differs from its resolved expectation")
			}
			offset++
		}
	}
	if offset != len(pages) {
		return errors.New("output geometry has unowned pages")
	}
	return nil
}

// fittingOwnsSelection checks exported occurrence facts against the private
// admitted owner. Equal geometry cannot substitute for original page identity.
func fittingOwnsSelection(f *OccurrenceFacts, policy *packet.FitPolicy, inputPages int) bool {
	if f == nil || f.Pages < 1 || f.fitting.Repeated || f.fitting.Count != f.Pages || len(f.fitting.Pages) != f.Pages || !sameFittingPolicy(policy, f.fittingPolicy) || len(f.Selection) == 0 {
		return false
	}
	if !slices.Equal(f.PageSizes, fittingRuns(f.fitting)) || f.FirstSize != f.fitting.Pages[0].Native.Size || f.LastSize != f.fitting.Pages[len(f.fitting.Pages)-1].Native.Size {
		return false
	}
	count, previous := 0, 0
	for _, span := range f.Selection {
		if span.First <= previous || span.Last < span.First || span.Last > inputPages || span.Last-span.First >= f.Pages-count {
			return false
		}
		count += span.Last - span.First + 1
		previous = span.Last
	}
	if count != f.Pages {
		return false
	}
	rangeIndex, original := 0, f.Selection[0].First
	for i, p := range f.fitting.Pages {
		if p.Original != original {
			return false
		}
		if i+1 == len(f.fitting.Pages) {
			break
		}
		if original == f.Selection[rangeIndex].Last {
			rangeIndex++
			original = f.Selection[rangeIndex].First
		} else {
			original++
		}
	}
	return true
}
