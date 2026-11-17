// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
)

// forwardAccuracyULPs is the supported output-coordinate precision, not a
// physical epsilon or a native-geometry verifier tolerance. 64 ulps reserve six
// low significand bits for constructing/evaluating a well-conditioned affine;
// cancellation must not consume the remaining target-coordinate precision.
const forwardAccuracyULPs = 64

// Nonnegative error-bound arithmetic rounds outward, including underflow.
func errorAdd(a, b float64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	return math.Nextafter(a+b, math.Inf(1))
}
func errorMul(a, b float64) float64 {
	if a == 0 || b == 0 {
		return 0
	}
	return math.Nextafter(a*b, math.Inf(1))
}
func observedDifference(a, b float64) float64 {
	if a == b {
		return 0
	}
	return math.Nextafter(math.Abs(a-b), math.Inf(1))
}

// accurateForwardFit bounds the represented map against centered placement over
// the ENTIRE visible rectangle. The difference between exact affine maps reaches
// its maximum at corners. Actual floating evaluation need not: its uniform
// gamma(4) bound uses max absolute input coordinates and all four dot-product
// operations (two multiplies/two adds). Thus matching corners alone cannot pass
// an ill-conditioned interior. Fused evaluation has no larger error.
func accurateForwardFit(box types.Rectangle, rotation int, target packet.Size, scale float64, m matrix.Matrix) bool {
	const u = 0x1p-53 // binary64 unit roundoff
	gamma4 := math.Nextafter(4*u/(1-4*u), math.Inf(1))
	gamma12 := math.Nextafter(12*u/(1-12*u), math.Inf(1))
	maxX := math.Max(math.Abs(box.LL.X), math.Abs(box.UR.X))
	maxY := math.Max(math.Abs(box.LL.Y), math.Abs(box.UR.Y))
	uniform := func(axis int) float64 {
		magnitude := errorAdd(errorAdd(errorMul(maxX, math.Abs(m[0][axis])), errorMul(maxY, math.Abs(m[1][axis]))), math.Abs(m[2][axis]))
		return errorAdd(errorMul(gamma4, magnitude), 4*math.SmallestNonzeroFloat64)
	}
	w, h := box.Width(), box.Height()
	corners := [4]types.Point{box.LL, {X: box.UR.X, Y: box.LL.Y}, box.UR, {X: box.LL.X, Y: box.UR.Y}}
	local := [4]types.Point{{}, {X: w}, {X: w, Y: h}, {Y: h}}
	switch (rotation%360 + 360) % 360 {
	case 90:
		local = [4]types.Point{{Y: w}, {}, {X: h}, {X: h, Y: w}}
		w, h = h, w
	case 180:
		local = [4]types.Point{{X: w, Y: h}, {Y: h}, {}, {X: w}}
	case 270:
		local = [4]types.Point{{X: h}, {X: h, Y: w}, {Y: w}, {}}
		w, h = h, w
	}
	offsetX, offsetY := (target.Width-scale*w)/2, (target.Height-scale*h)/2
	var residual [2]float64
	for i, p := range corners {
		got := m.Transform(p)
		wantX := math.FMA(local[i].X, scale, offsetX)
		wantY := math.FMA(local[i].Y, scale, offsetY)
		residual[0] = math.Max(residual[0], observedDifference(got.X, wantX))
		residual[1] = math.Max(residual[1], observedDifference(got.Y, wantY))
	}
	// The localized reference needs at most twelve rounded operations for native
	// extent, scale choice, centering and evaluation. Absolute terms also cover
	// gradual underflow in native subtraction and scale division; there is no
	// determinant/inverse or arbitrary source-coordinate bound.
	underflow := errorAdd(errorMul(8*math.SmallestNonzeroFloat64, math.Abs(scale)), errorMul(8*math.SmallestNonzeroFloat64, math.Max(w, h)))
	for axis, side := range [2]float64{target.Width, target.Height} {
		reference := errorAdd(errorMul(gamma12, side), underflow)
		total := errorAdd(residual[axis], errorAdd(errorMul(2, uniform(axis)), reference))
		budget := forwardAccuracyULPs * (math.Nextafter(side, math.Inf(1)) - side)
		if math.IsNaN(total) || math.IsInf(total, 0) || total > budget {
			return false
		}
	}
	return true
}
