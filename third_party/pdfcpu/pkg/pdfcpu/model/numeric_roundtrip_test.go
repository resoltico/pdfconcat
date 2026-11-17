// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis
package model

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"math"
	"strings"
	"testing"
)

func TestRealLexicalRoundTripAndIntegerRangeRefusal(t *testing.T) {
	for _, value := range []float64{1e20, -1e20, math.MaxFloat64, -math.MaxFloat64} {
		token := types.Float(value).PDFString()
		remaining := token
		object, err := parseNumericOrIndRef(&remaining)
		actual, ok := object.(types.Float)
		if err != nil || !ok || actual.Value() != value || remaining != "" {
			t.Errorf("%q decoded as %T %v: %v", token, object, object, err)
		}
	}
	for _, token := range []string{strings.Repeat("9", 400), strings.Repeat("9", 400) + ".0", "100000000000000000000", "-100000000000000000000"} {
		remaining := token
		if value, err := parseNumericOrIndRef(&remaining); err == nil || value != nil {
			t.Errorf("out-of-range integer became a plausible result: %q %v %v", token, value, err)
		}
	}

	ordinary := "42"
	object, err := parseNumericOrIndRef(&ordinary)
	if err != nil || object != types.Integer(42) {
		t.Fatal("ordinary integer semantics changed", object, err)
	}
}
