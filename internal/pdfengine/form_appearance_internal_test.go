// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestDefaultAppearanceRewritesOnlyParsedResourceOperands(t *testing.T) {
	t.Parallel()

	resources := map[string]types.Dict{keyFont: {"F1": types.Name(keyFont)}, keyExtGState: {"GS": types.Name("graphics")}}
	text := "% /F1 remains a comment\n/F#31 20 Tf 0.2 0.3 0.4 rg /GS gs 2 Tc"

	appearance, err := parseFormAppearance(t.Context(), text, resources)
	if err != nil {
		t.Fatal(err)
	}

	got := appearance.renamed(map[string]map[string]string{keyFont: {"F1": "Form2_Font1"}, keyExtGState: {"GS": "Form2_ExtGState1"}})

	want := "% /F1 remains a comment\n/Form2_Font1 20 Tf 0.2 0.3 0.4 rg /Form2_ExtGState1 gs 2 Tc"
	if got != want || !appearance.hasFont {
		t.Fatalf("rewritten appearance %q", got)
	}
}

func TestDefaultAppearanceRejectsMalformedOrUnsupportedSemantics(t *testing.T) {
	t.Parallel()

	resources := map[string]types.Dict{keyFont: {"F1": types.Name(keyFont)}}

	for _, text := range []string{
		"/F1", "/F1 12 Tf 1 2 g", "/Absent 12 Tf", "/F#xz 12 Tf", "/F1 (12) Tf",
		"(/F1) 12 Tf", "(/F1 12 Tf", "/F1 12 Tf (lookalike /F1) Tj", "/DeviceRGB cs", "1 2 Tf",
		"0.2 0.3 rg", "true g", "]", "<0g>", "/F1 1 R Tf",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			if _, err := parseFormAppearance(t.Context(), text, resources); err == nil {
				t.Fatalf("unsupported appearance accepted: %q", text)
			}
		})
	}
}

func TestDefaultAppearanceSupportsTextAndDirectColorState(t *testing.T) {
	t.Parallel()

	text := "/F1 0 Tf 1 Tc 2 Tw 80 Tz 14 TL 0 Tr -2 Ts .5 g .2 G .1 .2 .3 rg 0 0 0 RG 0 0 0 1 k 0 0 0 0 K"

	resources := map[string]types.Dict{keyFont: {"F1": types.Name(keyFont)}}
	if _, err := parseFormAppearance(t.Context(), text, resources); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultAppearanceCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := parseFormAppearance(ctx, strings.Repeat("0 g ", 100), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("form_appearance_internal_test cancellation: %v", err)
	}
}

func TestDefaultAppearanceNumericAndDelimiterFailures(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"1x g", "1.2.3 g", "9999999999999999999999999999999 g", "/F1 12 Tf /GS /GS gs", "(/F1) g"} {
		if _, err := parseFormAppearance(t.Context(), text, map[string]types.Dict{keyFont: {"F1": types.Name(keyFont)}}); err == nil {
			t.Fatalf("malformed appearance accepted: %q", text)
		}
	}

	for _, number := range []string{"", "1x", "1.2.3"} {
		if validAppearanceNumber(number) {
			t.Fatalf("invalid PDF number accepted: %q", number)
		}
	}

	if parsed, err := parseFormAppearance(t.Context(), " % comment without newline", nil); err != nil || parsed.hasFont {
		t.Fatalf("comment parsed as an operator: %v", err)
	}
}
