// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"math"
	"os"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestCustomContentsMetricsPaginationAndExclusions(t *testing.T) {
	data, err := os.ReadFile("../app/testdata/corpus/fonts/Go-Mono.ttf")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}
	size := packet.Size{Width: 260, Height: 320}
	j, fonts := contentsRecipe(t, 1, 40, size)
	jItems := j.Items()
	for _, item := range jItems {
		if group, ok := item.(*packet.GroupItem); ok {
			group.Title = strings.Repeat("ill ", 10) + group.Ref
		}
	}
	{
		draft := j.Declaration()
		draft.PageNumbers = &packet.PageNumbers{Size: 10, Margin: 18, Position: packet.NumberTopRight, Kinds: []packet.PartKind{packet.PartContents}, Mode: "physical", Start: 1}
		j = pdffixture.Plan(t, draft)
	}
	j = pdffixture.Plan(t, packet.Declaration{Output: j.Output(), Fitting: j.Fitting(), PageNumbers: j.PageNumbers(), PageDefaults: j.PageDefaults(), Items: jItems})
	fallback, ds := PrepareContents(t.Context(), j, nil, fonts, testFooter(t, j))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	selected := packet.Font{Path: "custom.data"}
	fonts[selected] = custom
	defaults := j.PageDefaults()
	defaults.Text.Font = packet.Setting[packet.Font]{Present: true, Value: selected, From: "/page_defaults/text/font"}
	// Only the font participates; these defaults would break contents if inherited.
	defaults.Text.Value.Value = "Do not inherit"
	defaults.Text.Size.Value = 1000
	defaults.Text.Size.Present = true
	defaults.Text.Leading.Value = 10
	defaults.Text.Color.Value = packet.Color{R: 1}
	{
		draft := j.Declaration()
		draft.PageDefaults = defaults
		j = pdffixture.Plan(t, draft)
	}
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

// These synthetic fonts share identical metrics and differ only in the dash's
// outline position. The field replay independently checks the raster and links.
func TestContentsDashInkStaysInItsOwnLink(t *testing.T) {
	bundled, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"control", "lowered"} {
		t.Run(variant, func(t *testing.T) {
			data, err := os.ReadFile("testdata/row-label-" + variant + ".ttf")
			if err != nil {
				t.Fatal(err)
			}
			font, err := typeset.LoadFont(data)
			if err != nil {
				t.Fatal(err)
			}
			key := packet.Font{Path: "row-label.ttf"}
			faces := map[packet.Font]*typeset.Font{{}: bundled, key: font}
			size := packet.Size{Width: 300, Height: 192}
			style := packet.Defaults()
			style.Size = packet.Setting[packet.Size]{Present: true, Value: size}
			style.Text.Font = packet.Setting[packet.Font]{Present: true, Value: key}
			numbers := packet.DefaultPageNumbers()
			numbers.Kinds = []packet.PartKind{packet.PartContents}
			plan := pdffixture.Plan(t, packet.Declaration{
				PageDefaults: style, PageNumbers: numbers,
				Items: []packet.Item{
					&packet.ContentsItem{Title: "Index", Size: size},
					&packet.GroupItem{Title: "Annex", Items: []packet.Item{&packet.PageItem{Count: 1}}},
					&packet.GroupItem{Title: "Following", Items: []packet.Item{&packet.PageItem{Count: 1}}},
				},
			})
			contents, findings := PrepareContents(t.Context(), plan, nil, faces, testFooter(t, plan))
			if len(findings) > 0 {
				t.Fatal(findings)
			}
			layout, findings := packet.Resolve(plan, nil, contents.Pages())
			if len(findings) > 0 {
				t.Fatal(findings)
			}
			pages, findings := contents.Finish(layout)
			if len(findings) > 0 {
				t.Fatal(findings)
			}
			row := 0
			for _, p := range pages {
				for i, link := range p.Links {
					if link.TargetPage != layout.Sections[row].Target || layout.LabelAt(link.TargetPage) != "—" {
						t.Fatalf("unexpected contents target: %+v", link)
					}
					ink := p.Blocks[2+2*i].Text.InkBounds
					const epsilon = 1e-6
					if ink == nil || ink.X < link.Rect.X-epsilon || ink.Y < link.Rect.Y-epsilon ||
						ink.X+ink.Width > link.Rect.X+link.Rect.Width+epsilon ||
						ink.Y+ink.Height > link.Rect.Y+link.Rect.Height+epsilon {
						t.Fatalf("dash ink %+v escapes its link %+v", ink, link.Rect)
					}
					row++
				}
			}
			if row != 2 {
				t.Fatalf("expected two labelled links, got %d", row)
			}
		})
	}
}
