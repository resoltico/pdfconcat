// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestParseLength(t *testing.T) {
	t.Parallel()

	// Expected points come from the unit definitions (1 in = 72 pt = 25.4 mm), not from the implementation's ratios.
	for text, want := range map[string]float64{
		"0": 0, "12": 12, "-12": -12, "12.5": 12.5, ".5": 0.5, "-.5": -0.5, "0012": 12, "12pt": 12, "1in": 72, "25.4mm": 72, "2.54cm": 72,
		"10mm": 10 * 72 / 25.4, "-3.5cm": -3.5 * 72 / 2.54, "5080mm": maxLength,
		"200in": maxLength, "-14400": -maxLength, "14400pt": maxLength,
	} {
		got, err := assembly.ParseLength(text)
		if err != nil || !near(float64(got), want) {
			t.Errorf("ParseLength(%q) = %v, %v; want %v", text, got, err, want)
		}
	}
}

func TestParseLengthRejects(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"", " ", "mm", "-", "-mm", ".", "5.", "5.mm", "+5", "1e3", "1E3mm",
		"0x10", "1_0", "Inf", "NaN", "-Inf", "infinity", "5 mm", " 5mm", "5mm ",
		unsupportedUnitText, "5MM", "5mmm", "5 mm", "٣", "5,5", "--5", "14400.0001", "-14401", "5081mm", "201in",
		"1" + strings.Repeat("0", 400), "0." + strings.Repeat("9", 400) + "e",
	} {
		got, err := assembly.ParseLength(text)
		if err == nil {
			t.Errorf("ParseLength(%q) = %v; want an error", text, got)
		}
	}
}

func TestLengthErrorsAreClassified(t *testing.T) {
	t.Parallel()

	_, err := assembly.ParseLength(unsupportedUnitText)
	if !errors.Is(err, assembly.ErrInvalidLength) {
		t.Errorf("a malformed length wraps ErrInvalidLength: %v", err)
	}

	_, err = assembly.ParseLength("99999in")
	if !errors.Is(err, assembly.ErrOutOfRange) {
		t.Errorf("an oversized length wraps ErrOutOfRange: %v", err)
	}

	_, err = assembly.ParseLength("1" + strings.Repeat("0", 400))
	if !errors.Is(err, assembly.ErrOutOfRange) {
		t.Errorf("an unrepresentable length wraps ErrOutOfRange: %v", err)
	}
}

func TestLengthDomainRejectsNonFiniteAndOversized(t *testing.T) {
	t.Parallel()

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 14400.000001, -14400.000001, math.MaxFloat64} {
		_, err := assembly.NewLength(bad)
		if !errors.Is(err, assembly.ErrOutOfRange) {
			t.Errorf("NewLength(%v) = %v", bad, err)
		}

		if !errors.Is(assembly.Length(bad).Validate(), assembly.ErrOutOfRange) {
			t.Errorf("Length(%v).Validate() accepted", bad)
		}
	}
}

func TestLengthDomainAcceptsItsBounds(t *testing.T) {
	t.Parallel()

	for _, good := range []float64{0, 1, -maxLength, maxLength, 0.001} {
		length, err := assembly.NewLength(good)
		if err != nil || float64(length) != good {
			t.Errorf("NewLength(%v) = %v, %v", good, length, err)
		}
	}
}
