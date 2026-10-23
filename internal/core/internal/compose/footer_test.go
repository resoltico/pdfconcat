// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func footerDigits(config packet.PageNumbers) (*typeset.Digits, error) {
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		return nil, err
	}
	return typeset.NewDigits(font, config.Size)
}
func checkNumberPlacement(digits *typeset.Digits, size packet.Size, number int64, margin float64) error {
	_, err := footerPlacement(digits, size, number, margin)
	return err
}
func TestFooterInkBottomMatchesMarginForEveryDigit(t *testing.T) {
	digits, err := footerDigits(packet.PageNumbers{Size: 9})
	if err != nil {
		t.Fatal(err)
	}
	for number := int64(1); number <= 20; number++ {
		placed, err := footerPlacement(digits, packet.Size{Width: 300, Height: 200}, number, 24)
		if err != nil {
			t.Fatal(err)
		}
		if placed.InkBounds == nil || math.Abs(placed.InkBounds.Y-24) > 1e-9 {
			t.Fatalf("number %d ink bottom: %+v", number, placed.InkBounds)
		}
	}
}

func TestFooterPlacementRejectsOverflowAndAcceptsLargeSourceCanvas(t *testing.T) {
	for _, test := range []struct {
		size   packet.Size
		number int64
		config packet.PageNumbers
		bad    bool
	}{
		{packet.Size{Width: 50000, Height: 50000}, 123, packet.PageNumbers{Size: 9, Margin: 24}, false},
		{packet.Size{Width: 5, Height: 200}, 1234567890, packet.PageNumbers{Size: 9, Margin: 24}, true},
		{packet.Size{Width: 200, Height: 20}, 1, packet.PageNumbers{Size: 9, Margin: 24}, true},
		{packet.Size{Width: 200, Height: 200}, 1, packet.PageNumbers{Size: 100, Margin: 180}, true},
	} {
		digits, err := footerDigits(test.config)
		if err != nil {
			t.Fatal(err)
		}
		err = checkNumberPlacement(digits, test.size, test.number, test.config.Margin)
		if (err != nil) != test.bad {
			t.Fatalf("placement %+v: %v", test, err)
		}
	}
}

func TestFooterOverflowAdviceMatchesFailedPhysicalBounds(t *testing.T) {
	digits, err := footerDigits(packet.PageNumbers{Size: 9})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		size         packet.Size
		number       int64
		margin       float64
		want, absent []string
	}{
		{"lower", packet.Size{Width: 300, Height: 200}, 1, 0, []string{"below the physical page", "Increase page_numbers.margin to at least 2.637 pt"}, []string{"Decrease page_numbers.margin"}},
		{"upper", packet.Size{Width: 300, Height: 20}, 1, 24, []string{"above the physical page", "Decrease page_numbers.margin to at most 10.379 pt"}, []string{"Increase page_numbers.margin"}},
		{"width", packet.Size{Width: 5, Height: 200}, 1234567890, 24, []string{"width", "reduce page_numbers.size", "increase sheet width"}, []string{"Increase page_numbers.margin", "Decrease page_numbers.margin"}},
		{"width and lower", packet.Size{Width: 5, Height: 200}, 1234567890, 0, []string{"width", "below the physical page", "Increase page_numbers.margin"}, nil},
		{"short envelope with lower only", packet.Size{Width: 300, Height: 11}, 1, 0, []string{"below the physical page", "complete logical and ink envelope", "reduce page_numbers.size", "increase sheet height"}, []string{"Increase page_numbers.margin"}},
		{"short envelope crossing both edges", packet.Size{Width: 300, Height: 5}, 1, 0, []string{"below and above the physical page", "complete logical and ink envelope", "reduce page_numbers.size", "increase sheet height"}, []string{"Increase page_numbers.margin"}},
		{"width and both vertical edges", packet.Size{Width: 5, Height: 5}, 1234567890, 0, []string{"width", "below and above the physical page", "increase sheet width", "increase sheet height"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkNumberPlacement(digits, tc.size, tc.number, tc.margin)
			if err == nil {
				t.Fatal("overflow was accepted")
			}
			diagnostic := Diagnostic(err, "")
			if diagnostic.Code != diag.TextOverflow {
				t.Fatalf("overflow classification: %+v", diagnostic)
			}
			assertFooterMarginHintFits(t, digits, tc.size, tc.number, diagnostic.Message)
			for _, text := range tc.want {
				if !strings.Contains(diagnostic.Message, text) {
					t.Errorf("missing %q in %q", text, diagnostic.Message)
				}
			}
			for _, text := range tc.absent {
				if strings.Contains(diagnostic.Message, text) {
					t.Errorf("misleading %q in %q", text, diagnostic.Message)
				}
			}
		})
	}
	// The stated measured thresholds must give the user working remedies.
	for _, tc := range []struct {
		size   packet.Size
		margin float64
	}{{packet.Size{Width: 300, Height: 200}, 2.637}, {packet.Size{Width: 300, Height: 20}, 10.379}} {
		if err = checkNumberPlacement(digits, tc.size, 1, tc.margin); err != nil {
			t.Fatalf("recommended margin %g did not fit: %v", tc.margin, err)
		}
	}
	tight, err := footerDigits(packet.PageNumbers{Size: 9.1})
	if err != nil {
		t.Fatal(err)
	}
	tightSize := packet.Size{Width: 300, Height: tight.Top - tight.Bottom}
	message := Diagnostic(checkNumberPlacement(tight, tightSize, 1, 0), "").Message
	_, tail, found := strings.Cut(message, "Increase page_numbers.margin to at least ")
	if !found {
		t.Fatalf("missing tight-interval advice: %s", message)
	}
	var minimum float64
	if _, err = fmt.Sscanf(tail, "%f", &minimum); err != nil {
		t.Fatal(err)
	}
	if err = checkNumberPlacement(tight, tightSize, 1, minimum); err != nil {
		t.Fatalf("rounded advice left the narrow usable interval: %g: %v", minimum, err)
	}
	if err = checkNumberPlacement(digits, packet.Size{Width: 300, Height: math.NaN()}, 1, 24); err == nil || Diagnostic(err, "").Code != diag.TextOverflow {
		t.Fatalf("nonfinite physical height escaped the guard: %v", err)
	}
}

