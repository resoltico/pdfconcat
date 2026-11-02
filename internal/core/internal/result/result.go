// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package result defines result v6 and its complete factual outcomes.
package result

import (
	_ "embed"
	"slices"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// Schema covers result v6, full reports and version objects.
//
//go:embed result.schema.json
var Schema []byte

// WireVersion is the result and version wire contract.
const WireVersion = 6

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

// Part is one compact declared or resolved leaf in the complete report.
type Part struct {
	Item          string               `json:"item"`
	InputPages    *int64               `json:"input_pages"`
	Spans         *[]Span              `json:"spans"`
	Section       string               `json:"section,omitempty"`
	Reason        string               `json:"reason,omitempty"`
	BeforeItem    string               `json:"before_item,omitempty"`
	Kind          packet.PartKind      `json:"kind"`
	Path          string               `json:"path,omitempty"`
	SHA256        string               `json:"sha256,omitempty"`
	Bytes         *int64               `json:"bytes,omitempty"`
	Pages         *int64               `json:"pages"`
	Count         int                  `json:"count,omitzero"`
	OutputPages   *[2]int64            `json:"output_pages"`
	Size          *packet.Size         `json:"size,omitempty"`
	Text          *Text                `json:"text"`
	FieldsRenamed []packet.FieldRename `json:"fields_renamed,omitzero"`
}

// Result is the complete run outcome before stdout caps.
type Result struct {
	FormatVersion      int                 `json:"format_version"`
	Command            string              `json:"command"`
	Status             diag.Status         `json:"status"`
	Output             *Output             `json:"output"`
	Pages              *Pages              `json:"pages"`
	FitTo              *packet.Size        `json:"fit_to"`
	PageNumbers        *packet.PageNumbers `json:"page_numbers"`
	Diagnostics        []diag.Diagnostic   `json:"diagnostics"`
	DiagnosticsOmitted int                 `json:"diagnostics_omitted"`
	Report             *Report             `json:"report"`
	Parts              []Part              `json:"parts,omitzero"`
	Sections           []Section           `json:"sections,omitzero"`
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

// New starts a result with unknown facts and no diagnostics.
func New(command string) Result {
	return Result{FormatVersion: WireVersion, Command: command, Status: "ok", Diagnostics: []diag.Diagnostic{}}
}

// Full normalizes diagnostics without caps and always includes report parts.
func (r Result) Full() Result {
	r.Diagnostics = coalesce(r.Diagnostics)
	r.Status, _ = diag.Outcome(r.Diagnostics)
	r.DiagnosticsOmitted = 0
	if r.Parts == nil {
		r.Parts = []Part{}
	}
	if r.Sections == nil {
		r.Sections = []Section{}
	}
	return r
}

func coalesce(input []diag.Diagnostic) []diag.Diagnostic {
	output := make([]diag.Diagnostic, 0, len(input))
	type findingKey struct {
		code                        diag.Code
		file, message, detail       string
		location                    diag.Location
		pdfLocation                 diag.PDFLocation
		hasLocation, hasPDFLocation bool
	}
	groups := map[findingKey]int{}
	seen := map[findingKey]map[string]bool{}
	for _, d := range input {
		d.Items = slices.Clone(d.Items)
		d.ItemsOmitted = 0
		if d.Severity != "warning" || d.File == "" {
			output = append(output, d)
			continue
		}
		key := findingKey{code: d.Code, file: d.File, message: d.Message, detail: d.Detail}
		if d.Location != nil {
			key.location = *d.Location
			key.hasLocation = true
		}
		if d.PDFLocation != nil {
			key.pdfLocation = *d.PDFLocation
			key.hasPDFLocation = true
		}
		index, exists := groups[key]
		if !exists {
			index = len(output)
			groups[key] = index
			seen[key] = map[string]bool{}
			output = append(output, d)
			output[index].Items = nil
		}
		for _, item := range d.Items {
			if !seen[key][item] {
				seen[key][item] = true
				output[index].Items = append(output[index].Items, item)
			}
		}
	}
	slices.SortStableFunc(output, func(a, b diag.Diagnostic) int {
		return diag.Priority(b) - diag.Priority(a)
	})
	return output
}
