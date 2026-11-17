// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
)

func TestForwardAccuracySupportsOrdinaryNonzeroOriginsAndRotation(t *testing.T) {
	target, err := packet.ParseSize("A4", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []float64{0, 10, -100, 1000} {
		for _, rotation := range []int{0, 90, 180, 270} {
			box := types.NewRectangle(origin, origin+20, origin+600, origin+820)
			m, scale := fitMatrix(*box, rotation, &target)
			if !accurateForwardFit(*box, rotation, target, scale, m) {
				t.Fatal("ordinary conditioned mapping refused", origin, rotation)
			}
		}
	}
}
func TestForwardAccuracyBoundsInteriorEvenWhenCornersMatch(t *testing.T) {
	box := types.NewRectangle(1e16, 1e16, 1e16+512, 1e16+512)
	target := packet.Size{Width: 384, Height: 384}
	m, scale := fitMatrix(*box, 0, &target)
	corners := [4]types.Point{box.LL, {X: box.UR.X, Y: box.LL.Y}, box.UR, {X: box.LL.X, Y: box.UR.Y}}
	wants := [4]types.Point{{}, {X: 384}, {X: 384, Y: 384}, {Y: 384}}
	for i, p := range corners {
		if got := m.Transform(p); got != wants[i] {
			t.Fatal("exact-corner control not established", got, wants[i])
		}
	}
	got := m.Transform(types.Point{X: 1e16 + 2, Y: 1e16 + 2})
	if got == (types.Point{X: 1.5, Y: 1.5}) {
		t.Fatal("interior rounding counterexample not established")
	}
	if accurateForwardFit(*box, 0, target, scale, m) {
		t.Fatal("corner-only accuracy check accepted a distorted interior")
	}
}