func assertFooterMarginHintFits(t *testing.T, digits *typeset.Digits, size packet.Size, number int64, message string) {
	t.Helper()
	for _, phrase := range []string{"Increase page_numbers.margin to at least ", "Decrease page_numbers.margin to at most "} {
		_, tail, found := strings.Cut(message, phrase)
		if !found {
			continue
		}
		var margin float64
		if _, err := fmt.Sscanf(tail, "%f", &margin); err != nil {
			t.Fatalf("invalid margin hint in %q: %v", message, err)
		}
		if err := checkNumberPlacement(digits, size, number, margin); err != nil {
			t.Fatalf("unusable margin hint %g in %q: %v", margin, message, err)
		}
	}
}

func TestFooterAdviceOmitsNumericalBoundsThatFailExactContainment(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		fontSize, width, height, margin, validMargin float64
		direction                                    string
	}{
		{"tight upper 9.1pt", 9.1, 1000000, 12.3942, 13.3942, 2.6663, "Decrease"},
		{"tight upper 1pt", 1, 1000000, 1.3619999999999999, 2.362, 0.293, "Decrease"},
		{"lower roundoff", 1.3890784982935154, 300, 200, 0, 0.408, "Increase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			digits, err := footerDigits(packet.PageNumbers{Size: tc.fontSize})
			if err != nil {
				t.Fatal(err)
			}
			size := packet.Size{Width: tc.width, Height: tc.height}
			err = checkNumberPlacement(digits, size, 1, tc.margin)
			if err == nil {
				t.Fatal("expected the initial margin to overflow")
			}
			message := Diagnostic(err, "").Message
			assertFooterMarginHintFits(t, digits, size, 1, message)
			if !strings.Contains(message, tc.direction+" page_numbers.margin.") || !strings.Contains(message, "reduce page_numbers.size or increase sheet height") {
				t.Fatalf("missing directional fallback: %s", message)
			}
			if strings.Contains(message, "to at least") || strings.Contains(message, "to at most") {
				t.Fatalf("unverified numerical bound was advertised: %s", message)
			}
			if err = checkNumberPlacement(digits, size, 1, tc.validMargin); err != nil {
				t.Fatalf("known usable margin %g refused: %v", tc.validMargin, err)
			}
		})
	}
}
