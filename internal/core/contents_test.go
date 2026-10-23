// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func contentsRecipe(t *testing.T, leading, count int, size packet.Size) (packet.Plan, map[packet.Font]*typeset.Font) {
	t.Helper()
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	style := packet.Defaults()
	style.Size.Value = packet.Size{Width: 595, Height: 842}
	j := packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.PageItem{Ref: "cover", Style: style, Count: leading}, &packet.ContentsItem{Ref: "toc", Title: "Contents", Size: size}})
	for i := range count {
		j.Items = append(j.Items, &packet.GroupItem{Ref: fmt.Sprintf("section%d", i), Title: fmt.Sprintf("Pielikums Ā ļ ķ Ελληνικά Кириллица %d", i), Items: []packet.Item{&packet.PageItem{Ref: fmt.Sprintf("page%d", i), Style: style, Count: 1}}})
	}
	return j, map[packet.Font]*typeset.Font{{}: font}
}

func TestContentsCountDoesNotReflowAtTargetDigitTransitions(t *testing.T) {
	for _, leading := range []int{7, 97} {
		var heights []float64
		for delta := range 2 {
			j, fonts := contentsRecipe(t, leading+delta, 12, packet.Size{Width: 595, Height: 842})
			c, ds := prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts)
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			layout, ds := packet.Resolve(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, len(c.pages))
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			pages := map[int][]pdf.Page{}
			if ds = c.finish(layout, pages); len(ds) > 0 {
				t.Fatal(ds)
			}
			if len(c.pages) != 1 {
				t.Fatalf("unexpected contents pages%d", len(c.pages))
			}
			target := int64(leading + delta + 2)
			if layout.Sections[0].Target != target {
				t.Fatal(layout.Sections)
			}
			for i, row := range c.rows {
				if delta == 0 {
					heights = append(heights, row.bounds.Height)
				} else if math.Abs(heights[i]-row.bounds.Height) > 1e-9 {
					t.Fatal("actual target digits reflowed rows")
				}
				if c.pages[row.page].Links[i].TargetPage != layout.Sections[i].Target {
					t.Fatal("contents target differs from section")
				}
			}
			page := c.pages[0]
			found := false
			for _, block := range page.Blocks {
				for _, line := range block.Text.Lines {
					if line.Text() == strconv.FormatInt(target, 10) {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("physical target digits missing")
			}
		}
	}
}

func TestContentsMultiplePagesReserveFittedFooterEnvelope(t *testing.T) {
	authored, _ := packet.ParseSize("A3", false)
	fit, _ := packet.ParseSize("A5", false)
	j, fonts := contentsRecipe(t, 1, 80, authored)
	j.FitTo = &fit
	j.PageNumbers = &packet.PageNumbers{Size: 9, Margin: 50 * 72 / 25.4}
	c, ds := prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(c.pages) < 2 || len(c.pages) > 80 {
		t.Fatalf("contents count%d", len(c.pages))
	}
	digits, err := typeset.NewDigits(fonts[packet.Font{}], 9)
	if err != nil {
		t.Fatal(err)
	}
	protected := j.PageNumbers.Margin - digits.InkBottom + digits.Top + 6
	scale := math.Min(fit.Width/authored.Width, fit.Height/authored.Height)
	offset := (fit.Height - authored.Height*scale) / 2
	for _, row := range c.rows {
		if row.bounds.Y*scale+offset < protected-1e-6 {
			t.Fatalf("row overlaps physical footer exclusion: %+v", row)
		}
	}
	j.PageNumbers.Margin = fit.Height
	if _, ds = prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts); len(ds) == 0 || ds[0].Code.String() != "text_overflow" {
		t.Fatalf("impossible footer space accepted: %+v", ds)
	}
}

func TestContentsRefusesHeadingInkAndWholeRowOverflow(t *testing.T) {
	j, fonts := contentsRecipe(t, 1, 1, packet.Size{Width: 109.152, Height: 200})
	itemAs[*packet.ContentsItem](t, j.Items[1]).Title = "ffffff"
	if _, ds := prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts); len(ds) == 0 || ds[0].Code.String() != "text_overflow" {
		t.Fatalf("heading ink margin was ignored: %+v", ds)
	}
	j, fonts = contentsRecipe(t, 1, 1, packet.Size{Width: 200, Height: 150})
	itemAs[*packet.GroupItem](t, j.Items[2]).Title = strings.Repeat("Long wrapped title ", 20)
	if _, ds := prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts); len(ds) == 0 || ds[0].Code.String() != "text_overflow" {
		t.Fatalf("oversized whole row accepted: %+v", ds)
	}
	nested := j.Items[2]
	for i := range 20 {
		nested = &packet.GroupItem{Ref: fmt.Sprintf("ancestor%d", i), Title: "Ancestor", Items: []packet.Item{nested}}
	}
	j.Items[2] = nested
	if _, ds := prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts); len(ds) == 0 {
		t.Fatal("unusable indented column accepted")
	}
	j.Items = j.Items[:1]
	if c, ds := prepareContents(t.Context(), packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), nil, fonts); c != nil || len(ds) > 0 {
		t.Fatalf("absent contents must mean C=0: %+v %+v", c, ds)
	}
}

func itemAs[T packet.Item](t testing.TB, item packet.Item) T {
	t.Helper()
	value, ok := item.(T)
	if !ok {
		t.Fatalf("item has type %T, expected %T", item, value)
	}
	return value
}
