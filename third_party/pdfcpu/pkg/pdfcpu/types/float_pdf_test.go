// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis
package types

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestFloatPDFRoundTrip(t *testing.T) {
	for _, n := range []float64{1, -1, 1e20, -1e20, 0, math.Copysign(0, -1), 1e-13, math.SmallestNonzeroFloat64, math.MaxFloat64, -123.45678901234567, 595.2755905511812} {
		token := Float(n).PDFString()
		got, e := strconv.ParseFloat(token, 64)
		if e != nil || got != n || strings.ContainsAny(token, "eE") {
			t.Errorf("%g => %q => %g %v", n, token, got, e)
		}
		if n != 0 && !strings.ContainsRune(token, '.') {
			t.Errorf("real lost its decimal marker: %q", token)
		}
		if n == 0 && token != "0" {
			t.Errorf("noncanonical zero %q", token)
		}
	}
	if Integer(42).PDFString() != "42" {
		t.Fatal("integer spelling changed")
	}
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("nonfinite emitted")
				}
			}()
			Float(n).PDFString()
		}()
	}
}
