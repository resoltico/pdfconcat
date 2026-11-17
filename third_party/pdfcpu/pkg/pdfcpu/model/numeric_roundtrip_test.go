// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis
package model

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"math"
	"strings"
	"testing"
)

func TestLargeFiniteIntegerShapedRealRoundTrip(t *testing.T) {
	for _, value := range []float64{1e20, -1e20, math.MaxFloat64, -math.MaxFloat64} {
		token := types.Float(value).PDFString()
		remaining := token
		object, err := parseNumericOrIndRef(&remaining)
		actual, ok := object.(types.Float)
		if err != nil || !ok || actual.Value() != value || remaining != "" {
			t.Errorf("%q decoded as %T %v: %v", token, object, object, err)
		}
	}
	overflow := strings.Repeat("9", 400)
	if _, err := parseNumericOrIndRef(&overflow); err == nil {
		t.Error("nonfinite integer-shaped token did not fail")
	}
	ordinary := "42"
	object, err := parseNumericOrIndRef(&ordinary)
	if err != nil || object != types.Integer(42) {
		t.Fatal("ordinary integer semantics changed", object, err)
	}
}
