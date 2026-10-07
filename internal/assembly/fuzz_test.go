// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"math"
	"math/big"
	"regexp"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// The grammars below are written independently of the parsers, as regular expressions; they are the same
// strings that plan.schema.json declares.
const (
	numberPattern = `(?:\d+(?:\.\d+)?|\.\d+)`
	unitPattern   = `(pt|mm|cm|in)?`
	slack         = 1_000_000_000_000
)

func lengthGrammar() *regexp.Regexp {
	return regexp.MustCompile(`^(-?` + numberPattern + `)` + unitPattern + `$`)
}

func sizeGrammar() *regexp.Regexp {
	return regexp.MustCompile(`^(` + numberPattern + `)x(` + numberPattern + `)` + unitPattern + `$`)
}

func colorGrammar() *regexp.Regexp {
	return regexp.MustCompile(`^#(?:[0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)
}

// unitPoints is the exact points per unit as a rational number: 1 in = 72 pt = 25.4 mm.
func unitPoints(unit string) *big.Rat {
	switch unit {
	case "mm":
		return big.NewRat(720, 254)
	case "cm":
		return big.NewRat(7200, 254)
	case "in":
		return big.NewRat(72, 1)
	default:
		return big.NewRat(1, 1)
	}
}

// pointsOf is the exact value in points of a number written in a unit.
func pointsOf(tb testing.TB, number, unit string) *big.Rat {
	tb.Helper()

	value, ok := new(big.Rat).SetString(number)
	if !ok {
		tb.Fatalf("oracle cannot parse %q", number)
	}

	return value.Mul(value, unitPoints(unit))
}

// ratio is 1 + 10^-12, the slack that float64 rounding of a boundary value can account for.
func ratio() *big.Rat { return big.NewRat(slack+1, slack) }

// within reports whether |value| is below limit by more than the rounding slack.
func within(value *big.Rat, limit int64) bool {
	return new(big.Rat).Abs(value).Cmp(new(big.Rat).Quo(big.NewRat(limit, 1), ratio())) < 0
}

// above reports whether value exceeds limit by more than the rounding slack.
func above(value *big.Rat, limit int64) bool {
	return value.Cmp(new(big.Rat).Mul(big.NewRat(limit, 1), ratio())) > 0
}

// checkLength verifies ParseLength on one input against the grammar and exact arithmetic.
func checkLength(t *testing.T, grammar *regexp.Regexp, text string) {
	t.Helper()

	length, err := assembly.ParseLength(text)
	again, errAgain := assembly.ParseLength(text)

	if (err == nil) != (errAgain == nil) || (err == nil && length != again) {
		t.Fatalf("not deterministic for %q", text)
	}

	match := grammar.FindStringSubmatch(text)
	inGrammar := len(match) == 3

	if err != nil {
		if inGrammar && within(pointsOf(t, match[1], match[2]), maxLength) {
			t.Fatalf("%q is in the grammar and well inside ±14400 points but was rejected: %v", text, err)
		}

		return
	}

	if !inGrammar {
		t.Fatalf("%q accepted but is outside the grammar", text)
	}

	exact, _ := pointsOf(t, match[1], match[2]).Float64()
	if length.Validate() != nil || !near(float64(length), exact) {
		t.Fatalf("%q = %v, exact value %v", text, length, exact)
	}
}

func FuzzLength(f *testing.F) {
	for _, seed := range []string{
		"", "0", "12", "-12.5", ".5", "5.", "10mm", "-3cm", "1in", "5080mm",
		"5081mm", "14400", "14400.0000000000001", "14399.99999999999999999999",
		"1e3", "Inf", "NaN", "0x10", "1_0", " 1", "1 ", "٣", "999999999999999999999999999999999999999999999999", "5MM", "--1", "+1",
	} {
		f.Add(seed)
	}

	grammar := lengthGrammar()

	f.Fuzz(func(t *testing.T, text string) { checkLength(t, grammar, text) })
}

func FuzzColor(f *testing.F) {
	for _, seed := range []string{
		"", "#", "#000", "#FFF", "#abcdef", "#ABCDEG", shortColor, "#1234567", "000000", "#ééé", "#+12", noFillName,
	} {
		f.Add(seed)
	}

	grammar := colorGrammar()

	f.Fuzz(func(t *testing.T, text string) {
		color, err := assembly.ParseColor(text)

		if (err == nil) != grammar.MatchString(text) {
			t.Fatalf("%q: ParseColor error %v disagrees with the grammar", text, err)
		}

		if err != nil {
			return
		}

		back, backErr := assembly.ParseColor(color.String())
		if backErr != nil || back != color {
			t.Fatalf("%q -> %v does not round-trip: %v %v", text, color, back, backErr)
		}

		fill, fillErr := assembly.ParseFill(text)
		if fillErr != nil || !fill.Painted || fill.Color != color {
			t.Fatalf("ParseFill(%q) = %+v, %v", text, fill, fillErr)
		}
	})
}

// sidesAreSafe reports whether both sides of a written size are well inside the accepted 1 to 14400 points.
func sidesAreSafe(width, height *big.Rat) bool {
	return within(width, maxLength) && within(height, maxLength) && above(width, 1) && above(height, 1)
}

// checkPageSize verifies ParsePageSize on one input against the grammar and exact arithmetic.
func checkPageSize(t *testing.T, grammar *regexp.Regexp, text string) {
	t.Helper()

	size, err := assembly.ParsePageSize(text)
	again, errAgain := assembly.ParsePageSize(text)

	if (err == nil) != (errAgain == nil) || (err == nil && size != again) {
		t.Fatalf("not deterministic for %q", text)
	}

	match := grammar.FindStringSubmatch(text)
	known := text == inheritName || slices.Contains(assembly.PageSizeNames(), text)

	if err == nil {
		if !known && len(match) != 4 {
			t.Fatalf("%q accepted but is outside the grammar", text)
		}

		checkAcceptedSize(t, text, &size)

		return
	}

	if known {
		t.Fatalf("%q is a documented size but was rejected: %v", text, err)
	}

	if len(match) == 4 && sidesAreSafe(pointsOf(t, match[1], match[3]), pointsOf(t, match[2], match[3])) {
		t.Fatalf("%q is in the grammar with sides well inside 1..14400 points but was rejected: %v", text, err)
	}
}

func checkAcceptedSize(t *testing.T, text string, size *assembly.PageSize) {
	t.Helper()

	if size.Validate() != nil || size.Inherit != (text == inheritName) || math.IsNaN(float64(size.Dim.Width)) {
		t.Fatalf("%q = %+v violates the domain", text, size)
	}
}

func FuzzPageSize(f *testing.F) {
	for _, seed := range []string{
		"", inheritName, "A4", "a4", "Letter", "100x200", "100x200mm", "210x297mm",
		"0x0", "1x1", "14400x14400", "14401x1", "5080x5080mm", "5081x1mm",
		"-1x5", "1X1", "1e3x1", ".5x.5in", "5.x5", "1x", "x1", "99999999999999999999999999x1",
	} {
		f.Add(seed)
	}

	grammar := sizeGrammar()

	f.Fuzz(func(t *testing.T, text string) { checkPageSize(t, grammar, text) })
}
