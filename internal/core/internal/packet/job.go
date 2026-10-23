// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet

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

// PageRange selects an inclusive range in the original source page order.
type PageRange struct {
	First, Last int
}

// ContentsStyle describes the fixed-format contents heading and authored canvas.
type ContentsStyle struct {
	Title string
	Size  Size // zero means fit target, or A4 without fitting
}

// PageNumbers configures physical Arabic footer numbers when present.
type PageNumbers struct {
	Size   float64 `json:"size"`
	Margin float64 `json:"margin"`
}

// Part is a declared source, generated page, contents item or semantic container.
type Part struct {
	Pointer        string
	Source         string
	Dir            string
	Selection      []PageRange
	Page           *PageStyle
	Count          int
	Contents       *ContentsStyle
	Items          []Part
	Title, StartOn string
	Divider        *PageStyle
}

// Job is an ordered declared packet with absolute input/output paths.
type Job struct {
	Output      string
	FitTo       *Size
	Parts       []Part
	PageNumbers *PageNumbers
}

// SourceFacts contains the selected count, boundaries and normalized ranges of one occurrence.
type SourceFacts struct {
	Pages               int
	FirstSize, LastSize Size
	Selection           []PageRange
}

// Leaf is a derived declared emission, with its innermost authored section.
type Leaf struct {
	Part
	Kind, Owner string
}

// SourceMapping associates one selected original range with its first output page.
type SourceMapping struct {
	SourceFirst, SourceLast int
	OutputFirst             int64
}

// PlacedPart is one compact physical span in the complete resolved packet.
type PlacedPart struct {
	Leaf
	Pages               int
	FirstPage, LastPage int64
	Size                Size
	Mapping             []SourceMapping
	Reason, BeforeItem  string
}

// Section identifies an authored container and its target and inclusive subtree range.
type Section struct {
	Item, Title, Parent         string
	Depth                       int
	FirstPage, LastPage, Target int64
}

// Layout is the authoritative physical sequence, section hierarchy and totals.
type Layout struct {
	Parts                                   []PlacedPart
	Sections                                []Section
	PageNumbers                             *PageNumbers
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

// FieldRename records a fully-qualified form field name before and after merging.
type FieldRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}
