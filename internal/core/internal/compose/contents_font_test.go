// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func TestCustomContentsMetricsPaginationAndExclusions(t *testing.T) {
	data, err := os.ReadFile("../../testdata/corpus/fonts/Go-Mono.ttf")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}
	size := packet.Size{Width: 260, Height: 320}
	j, fonts := contentsRecipe(t, 1, 40, size)
	for _, item := range j.Items {
		if group, ok := item.(*packet.GroupItem); ok {
			group.Title = strings.Repeat("ill ", 10) + group.Ref
		}
	}
	j.PageNumbers = &packet.PageNumbers{Size: 10, Margin: 18, Position: packet.NumberTopRight, Kinds: []packet.PartKind{packet.PartContents}}
	j = packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items)
	fallback, ds := PrepareContents(t.Context(), j, nil, fonts, testFooter(t, j))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	selected := packet.Font{Path: "custom.data"}
	fonts[selected] = custom
	j.PageDefaults.Text.Font = packet.Setting[packet.Font]{Value: selected, From: "/page_defaults/text/font"}
	// Only the font participates; these defaults would break contents if inherited.
	j.PageDefaults.Text.Value.Value = "Do not inherit"
	j.PageDefaults.Text.Size.Value = 1000
	j.PageDefaults.Text.Leading.Value = 10
	j.PageDefaults.Text.Color.Value = packet.Color{R: 1}
	inherited, ds := PrepareContents(t.Context(), j, nil, fonts, testFooter(t, j))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if inherited.Pages() <= fallback.Pages() {
		t.Fatalf("wide custom font did not affect pagination: custom=%d bundled=%d", inherited.Pages(), fallback.Pages())
	}
	layout, ds := resolvePacket(j, nil, inherited.Pages())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	pages, ds := inherited.Finish(layout)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	overlay, err := typeset.NewDigits(fonts[packet.Font{}], 10)
	if err != nil {
		t.Fatal(err)
	}
	maxTop := size.Height - 18 - overlay.InkTop + overlay.Bottom - 6
	seenRows := 0
	for i, page := range pages {
		for n, b := range page.Blocks {
			if b.Text.Font != custom || b.Color != (packet.Color{}) {
				t.Fatalf("page%d block%d: wrong font/color", i, n)
			}
			want := 12.0
			if n == 0 {
				want = 18
			}
			if b.Text.Size != want {
				t.Fatalf("wrong fixed contents size %v", b.Text.Size)
			}
			bounds := paintedBounds(b.Text)
			if bounds.Y < 36-1e-6 || bounds.Y+bounds.Height > maxTop+1e-6 {
				t.Fatalf("block crosses custom/overlay bounds: %+v, top %v", bounds, maxTop)
			}
		}
		for row, link := range page.Links {
			a, z := page.Blocks[1+row*2].Text, page.Blocks[2+row*2].Text
			if paintedBounds(a).X+paintedBounds(a).Width > paintedBounds(z).X-12+1e-6 {
				t.Fatal("title overlaps digit column")
			}
			if link.Label != j.Sections()[seenRows].Title || link.TargetPage != layout.Sections[seenRows].Target {
				t.Fatal("link changed title/target association")
			}
			seenRows++
			if math.Abs(a.Lines[0].Baseline-z.Lines[0].Baseline) > 1e-6 {
				t.Fatal("label baseline differs from title")
			}
		}
	}
	if seenRows != len(j.Sections()) {
		t.Fatalf("missing contents rows: %d", seenRows)
	}
}
