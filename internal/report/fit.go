// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"reflect"
	"slices"
)

type (
	// FitDeclaration identifies the effective global target and where it was declared.
	FitDeclaration struct {
		Location *Location `json:"location"`
		Paper    string    `json:"paper"`
		Size     PageSize  `json:"size"`
	}
	// Geometry is captured original effective geometry and its source-to-sheet transformation.
	// Target dimensions belong to the report's one global Fit declaration.
	Geometry struct {
		Media    [4]float64 `json:"media_box"`
		Crop     [4]float64 `json:"crop_box"`
		Visible  [4]float64 `json:"visible_box"`
		Matrix   [6]float64 `json:"matrix"`
		Width    float64    `json:"original_width"`
		Height   float64    `json:"original_height"`
		Scale    float64    `json:"physical_scale"`
		UserUnit float64    `json:"user_unit"`
		Rotation int        `json:"rotation"`
		HasCrop  bool       `json:"has_crop_box"`
	}
	// GeometryRange maps consecutive source pages to one shared geometry entry.
	GeometryRange struct {
		First    int `json:"first"`
		Last     int `json:"last"`
		Geometry int `json:"geometry"`
	}
	// GeometryEntry materializes one captured table row in a selected query.
	GeometryEntry struct {
		Geometry Geometry `json:"geometry"`
		Index    int      `json:"index"`
	}
	// TextPlacement captures final physical bounds and font size after canvas fitting.
	TextPlacement struct {
		Bounds    *Rect   `json:"bounds"`
		InkBounds *Rect   `json:"ink_bounds"`
		FontSize  float64 `json:"font_size"`
	}
)

func clonePlacement(placement *TextPlacement) *TextPlacement {
	captured := clonePointer(placement)
	if captured != nil {
		captured.Bounds = clonePointer(captured.Bounds)
		captured.InkBounds = clonePointer(captured.InkBounds)
	}

	return captured
}

func samePlacement(first, second *TextPlacement) bool {
	return reflect.DeepEqual(first, second)
}

// SetFit records the effective declaration without borrowing mutable caller locations.
func (b *Builder) SetFit(fit *FitDeclaration) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.report.Fit = clonePointer(fit)
	if b.report.Fit != nil {
		b.report.Fit.Location = cloneLocation(fit.Location)
	}
}

// Geometry interns captured scalar geometry so repeated styles and sources can refer to it.
func (b *Builder) Geometry(geometry Geometry) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return intern(b.geometryIndex, &b.report.Geometries, geometry)
}

func (b *Builder) capturedSources() []Source {
	sources := tableOf(b.sources, sourceKey.source)
	for index := range sources {
		sources[index].Geometries = slices.Clone(b.sourceGeometry[index])
	}

	return sources
}

func sameGeometryReference(first, second *int) bool {
	return first == nil && second == nil || first != nil && second != nil && *first == *second
}
