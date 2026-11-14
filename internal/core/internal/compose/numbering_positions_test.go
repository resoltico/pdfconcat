// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func TestNumberingAllPhysicalPlacements(t *testing.T) {
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	size := packet.Size{Width: 300, Height: 400}
	for _, position := range []packet.NumberPosition{packet.NumberTopLeft, packet.NumberTopCenter, packet.NumberTopRight, packet.NumberBottomLeft, packet.NumberBottomCenter, packet.NumberBottomRight} {
		footer, err := NewPageNumberLayout(font, packet.PageNumbers{Size: 9, Margin: 24, Position: position})
		if err != nil {
			t.Fatal(err)
		}
		placed, err := footer.Place(size, 987)
		if err != nil {
			t.Fatal(position, err)
		}
		ink := placed.InkBounds
		if ink == nil || !numberFits(placed, size) {
			t.Fatal("incomplete envelope")
		}
		if strings.HasPrefix(string(position), "top-") {
			if math.Abs(ink.Y+ink.Height-376) > 1e-8 {
				t.Fatal(position, ink)
			}
		} else if math.Abs(ink.Y-24) > 1e-8 {
			t.Fatal(position, ink)
		}
		if strings.HasSuffix(string(position), "-left") && math.Abs(ink.X-24) > 1e-8 {
			t.Fatal(position, ink)
		}
		if strings.HasSuffix(string(position), "-right") && math.Abs(ink.X+ink.Width-276) > 1e-8 {
			t.Fatal(position, ink)
		}
		if strings.HasSuffix(string(position), "-center") && math.Abs(placed.Bounds.X+placed.Bounds.Width/2-150) > 1e-8 {
			t.Fatal(position, placed.Bounds)
		}
	}
}

func TestTopContentsExclusionOnlyWhenEligible(t *testing.T) {
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	size := packet.Size{Width: 300, Height: 400}
	fit := packet.Size{Width: 450, Height: 500}
	numbers := &packet.PageNumbers{Size: 9, Margin: 100, Position: packet.NumberTopLeft}
	for _, enabled := range []bool{true, false} {
		items := []packet.Item{&packet.ContentsItem{Ref: "toc", Size: size, Title: "Contents", Numbering: &enabled}, &packet.GroupItem{Ref: "section", Title: "A section", Items: []packet.Item{&packet.PageItem{Ref: "body", Count: 1, Style: packet.Defaults()}}}}
		plan := packet.NewPlan("", &fit, numbers, packet.Defaults(), items)
		footer, err := NewPageNumberLayout(font, *numbers)
		if err != nil {
			t.Fatal(err)
		}
		contents, ds := PrepareContents(t.Context(), plan, nil, map[packet.Font]*typeset.Font{{}: font}, footer)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		heading := paintedBounds(contents.pages[0].Blocks[0].Text)
		top := heading.Y + heading.Height
		if enabled {
			_, limit := footer.Available(size, &fit)
			if top > limit+1e-8 {
				t.Fatalf("heading %g crosses exclusion %g", top, limit)
			}
		} else if math.Abs(top-364) > 1e-8 {
			t.Fatal("suppressed overlay reserved space", top)
		}
	}
}
