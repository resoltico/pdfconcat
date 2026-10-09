// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

func (r *Report) pageGeometry(part *Part, sourcePage int) *Geometry {
	if part.Style != nil {
		if geometry := r.Styles[*part.Style].Geometry; geometry != nil {
			return clonePointer(&r.Geometries[*geometry])
		}
	}

	if part.Source != nil {
		for _, interval := range r.Sources[*part.Source].Geometries {
			if sourcePage >= interval.First && sourcePage <= interval.Last {
				return clonePointer(&r.Geometries[interval.Geometry])
			}
		}
	}

	return nil
}

func (r *Report) sourceGeometryEntries(source *Source) []GeometryEntry {
	var entries []GeometryEntry

	seen := make(map[int]bool)
	for _, interval := range source.Geometries {
		if !seen[interval.Geometry] {
			seen[interval.Geometry] = true
			entries = append(entries, GeometryEntry{Index: interval.Geometry, Geometry: r.Geometries[interval.Geometry]})
		}
	}

	return entries
}
