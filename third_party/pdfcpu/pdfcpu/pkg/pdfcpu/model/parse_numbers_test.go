package model

import (
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFiniteRealNativeRoundTripPreservesTypeAndBits(t *testing.T) {
	for _, value := range []float64{0, math.Copysign(0, -1), 1, -1, 1e-13, math.SmallestNonzeroFloat64, 1e20, math.MaxFloat64} {
		token := types.Float(value).PDFString()
		if strings.ContainsAny(token, "eE") || !strings.Contains(token, ".") {
			t.Fatalf("not a PDF real token: %q", token)
		}
		for _, mode := range []int{ValidationStrict, ValidationRelaxed} {
			remaining := token
			result, err := ParseObjectWithPolicy(t.Context(), &remaining, 0, mode)
			parsed, ok := result.Object.(types.Float)
			if err != nil || !ok || math.Float64bits(float64(parsed)) != math.Float64bits(value) {
				t.Fatalf("value=%g token=%s mode=%d: %T %v %v", value, token, mode, result.Object, result.Object, err)
			}
		}
	}
}

func TestNumericInstructionsNeverBecomeFabricatedZeroOrAbsentValue(t *testing.T) {
	for _, token := range []string{"100000000000000000000", "-100000000000000000000", "1e20", "1,5", ".-5", "0+5", "0.00-5", "1.2.3", ".", "+", "0." + strings.Repeat("0", 400) + "1", strings.Repeat("9", 400) + ".0"} {
		for _, mode := range []int{ValidationStrict, ValidationRelaxed} {
			remaining := "<< /Value " + token + " >>"
			result, err := ParseObjectWithPolicy(t.Context(), &remaining, 0, mode)
			if err == nil {
				t.Fatalf("invalid numeric instruction admitted: token=%q mode=%d object=%v", token, mode, result.Object)
			}
		}
	}
	remaining := "1 0%comment\nR"
	if _, err := ParseObject(t.Context(), &remaining, 0); err != nil {
		t.Fatalf("valid indirect reference rejected: %v", err)
	}
}
