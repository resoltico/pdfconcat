// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestFixedWrapWidthDiagnosticNamesWidthAndItsRepair(t *testing.T) {
	t.Parallel()

	params := baseParams("x")
	params.PageWidth, params.WrapWidth = 148*72/25.4, 160*72/25.4
	params.Overflow = typeset.OverflowReject
	font := defaultFont(t)

	var shaper typeset.Shaper

	placed, err := shaper.Place(font, params)

	overflow, found := errors.AsType[*typeset.OverflowError](err)
	if !found || !overflow.FixedWidthExceedsPage() || placed == nil {
		t.Fatalf("configured oversized block not distinguished: %v", err)
	}

	if overflow.WrapWidth != params.WrapWidth || overflow.PageWidth != params.PageWidth {
		t.Fatal("causal width/page facts changed")
	}

	message := err.Error()
	for _, part := range []string{"wider than the page", "reduce", "width"} {
		if !strings.Contains(message, part) {
			t.Fatalf("missing actionable width fact %q: %s", part, message)
		}
	}

	if strings.Contains(message, "shorten or wrap") {
		t.Fatal("text editing still advertised as a repair for the fixed oversized box")
	}

	params.WrapWidth = params.PageWidth - 20

	repaired, err := shaper.Place(font, params)
	if err != nil || len(repaired.Findings) != 0 {
		t.Fatalf("suggested width repair did not repair short text: %v %+v", err, repaired)
	}
}

func TestFixedWrapWidthAllowsIdenticalGeometryAndRetainsMixedFindings(t *testing.T) {
	t.Parallel()

	params := baseParams(strings.Repeat("W", 100) + "\n" + strings.Repeat("x\n", 40))
	params.WrapWidth, params.OffsetY, params.Overflow = 250, 50, typeset.OverflowReject
	font := defaultFont(t)

	var shaper typeset.Shaper

	rejected, err := shaper.Place(font, params)

	overflow, found := errors.AsType[*typeset.OverflowError](err)
	if !found || !overflow.FixedWidthExceedsPage() {
		t.Fatalf("mixed fixed-width rejection missing: %v", err)
	}

	kinds := make([]typeset.FindingKind, 0, len(rejected.Findings))
	for _, finding := range rejected.Findings {
		kinds = append(kinds, finding.Kind)
	}

	for _, kind := range []typeset.FindingKind{
		typeset.FindingWordTooWide,
		typeset.FindingOutsidePageHorizontal,
		typeset.FindingOutsidePageVertical,
	} {
		if !slices.Contains(kinds, kind) {
			t.Fatalf("mixed finding %s lost: %v", kind, kinds)
		}
	}

	params.Overflow = typeset.OverflowAllow

	allowed, err := shaper.Place(font, params)
	if err != nil {
		t.Fatal(err)
	}

	if rejected.Bounds != allowed.Bounds || !reflect.DeepEqual(rejected.InkBounds, allowed.InkBounds) ||
		!reflect.DeepEqual(rejected.Lines, allowed.Lines) ||
		!slices.Equal(rejected.Findings, allowed.Findings) {
		t.Fatal("allow policy changed bounds, ink, lines or findings")
	}
}

func TestFixedWrapWidthClassificationRequiresAnActualIntrinsicBlockFault(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, text    string
		width, offset float64
		fixed         bool
	}{
		{"within centered tolerance", "x", 200.0000015, 0, false},
		{"outside centered tolerance", "x", 200.0000025, 0, true},
		{"shift only", "x", 100, 250, false},
		{"glyph-expanded width", strings.Repeat("W", 100), 100, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := baseParams(tc.text)
			params.Anchor, params.WrapWidth = typeset.AnchorCenter, tc.width
			params.OffsetX, params.Overflow = tc.offset, typeset.OverflowReject

			var shaper typeset.Shaper

			placed, err := shaper.Place(defaultFont(t), params)
			overflow, found := errors.AsType[*typeset.OverflowError](err)

			fixed := found && overflow.FixedWidthExceedsPage()
			if placed == nil || fixed != tc.fixed {
				t.Fatalf("intrinsic width=%v want%v: %v", fixed, tc.fixed, err)
			}

			if fixed && !strings.Contains(err.Error(), "200.0000025") {
				t.Fatal("near-boundary width diagnostic rounded distinct widths to equal values")
			}
		})
	}
}

func TestEmptyWidthHasNoPlacementAndWhitespaceRetainsLogicalBlock(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "  "} {
		t.Run("text="+text, func(t *testing.T) {
			t.Parallel()

			params := baseParams(text)
			params.WrapWidth, params.Overflow = 250, typeset.OverflowReject

			var shaper typeset.Shaper

			placed, err := shaper.Place(defaultFont(t), params)
			if placed == nil || placed.InkBounds != nil {
				t.Fatalf("empty/whitespace unexpectedly painted: %+v %v", placed, err)
			}

			if text == "" {
				requireEmptyWidthPlacement(t, placed, err)
				return
			}

			overflow, found := errors.AsType[*typeset.OverflowError](err)
			if !found || !overflow.FixedWidthExceedsPage() || placed.Bounds.Width != 250 {
				t.Fatalf("whitespace logical width lost: %+v %v", placed, err)
			}
		})
	}
}

func requireEmptyWidthPlacement(t *testing.T, placed *typeset.Placed, err error) {
	t.Helper()

	if err != nil || len(placed.Lines) != 0 || placed.Bounds != (typeset.Rect{}) || len(placed.Findings) != 0 {
		t.Fatalf("empty semantics changed: %+v %v", placed, err)
	}
}

func TestConfiguredWidthFactsDoNotInventMissingHorizontalCause(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		findings []typeset.Finding
	}{
		{"empty findings", nil},
		{"vertical only", []typeset.Finding{{
			Kind: typeset.FindingOutsidePageVertical, Line: -1,
			Detail: "text block exceeds the page height",
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Exported error facts need the actual causal finding; scalar width facts alone are insufficient.
			overflow := &typeset.OverflowError{WrapWidth: 250, PageWidth: 200, Findings: tc.findings}
			if overflow.FixedWidthExceedsPage() || strings.Contains(overflow.Error(), "reduce the effective text width") {
				t.Fatal("width facts invented an absent horizontal overflow cause or remedy")
			}
		})
	}
}
