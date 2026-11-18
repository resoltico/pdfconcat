// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Package result defines result JSON and its complete factual outcomes.
package result

import (
	_ "embed"
	"encoding/json/v2"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
)

// Schema covers results, full reports and version objects.
//
//go:embed result.schema.json
var Schema []byte

// WireVersion is the result and version wire contract.
const WireVersion = 10

// Output records publication separately from trailing errors.
type Output struct {
	Path      string `json:"path"`
	Published bool   `json:"published"`
	SHA256    string `json:"sha256,omitempty"`
	Bytes     *int64 `json:"bytes,omitempty"`
}

// Pages keeps counts null until the corresponding facts are known.
type Pages struct {
	Source    *int64 `json:"source"`
	Generated *int64 `json:"generated"`
	Total     *int64 `json:"total"`
}

// Report records actual report-path files.
type Report struct {
	Path    string `json:"path"`
	Written bool   `json:"written"`
}

// Text records authored text layout and measured overflow.
type Text struct {
	Font      string      `json:"font"`
	FontSize  float64     `json:"font_size"`
	Bounds    [4]float64  `json:"bounds"`
	InkBounds *[4]float64 `json:"ink_bounds"`
	Overflow  bool        `json:"overflow"`
}

// Span maps an inclusive original-source range to its physical output range.
type Span struct {
	Source [2]int   `json:"source"`
	Output [2]int64 `json:"output"`
}

// Section records declared identity separately from resolved navigation/ranges.
type Section struct {
	Item        string    `json:"item"`
	Title       string    `json:"title"`
	Parent      *string   `json:"parent"`
	TargetPage  *int64    `json:"target_page"`
	OutputPages *[2]int64 `json:"output_pages"`
}

// PageSizeRun records original selected dimensions and resolved expected output.
type PageSizeRun struct {
	Source     [2]int      `json:"source"`
	SourceSize packet.Size `json:"source_size"`
	OutputSize packet.Size `json:"output_size"`
}

// Part is one compact declared or resolved leaf in the complete report.
type Part struct {
	Item          string          `json:"item"`
	InputPages    *int64          `json:"input_pages"`
	Spans         *[]Span         `json:"spans"`
	Section       string          `json:"section,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	BeforeItem    string          `json:"before_item,omitempty"`
	Kind          packet.PartKind `json:"kind"`
	Path          string          `json:"path,omitempty"`
	SHA256        string          `json:"sha256,omitempty"`
	Bytes         *int64          `json:"bytes,omitempty"`
	Pages         *int64          `json:"pages"`
	Count         int             `json:"count,omitzero"`
	OutputPages   *[2]int64       `json:"output_pages"`
	Size          *packet.Size    `json:"size,omitempty"`
	OutputSize    *packet.Size    `json:"output_size"`
	Text          *Text           `json:"text"`
	PageSizes     *[]PageSizeRun  `json:"page_sizes"`
	FieldsRenamed RenameFacts     `json:"fields_renamed"`
}

// Fitting is the effective policy in canonical physical points.
type Fitting struct {
	Target    packet.Size `json:"target"`
	Tolerance float64     `json:"tolerance"`
}

// Result is the complete run outcome before stdout caps.
type Result struct {
	Metadata           *packet.Metadata     `json:"metadata"`
	Timestamp          *int64               `json:"timestamp"`
	Structure          *Structure           `json:"structure"`
	Numbering          *[]packet.NumberSpan `json:"numbering"`
	FormatVersion      int                  `json:"format_version"`
	Command            string               `json:"command"`
	Status             diag.Status          `json:"status"`
	Output             *Output              `json:"output"`
	Pages              *Pages               `json:"pages"`
	Fitting            *Fitting             `json:"fitting"`
	PageNumbers        *packet.PageNumbers  `json:"page_numbers"`
	Diagnostics        []diag.Diagnostic    `json:"diagnostics"`
	DiagnosticsOmitted int                  `json:"diagnostics_omitted"`
	Report             *Report              `json:"report"`
	Parts              []Part               `json:"parts,omitzero"`
	Sections           []Section            `json:"sections,omitzero"`
}

// Structure records policy and semantic coverage, not PDF/UA certification.
type Structure struct {
	Policy   packet.StructurePolicy `json:"policy"`
	Coverage *string                `json:"coverage"`
}

// Version identifies the executable and its build environment.
type Version struct {
	FormatVersion int    `json:"format_version"`
	Command       string `json:"command"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	Go            string `json:"go"`
	Platform      string `json:"platform"`
}

// Project serializes domain facts without making a wire DTO the execution state.
func Project(input outcome.Result) Result {
	r := input.Full()
	result := Result{FormatVersion: WireVersion, Metadata: r.Metadata, Timestamp: r.Timestamp, Numbering: r.Numbering, Command: r.Command, Status: r.Status, PageNumbers: r.PageNumbers, Diagnostics: r.Diagnostics, Parts: []Part{}, Sections: []Section{}}
	if r.Fitting != nil {
		result.Fitting = &Fitting{Target: r.Fitting.Target, Tolerance: r.Fitting.Tolerance}
	}
	if r.Output != nil {
		result.Output = &Output{Path: r.Output.Path, Published: r.Output.Published, SHA256: r.Output.SHA256, Bytes: r.Output.Bytes}
	}
	if r.Report != nil {
		result.Report = &Report{Path: r.Report.Path, Written: r.Report.Written}
	}
	if r.Pages != nil {
		result.Pages = &Pages{Source: r.Pages.Source, Generated: r.Pages.Generated, Total: r.Pages.Total}
	}
	if r.Structure != nil {
		result.Structure = &Structure{Policy: r.Structure.Policy, Coverage: r.Structure.Coverage}
	}
	for _, part := range r.Parts {
		p := Part{Item: part.Item, InputPages: part.InputPages, Section: part.Section, Reason: part.Reason, BeforeItem: part.BeforeItem, Kind: part.Kind, Path: part.Path, SHA256: part.SHA256, Bytes: part.Bytes, Pages: part.Pages, Count: part.Count, OutputPages: part.OutputPages, Size: part.Size, OutputSize: part.OutputSize, FieldsRenamed: RenameFacts(part.FieldsRenamed)}
		if part.PageSizes != nil {
			runs := make([]PageSizeRun, len(*part.PageSizes))
			for i, r := range *part.PageSizes {
				runs[i] = PageSizeRun{Source: r.Source, SourceSize: r.SourceSize, OutputSize: r.OutputSize}
			}
			p.PageSizes = &runs
		}
		if part.Text != nil {
			p.Text = &Text{Font: part.Text.Font, FontSize: part.Text.FontSize, Bounds: part.Text.Bounds, InkBounds: part.Text.InkBounds, Overflow: part.Text.Overflow}
		}
		if part.Spans != nil {
			spans := make([]Span, len(*part.Spans))
			for index, span := range *part.Spans {
				spans[index] = Span{Source: span.Source, Output: span.Output}
			}
			p.Spans = &spans
		}
		result.Parts = append(result.Parts, p)
	}
	for _, section := range r.Sections {
		result.Sections = append(result.Sections, Section{Item: section.Item, Title: section.Title, Parent: section.Parent, TargetPage: section.TargetPage, OutputPages: section.OutputPages})
	}
	return result
}

// RenameFacts distinguishes unknown/null from an observed empty array.
type RenameFacts []packet.FieldRename

// MarshalJSON preserves the factual null/empty distinction across JSON implementations.
func (r RenameFacts) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return json.Marshal([]packet.FieldRename(r))
}
