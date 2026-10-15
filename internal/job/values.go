// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package job defines the assembly domain without PDF or filesystem I/O.
package job

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// MaxEntries bounds flattened source/generated items.
const MaxEntries = 100_000

// MaxGeneratedPages bounds both an item count and the job total.
const MaxGeneratedPages = 100_000

// MaxSide is the generated canvas/length limit in points.
const MaxSide = 14_400.0

// Size is a physical width and height in points.
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Color is an eight-bit sRGB triplet.
type Color struct{ R, G, B uint8 }

var decimalLength = regexp.MustCompile(`^-?([0-9]+(\.[0-9]+)?|\.[0-9]+)(pt|mm|cm|in)?$`)
var dimensions = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?|\.[0-9]+)x([0-9]+(\.[0-9]+)?|\.[0-9]+)(pt|mm|cm|in)?$`)

// Length parses a decimal point length with an optional physical unit.
func Length(text string) (float64, error) {
	if !decimalLength.MatchString(text) {
		return 0, fmt.Errorf("invalid length %q; use a decimal with pt, mm, cm or in", text)
	}
	factor := 1.0
	for unit, scale := range map[string]float64{"pt": 1, "mm": 72 / 25.4, "cm": 72 / 2.54, "in": 72} {
		if strings.HasSuffix(text, unit) {
			text = strings.TrimSuffix(text, unit)
			factor = scale
			break
		}
	}
	n, err := strconv.ParseFloat(text, 64)
	n *= factor
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > MaxSide {
		return 0, errors.New("length must be within ±14400 points")
	}
	return n, nil
}

// ParseSize accepts portrait paper names or explicit dimensions.
func ParseSize(text string, inherit bool) (Size, error) {
	if text == "inherit" && inherit {
		return Size{}, nil
	}
	paper := map[string]Size{
		"A3": {297 * 72 / 25.4, 420 * 72 / 25.4}, "A4": {210 * 72 / 25.4, 297 * 72 / 25.4}, "A5": {148 * 72 / 25.4, 210 * 72 / 25.4},
		"B4": {250 * 72 / 25.4, 353 * 72 / 25.4}, "B5": {176 * 72 / 25.4, 250 * 72 / 25.4},
		"Letter": {612, 792}, "Legal": {612, 1008}, "Tabloid": {792, 1224},
	}
	if size, ok := paper[text]; ok {
		return size, nil
	}
	m := dimensions.FindStringSubmatch(text)
	if m == nil {
		return Size{}, fmt.Errorf("invalid size %q; use a paper name or WIDTHxHEIGHT[unit]", text)
	}
	w, e1 := Length(m[1] + m[5])
	h, e2 := Length(m[3] + m[5])
	if e1 != nil || e2 != nil || w < 1 || h < 1 {
		return Size{}, errors.New("each page side must be 1–14400 points")
	}
	return Size{w, h}, nil
}

// ParseColor accepts RGB hex colors and optionally an absent fill.
func ParseColor(text string, none bool) (*Color, error) {
	if none && text == "none" {
		return nil, nil
	}
	if len(text) != 4 && len(text) != 7 || !strings.HasPrefix(text, "#") {
		return nil, errors.New("color must be #RGB or #RRGGBB")
	}
	s := text[1:]
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	n, err := strconv.ParseUint(s, 16, 24)
	if err != nil {
		return nil, errors.New("color must be #RGB or #RRGGBB")
	}
	return &Color{uint8(n >> 16), uint8(n >> 8 & 0xff), uint8(n & 0xff)}, nil
}

// ValidatePath rejects empty/NUL paths and the two ambiguous Windows forms.
func ValidatePath(name string) error {
	if name == "" || strings.ContainsRune(name, 0) {
		return errors.New("path must be nonempty and contain no NUL")
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(name)
		if volume != "" && !filepath.IsAbs(name) || volume == "" && (strings.HasPrefix(name, `\`) || strings.HasPrefix(name, "/")) {
			return errors.New("use an absolute or ordinary relative path, not a drive-relative or rooted-without-drive path")
		}
	}
	return nil
}

// Path resolves lexically, following the host's path conventions.
func Path(base, name string) (string, error) {
	if err := ValidatePath(name); err != nil {
		return "", err
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(base, name)
	}
	return filepath.Abs(name)
}
