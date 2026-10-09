// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

const (
	geometryBoxCoordinates     = 4
	geometryMatrixCoefficients = 6
	geometryScalarFacts        = 6
	geometryNodes              = 1 + 3*(1+geometryBoxCoordinates) + (1 + geometryMatrixCoefficients) + geometryScalarFacts
	fitDeclarationNodes        = 7 // object, paper, size object+three scalars, nullable location.
	finalPlacementNodes        = 4 // object, nullable block/ink bounds, physical font size.
)

func (c *nodeCounter) fit(fit *FitDeclaration, geometries []Geometry) {
	if fit != nil {
		c.add(fitDeclarationNodes)

		if fit.Location != nil {
			position := fit.Location.position()
			c.position(&position)
			c.when(fit.Location.ArgvIndex != nil, fit.Location.Pointer != "")
		}
	}

	if len(geometries) > 0 {
		c.add(1 + geometryNodes*int64(len(geometries)))
	}
}
