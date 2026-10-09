// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
)

func (p *pipeline) resolveFit() error {
	target := p.job.FitTo.Value
	if p.command.FitTo != "" {
		target = p.command.FitTo
	}

	if target == "" {
		return nil
	}

	dim, err := target.Dim()
	if err != nil {
		return p.stop(stageLayout, err)
	}

	p.target = &pdfengine.PageSize{Width: float64(dim.Width), Height: float64(dim.Height)}
	p.builder.SetFit(&report.FitDeclaration{
		Paper: string(target), Location: p.fitLocation(),
		Size: report.PageSize{Origin: report.SizeFitTarget, Width: p.target.Width, Height: p.target.Height},
	})

	return nil
}

func (p *pipeline) fitLocation() *report.Location {
	if p.command.FitTo != "" {
		return argumentLocation(p.env, "--fit-to")
	}

	return report.LocationOf(assembly.Locate(p.job.Source, p.job.FitTo.Origin, "/fit_to"))
}

func (p *pipeline) captureGeneratedFits() error {
	if p.target == nil {
		return nil
	}

	p.generatedFits = make([]pdfengine.PageFit, len(p.layout.Specs))
	for index := range p.layout.Specs {
		dim := p.layout.Specs[index].Spec.Dim

		fit, err := pdfengine.CanvasFit(pdfengine.PageSize{Width: float64(dim.Width), Height: float64(dim.Height)}, *p.target)
		if err != nil {
			return p.stop(stageLayout, err)
		}

		p.generatedFits[index] = fit
	}

	return nil
}

func capturedReportGeometry(fit pdfengine.PageFit) report.Geometry {
	return report.Geometry{
		Media:    fit.Media,
		Crop:     fit.Crop,
		Visible:  fit.Visible,
		Matrix:   [6]float64(fit.Matrix),
		Width:    fit.Original.Width,
		Height:   fit.Original.Height,
		Scale:    fit.Scale,
		UserUnit: fit.UserUnit,
		Rotation: fit.Rotation,
		HasCrop:  fit.HasCrop,
	}
}

func (p *pipeline) fitStyle(style *report.Style, spec int) {
	if p.target == nil {
		return
	}

	fit := p.generatedFits[spec]
	geometry := p.builder.Geometry(capturedReportGeometry(fit))

	style.Geometry = &geometry
	if style.Text == nil {
		return
	}

	style.FinalText = &report.TextPlacement{
		FontSize: style.Text.Size * fit.Scale,
		Bounds:   projectedReportRect(style.Text.Bounds, fit.Matrix), InkBounds: visibleReportInk(style.Text.InkBounds, fit),
	}
}

func projectedReportRect(rect *report.Rect, matrix pdfengine.Affine) *report.Rect {
	if rect == nil {
		return nil
	}

	bounds := matrix.Bounds([4]float64{rect.X, rect.Y, rect.X + rect.Width, rect.Y + rect.Height})

	return &report.Rect{X: bounds[0], Y: bounds[1], Width: bounds[2] - bounds[0], Height: bounds[3] - bounds[1]}
}

func visibleReportInk(rect *report.Rect, fit pdfengine.PageFit) *report.Rect {
	if rect == nil {
		return nil
	}

	bounds, visible := fit.ClippedBounds([4]float64{rect.X, rect.Y, rect.X + rect.Width, rect.Y + rect.Height})
	if !visible {
		return nil
	}

	return &report.Rect{X: bounds[0], Y: bounds[1], Width: bounds[2] - bounds[0], Height: bounds[3] - bounds[1]}
}
