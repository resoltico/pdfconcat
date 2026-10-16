// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func contentsRecipe(t *testing.T, leading, count int, size job.Size) (job.Job, map[string]*typeset.Font) {
	t.Helper()
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	style := job.Defaults()
	style.Size = job.Size{Width: 595, Height: 842}
	j := job.Job{Parts: []job.Part{{Pointer: "cover", Page: &style, Count: leading}, {Pointer: "toc", Contents: &job.ContentsStyle{Title: "Contents", Size: size}}}}
	for i := range count {
		j.Parts = append(j.Parts, job.Part{Pointer: fmt.Sprintf("section%d", i), Title: fmt.Sprintf("Pielikums Ā ļ ķ Ελληνικά Кириллица %d", i), Items: []job.Part{{Pointer: fmt.Sprintf("page%d", i), Page: &style, Count: 1}}})
	}
	return j, map[string]*typeset.Font{"default": font}
}

func TestContentsCountDoesNotReflowAtTargetDigitTransitions(t *testing.T) {
	for _, leading := range []int{7, 97} {
		var heights []float64
		for delta := range 2 {
			j, fonts := contentsRecipe(t, leading+delta, 12, job.Size{Width: 595, Height: 842})
			c, ds := prepareContents(t.Context(), j, nil, fonts)
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			layout, ds := job.Resolve(j, nil, len(c.pages))
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
	authored, _ := job.ParseSize("A3", false)
	fit, _ := job.ParseSize("A5", false)
	j, fonts := contentsRecipe(t, 1, 80, authored)
	j.FitTo = &fit
	j.PageNumbers = &job.PageNumbers{Size: 9, Margin: 50 * 72 / 25.4}
	c, ds := prepareContents(t.Context(), j, nil, fonts)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(c.pages) < 2 || len(c.pages) > 80 {
		t.Fatalf("contents count%d", len(c.pages))
	}
	digits, err := typeset.NewDigits(fonts["default"], 9)
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
	if _, ds = prepareContents(t.Context(), j, nil, fonts); len(ds) == 0 || ds[0].Code != "text_overflow" {
		t.Fatalf("impossible footer space accepted: %+v", ds)
	}
}

func TestContentsRefusesHeadingInkAndWholeRowOverflow(t *testing.T) {
	j, fonts := contentsRecipe(t, 1, 1, job.Size{Width: 109.152, Height: 200})
	j.Parts[1].Contents.Title = "ffffff"
	if _, ds := prepareContents(t.Context(), j, nil, fonts); len(ds) == 0 || ds[0].Code != "text_overflow" {
		t.Fatalf("heading ink margin was ignored: %+v", ds)
	}
	j, fonts = contentsRecipe(t, 1, 1, job.Size{Width: 200, Height: 150})
	j.Parts[2].Title = strings.Repeat("Long wrapped title ", 20)
	if _, ds := prepareContents(t.Context(), j, nil, fonts); len(ds) == 0 || ds[0].Code != "text_overflow" {
		t.Fatalf("oversized whole row accepted: %+v", ds)
	}
	nested := j.Parts[2]
	for i := range 20 {
		nested = job.Part{Pointer: fmt.Sprintf("ancestor%d", i), Title: "Ancestor", Items: []job.Part{nested}}
	}
	j.Parts[2] = nested
	if _, ds := prepareContents(t.Context(), j, nil, fonts); len(ds) == 0 {
		t.Fatal("unusable indented column accepted")
	}
	j.Parts = j.Parts[:1]
	if c, ds := prepareContents(t.Context(), j, nil, fonts); c != nil || len(ds) > 0 {
		t.Fatalf("absent contents must mean C=0: %+v %+v", c, ds)
	}
}
