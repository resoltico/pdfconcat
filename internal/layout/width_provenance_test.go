// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package layout_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type widthProvenanceCase struct {
	name, document string
	pointers       []string
	affected       int64
}

const (
	widthDeclarationPointer = "/blank/text/width"
	inheritedWidthDocument  = `{
  "version": 1,
  "blank": {
    "text": {
      "value": "x",
      "width": "160mm",
      "anchor": "top"
    }
  },
  "items": [
    {
      "blank": {
        "size": "A5"
      },
      "count": 2
    },
    {
      "blank": {
        "size": "A5"
      }
    }
  ]
}`
	itemWidthDocument = `{
  "version": 1,
  "blank": {
    "text": {
      "value": "x",
      "width": "100mm",
      "anchor": "top"
    }
  },
  "items": [
    {
      "blank": {
        "size": "A5",
        "text": {
          "width": "160mm"
        }
      }
    },
    {
      "blank": {
        "size": "A5",
        "text": {
          "width": "160mm"
        }
      }
    }
  ]
}`
	groupedWidthDocument = `{
  "version": 1,
  "blank": {
    "text": {
      "value": "x",
      "width": "160mm",
      "anchor": "top"
    }
  },
  "items": [
    {
      "dir": ".",
      "items": [
        {
          "blank": {
            "size": "A5"
          }
        },
        {
          "blank": {
            "size": "A5"
          }
        }
      ]
    }
  ]
}`
)

func resolveWidthPlan(t *testing.T, document string) (*assembly.Layout, *typeset.Font) {
	t.Helper()
	font := loadFont(t)

	job, err := plan.Decode(t.Context(), plan.Input{Name: "<inline>", BaseDir: t.TempDir()}, strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}

	flat, err := assembly.Flatten(job)
	if err != nil {
		t.Fatal(err)
	}

	table, err := flat.Resolve(nil, func(string) (assembly.FontDigest, bool) { return assembly.FontDigest(font.Identity()), true })
	if err != nil {
		t.Fatal(err)
	}

	return table, font
}

func TestFixedWidthOverflowLocatesEffectiveDeclarationsAndConsumers(t *testing.T) {
	t.Parallel()

	cases := []widthProvenanceCase{
		{
			"inherited width",
			inheritedWidthDocument,
			[]string{widthDeclarationPointer},
			2,
		},
		{
			"equal appearance distinct item widths",
			itemWidthDocument,
			[]string{"/items/0/blank/text/width", "/items/1/blank/text/width"},
			1,
		},
		{
			"group width",
			groupedWidthDocument,
			[]string{widthDeclarationPointer},
			2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			table, font := resolveWidthPlan(t, tc.document)
			if len(table.Specs) != 1 {
				t.Fatalf("identical appearances not shared: %d", len(table.Specs))
			}

			var shaper typeset.Shaper

			placed, err := layout.PlaceSpec(&shaper, table, 0, builtIn(font))
			if placed == nil {
				t.Fatalf("overflow discarded measured bounds: %v", err)
			}

			assertWidthConsumers(t, err, tc.pointers, tc.affected)
		})
	}
}

func TestFixedWidthOverflowKeepsIndependentVerticalDeclaration(t *testing.T) {
	t.Parallel()
	table, font := resolveWidthPlan(
		t,
		`{
  "version": 1,
  "blank": {
    "text": {
      "value": "x",
      "width": "160mm",
      "anchor": "top",
      "y": "1000pt"
    }
  },
  "items": [
    {
      "blank": {
        "size": "A5"
      }
    }
  ]
}`,
	)

	var shaper typeset.Shaper

	placed, err := layout.PlaceSpec(&shaper, table, 0, builtIn(font))
	if placed == nil {
		t.Fatal(err)
	}

	diagnostics := assembly.Diagnostics(err)

	pointers := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		pointers = append(pointers, diagnostic.Location.Pointer)
	}

	if !slices.Equal(pointers, []string{widthDeclarationPointer, "/blank/text/y"}) {
		t.Fatalf("mixed width/vertical declarations lost: %v", pointers)
	}
}

func TestGlyphExpandedHorizontalOverflowKeepsPositionDeclaration(t *testing.T) {
	t.Parallel()
	table, font := resolveWidthPlan(
		t,
		`{
  "version": 1,
  "blank": {
    "text": {
      "value": "WWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWW",
      "width": "30pt",
      "x": "1000pt"
    }
  },
  "items": [
    {
      "blank": {
        "size": "A5"
      }
    }
  ]
}`,
	)

	var shaper typeset.Shaper

	_, err := layout.PlaceSpec(&shaper, table, 0, builtIn(font))
	diagnostics := assembly.Diagnostics(err)

	pointers := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		pointers = append(pointers, diagnostic.Location.Pointer)
	}

	if !slices.Contains(pointers, "/blank/text/x") || !slices.Contains(pointers, widthDeclarationPointer) {
		t.Fatalf("glyph/position declaration lost: %v", pointers)
	}
}

func TestStandaloneFixedWidthLocatesDeclaredWidthOrHonestFallback(t *testing.T) {
	t.Parallel()

	for _, declared := range []bool{true, false} {
		t.Run(map[bool]string{true: "declared", false: "unavailable"}[declared], func(t *testing.T) {
			t.Parallel()
			table, font := resolveWidthPlan(
				t,
				`{
  "version": 1,
  "blank": {
    "text": {
      "value": "x",
      "width": "160mm",
      "anchor": "top"
    }
  },
  "items": [
    {
      "blank": {
        "size": "A5"
      }
    }
  ]
}`,
			)
			table.Flat = nil
			want := widthDeclarationPointer

			if !declared {
				table.Specs[0].Declared.Width = assembly.Field[assembly.Length]{}
				want = "/items/0/blank"
			}

			var shaper typeset.Shaper

			_, err := layout.PlaceSpec(&shaper, table, 0, builtIn(font))

			diagnostics := assembly.Diagnostics(err)
			if len(diagnostics) != 1 || diagnostics[0].Location.Pointer != want {
				t.Fatalf("standalone width location %v want%s", diagnostics, want)
			}
		})
	}
}

func assertWidthConsumers(t *testing.T, err error, want []string, affected int64) {
	t.Helper()

	diagnostics := assembly.Diagnostics(err)

	pointers := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		pointers = append(pointers, diagnostic.Location.Pointer)
		if diagnostic.Code != layout.CodeTextOverflow || diagnostic.Affected != affected || int64(len(diagnostic.Consumers)) != affected {
			t.Fatalf("consumer/declaration facts changed: %+v", diagnostic)
		}
	}

	if !slices.Equal(pointers, want) {
		t.Fatalf("effective width locations %v want%v", pointers, want)
	}
}
