// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/resoltico/pdfconcat/internal/plan"
)

func TestExactInt64Spellings(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]int64{
		"0": 0, "-0": 0, "0.0": 0, "0e999999999999": 0, "0.000e-5": 0,
		"1": 1, "1.0": 1, "1e0": 1, "10e-1": 1, "0.1E1": 1, "1E+2": 100, "100e-2": 1, "12.50e1": 125,
		"9223372036854775807": math.MaxInt64, "-9223372036854775808": math.MinInt64,
		"9.223372036854775807e18": math.MaxInt64, "9223372036854775807000e-3": math.MaxInt64,
		"-1": -1, "1000000": 1000000,
	} {
		got, err := plan.ParseExactInt(text)
		if err != nil || got != want {
			t.Errorf("%s = %d, %v; want %d", text, got, err, want)
		}
	}
}

func TestExactInt64Rejections(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]plan.IntegerFailure{
		"1.5": plan.NotIntegral, "1e-1": plan.NotIntegral, "0.1": plan.NotIntegral, "1.00000000000000000000000001": plan.NotIntegral,
		"-1e-400":             plan.NotIntegral,
		"9223372036854775808": plan.OutOfRange, "-9223372036854775809": plan.OutOfRange, "1e400": plan.OutOfRange,
		"1e19": plan.OutOfRange, "92233720368547758080e-1": plan.OutOfRange, "1e99999999999999999999": plan.OutOfRange,
		"100000000000000000000": plan.OutOfRange, "18446744073709551616e0": plan.OutOfRange, "9999999999999999999": plan.OutOfRange,
	} {
		if got := plan.ClassifyInteger(text); got != want {
			t.Errorf("%s: %v, want %v", text, got, want)
		}
	}
}

// numberSpellings lists JSON numbers built from the boundaries that matter: zero, one, powers of ten, the
// int64 and uint64 limits and their neighbors, every fraction shape, and exponents on both sides of zero.
func numberSpellings() []string {
	integers := []string{
		"0", "1", "9", "10", "123", "9223372036854775807", "9223372036854775808", "18446744073709551615", "18446744073709551616",
		"99999999999999999999", "100000000000000000000",
	}
	fractions := []string{"", ".0", ".5", ".50", ".000", ".0001", ".00000000000000000000001"}
	exponents := []string{"", "e0", "e1", "e-1", "e18", "e19", "e20", "e-19", "E+2", "e-400", "e400"}

	spellings := make([]string, 0, 2*len(integers)*len(fractions)*len(exponents))

	for _, sign := range []string{"", "-"} {
		for _, integer := range integers {
			for _, fraction := range fractions {
				for _, exponent := range exponents {
					spellings = append(spellings, sign+integer+fraction+exponent)
				}
			}
		}
	}

	return spellings
}

// TestExactInt64AgainstBigRational compares with arbitrary-precision arithmetic, an oracle that shares no logic
// with the conversion.
func TestExactInt64AgainstBigRational(t *testing.T) {
	t.Parallel()

	for _, text := range numberSpellings() {
		rational, ok := new(big.Rat).SetString(text)
		if !ok {
			t.Fatalf("oracle cannot parse %q", text)
		}

		want := plan.NoFailure

		switch {
		case !rational.IsInt():
			want = plan.NotIntegral
		case !rational.Num().IsInt64():
			want = plan.OutOfRange
		default:
		}

		got, err := plan.ParseExactInt(text)
		if plan.ClassifyInteger(text) != want || (want == plan.NoFailure && (err != nil || got != rational.Num().Int64())) {
			t.Fatalf("%s = %d, %v; oracle says %s, class %v", text, got, err, rational.Num(), want)
		}
	}
}
