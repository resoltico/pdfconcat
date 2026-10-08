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

func TestParsePageSizeInherit(t *testing.T) {
	t.Parallel()

	inherit, err := assembly.ParsePageSize(inheritName)
	if err != nil || !inherit.Inherit || inherit.String() != inheritName || inherit.Validate() != nil {
		t.Errorf("inherit = %+v, %v", inherit, err)
	}
}

func TestParsePageSizeNamedPapers(t *testing.T) {
	t.Parallel()

	// ISO 216 sizes are millimetres, US sizes are points (inches times 72); both written down independently.
	for name, want := range map[string][2]float64{
		"A3": {297 * 72 / 25.4, 420 * 72 / 25.4}, "A4": {210 * 72 / 25.4, 297 * 72 / 25.4}, "A5": {148 * 72 / 25.4, 210 * 72 / 25.4},
		"B4": {250 * 72 / 25.4, 353 * 72 / 25.4}, "B5": {176 * 72 / 25.4, 250 * 72 / 25.4},
		"Letter": {612, 792}, "Legal": {612, 1008}, "Tabloid": {792, 1224},
	} {
		size, err := assembly.ParsePageSize(name)
		if err != nil || size.Inherit || !near(float64(size.Dim.Width), want[0]) || !near(float64(size.Dim.Height), want[1]) {
			t.Errorf("ParsePageSize(%q) = %+v, %v; want %v", name, size, err, want)
		}
	}

	if got := strings.Join(assembly.PageSizeNames(), ","); got != "A3,A4,A5,B4,B5,Letter,Legal,Tabloid" {
		t.Errorf("PageSizeNames() = %s", got)
	}
}

func TestParsePageSizeDimensions(t *testing.T) {
	t.Parallel()

	for text, want := range map[string][2]float64{
		"100x200": {100, 200}, "100x200pt": {100, 200}, "1x1": {1, 1}, "14400x14400": {
			maxLength,
			maxLength,
		}, "210x297mm": {210 * 72 / 25.4, 297 * 72 / 25.4},
		"8.5x11in": {612, 792}, "10.5x.5in": {756, 36}, "5080x5080mm": {maxLength, maxLength}, "20x30cm": {20 * 72 / 2.54, 30 * 72 / 2.54},
	} {
		size, err := assembly.ParsePageSize(text)
		if err != nil || !near(float64(size.Dim.Width), want[0]) || !near(float64(size.Dim.Height), want[1]) {
			t.Errorf("ParsePageSize(%q) = %v, %v; want %v", text, size.Dim, err, want)
		}
	}

	if got := (assembly.PageSize{Dim: assembly.PageDim{Width: 100.25, Height: 50}}).String(); got != "100.25x50.00pt" {
		t.Errorf(stringValueFormat, got)
	}
}

func TestParsePageSizeRejects(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"", "a4", "A6", "Inherit", " A4", "A4 ", "100", "x", "100x", "x100",
		"100x200x300", "100X200", "100x200 mm", "100mmx200mm", "-1x10", "10x-1",
		"0x10", "10x0", "0.99x10", "14400.5x10", "10x14401", "5081x10mm",
		"1e3x10", ".x5", "5.x5", "10x10km", "999999999999999999999999999999x1",
		"1" + strings.Repeat("0", 400) + "x1", "1x1" + strings.Repeat("0", 400),
	} {
		got, err := assembly.ParsePageSize(text)
		if !errors.Is(err, assembly.ErrInvalidPageSize) {
			t.Errorf("ParsePageSize(%q) = %+v, %v; want ErrInvalidPageSize", text, got, err)
		}
	}
}

func TestPageDimDomainRejectsInvalidSides(t *testing.T) {
	t.Parallel()

	nan, inf := math.NaN(), math.Inf(1)

	for _, dim := range []assembly.PageDim{
		{},
		{Width: 1},
		{Height: 1},
		{Width: 0.99, Height: 10},
		{Width: 10, Height: -1},
		{Width: 14400.01, Height: 10},
		{Width: assembly.Length(nan), Height: 10},
		{Width: 10, Height: assembly.Length(nan)},
		{Width: assembly.Length(inf), Height: 10},
		{Width: 10, Height: assembly.Length(-inf)},
	} {
		if !errors.Is(dim.Validate(), assembly.ErrOutOfRange) {
			t.Errorf("%v accepted", dim)
		}

		_, err := assembly.NewPageDim(dim.Width, dim.Height)
		if err == nil {
			t.Errorf("NewPageDim(%v) accepted", dim)
		}

		if (assembly.PageSize{Dim: dim}).Validate() == nil {
			t.Errorf("PageSize{%v} accepted", dim)
		}
	}
}

func TestPageDimDomainAcceptsItsBounds(t *testing.T) {
	t.Parallel()

	for _, dim := range []assembly.PageDim{{Width: 1, Height: 1}, {Width: maxLength, Height: maxLength}, {Width: 595.28, Height: 841.89}} {
		got, err := assembly.NewPageDim(dim.Width, dim.Height)
		if err != nil || got != dim {
			t.Errorf("NewPageDim(%v) = %v, %v", dim, got, err)
		}
	}
}
