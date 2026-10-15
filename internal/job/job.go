// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package job

import "github.com/resoltico/pdfconcat/internal/diag"

// TextStyle is the effective generated text declaration in points.
type TextStyle struct {
	Value, Font          string
	Size                 float64
	Color                Color
	Anchor               string
	X, Y, Width, Leading float64
	Align, Overflow      string
}

// PageStyle combines an authored canvas, fill, text and declaration origins.
type PageStyle struct {
	Size       Size // zero means inherit
	Background *Color
	Text       TextStyle
	Origins    map[string]string
}

// Part is one flattened source occurrence or generated item.
type Part struct {
	Pointer string
	Source  string
	Page    *PageStyle
	Count   int
}

// Job is an ordered packet with absolute input/output paths.
type Job struct {
	Output  string
	FitTo   *Size
	Parts   []Part
	BaseDir string
}

// SourceFacts contains inspected page counts and displayed boundary sizes.
type SourceFacts struct {
	Pages               int
	FirstSize, LastSize Size
}

// PlacedPart has its resolved canvas and inclusive output range.
type PlacedPart struct {
	Part
	Pages               int
	FirstPage, LastPage int64
	Size                Size
}

// Layout is the resolved ordered packet and its page totals.
type Layout struct {
	Parts                                   []PlacedPart
	SourcePages, GeneratedPages, TotalPages int64
}

// Defaults returns the built-in generated-page style.
func Defaults() PageStyle {
	return PageStyle{Text: TextStyle{Font: "default", Size: 12, Anchor: "center", Align: "center", Leading: 1.2, Overflow: "error"}, Origins: map[string]string{}}
}

// Layer changes only declared fields, including explicit empty text and no background.
func Layer(base, override PageStyle) PageStyle {
	result := base
	result.Origins = make(map[string]string, len(base.Origins)+len(override.Origins))
	for k, v := range base.Origins {
		result.Origins[k] = v
	}
	for k, v := range override.Origins {
		result.Origins[k] = v
		switch k {
		case "size":
			result.Size = override.Size
		case "background":
			result.Background = override.Background
		case "value":
			result.Text.Value = override.Text.Value
		case "font":
			result.Text.Font = override.Text.Font
		case "font_size":
			result.Text.Size = override.Text.Size
		case "color":
			result.Text.Color = override.Text.Color
		case "anchor":
			result.Text.Anchor = override.Text.Anchor
		case "x":
			result.Text.X = override.Text.X
		case "y":
			result.Text.Y = override.Text.Y
		case "width":
			result.Text.Width = override.Text.Width
		case "align":
			result.Text.Align = override.Text.Align
		case "leading":
			result.Text.Leading = override.Text.Leading
		case "overflow":
			result.Text.Overflow = override.Text.Overflow
		}
	}
	return result
}

// Resolve computes page ranges and generated canvases from inspected source facts.
func Resolve(j Job, facts map[string]SourceFacts) (Layout, []diag.Diagnostic) {
	var layout Layout
	var diagnostics []diag.Diagnostic
	var previous Size
	for _, part := range j.Parts {
		if part.Source != "" {
			if f, ok := facts[part.Source]; ok {
				previous = f.FirstSize
				break
			}
		}
	}
	var total int64
	for _, part := range j.Parts {
		placed := PlacedPart{Part: part, FirstPage: total + 1}
		if part.Page == nil {
			f, ok := facts[part.Source]
			if !ok || f.Pages < 1 {
				diagnostics = append(diagnostics, diag.New("source_invalid", "Source page facts are unavailable.", part.Pointer))
				continue
			}
			placed.Pages = f.Pages
			layout.SourcePages += int64(f.Pages)
			previous = f.LastSize
		} else {
			placed.Pages = part.Count
			if placed.Pages < 1 || placed.Pages > MaxGeneratedPages || layout.GeneratedPages+int64(placed.Pages) > MaxGeneratedPages {
				diagnostics = append(diagnostics, diag.New("plan_limit", "Generated pages exceed 100000.", part.Pointer))
				continue
			}
			placed.Size = part.Page.Size
			if placed.Size == (Size{}) {
				placed.Size = previous
				if j.FitTo != nil {
					placed.Size = *j.FitTo
				}
				switch {
				case placed.Size == (Size{}):
					diagnostics = append(diagnostics, diag.New("page_size_required", "Give an explicit page size when no source is available.", part.Pointer))
				case placed.Size.Width < 1 || placed.Size.Height < 1:
					diagnostics = append(diagnostics, diag.New("plan_value", "Inherited generated-page sides must be at least 1 point; give an explicit size.", part.Pointer))
				case placed.Size.Width > MaxSide || placed.Size.Height > MaxSide:
					diagnostics = append(diagnostics, diag.New("page_size_too_large", "Inherited page exceeds 14400 points; give an explicit size.", part.Pointer))
				}
			}
			layout.GeneratedPages += int64(placed.Pages)
		}
		total += int64(placed.Pages)
		placed.LastPage = total
		layout.Parts = append(layout.Parts, placed)
	}
	layout.TotalPages = total
	return layout, diagnostics
}

// FieldRename records a fully-qualified form field name before and after merging.
type FieldRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}
