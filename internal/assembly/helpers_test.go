// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	bPath               = "b.pdf"
	maxLength           = 14400
	leafPath            = "leaf.pdf"
	aPath               = "a.pdf"
	smallestPoint       = 1
	inheritName         = "inherit"
	shortColor          = "#12"
	fontFile            = "f.ttf"
	leadingName         = "leading"
	blankOperand        = "--blank"
	argumentThree       = "argv:3"
	windowsName         = "windows"
	stringValueFormat   = "String() = %q"
	noFillName          = "none"
	detailedValueFormat = "%+v"
	namedFailureFormat  = "%s: %v"
	workingDirectory    = "/cwd"
	driveRelativePath   = "C:x"
	baseDirectory       = "/base"
	unsupportedUnitText = "5km"
	pdfPathLabel        = "PDF path"
)

var errBoom = errors.New("boom")

// near compares with a relative tolerance suitable for floating-point unit conversions.
func near(got, want float64) bool {
	return math.Abs(got-want) <= 1e-9*math.Max(1, math.Abs(want))
}

// defaultsOrigin and itemOrigin are two distinct places a value can have been written.
func defaultsOrigin() assembly.Origin { return assembly.Origin{Ref: 0, Offset: 10} }

func itemOrigin() assembly.Origin { return assembly.Origin{Ref: 3, Offset: 99} }

// a4Dim is the ISO A4 portrait page in points, from 210 x 297 mm at 72 points per 25.4 mm.
func a4Dim() assembly.PageDim {
	return assembly.PageDim{Width: 210 * 72 / 25.4, Height: 297 * 72 / 25.4}
}

func mustBlankItem(tb testing.TB, ref assembly.Ref, style *assembly.BlankStyle, count int64) assembly.Item {
	tb.Helper()

	item, err := assembly.NewBlankItem(assembly.Origin{Ref: ref}, style, count)
	if err != nil {
		tb.Fatal(err)
	}

	return item
}

func pdfAt(ref assembly.Ref, path string) assembly.Item {
	return assembly.NewPDFItem(assembly.Origin{Ref: ref}, path)
}

func argumentJob(items ...assembly.Item) *assembly.Job {
	return &assembly.Job{Source: assembly.ArgumentSource{}, Base: workingDirectory, Items: items}
}

func contains(text, part string) bool { return strings.Contains(text, part) }
