// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Package outcome defines complete factual execution outcomes.
package outcome

import (
	"slices"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// Output records publication separately from trailing errors.
type Output struct {
	Path      string
	Published bool
	SHA256    string
	Bytes     *int64
}

// Pages keeps counts null until the corresponding facts are known.
type Pages struct {
	Source    *int64
	Generated *int64
	Total     *int64
}

// Report records actual report-path files.
type Report struct {
	Path    string
	Written bool
}

// Text records authored text layout and measured overflow.
type Text struct {
	Font      string
	FontSize  float64
	Bounds    [4]float64
	InkBounds *[4]float64
	Overflow  bool
}

// Span maps an inclusive original-source range to its physical output range.
type Span struct {
	Source [2]int
	Output [2]int64
}

// Section records declared identity separately from resolved navigation/ranges.
type Section struct {
	Item        string
	Title       string
	Parent      *string
	TargetPage  *int64
	OutputPages *[2]int64
}

// Part is one compact declared or resolved leaf in the complete report.
type Part struct {
	Item          string
	InputPages    *int64
	Spans         *[]Span
	Section       string
	Reason        string
	BeforeItem    string
	Kind          packet.PartKind
	Path          string
	SHA256        string
	Bytes         *int64
	Pages         *int64
	Count         int
	OutputPages   *[2]int64
	Size          *packet.Size
	Text          *Text
	FieldsRenamed []packet.FieldRename
}

// Result is the complete run outcome before stdout caps.
type Result struct {
	Metadata  *packet.Metadata
	Timestamp *int64
	Structure *Structure
	Numbering *[]packet.NumberSpan

	Command     string
	Status      diag.Status
	Output      *Output
	Pages       *Pages
	FitTo       *packet.Size
	PageNumbers *packet.PageNumbers
	Diagnostics []diag.Diagnostic

	Report   *Report
	Parts    []Part
	Sections []Section
}

// Structure records policy and semantic coverage, not PDF/UA certification.
type Structure struct {
	Policy   packet.StructurePolicy
	Coverage *string
}

// New starts a result with unknown facts and no diagnostics.
func New(command string) Result {
	return Result{Command: command, Status: "ok", Diagnostics: []diag.Diagnostic{}}
}

// Full normalizes diagnostics without caps and always includes report parts.
func (r Result) Full() Result {
	r.Diagnostics = coalesce(r.Diagnostics)
	r.Status = diag.Outcome(r.Diagnostics)
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
		file                        string
		identity                    diag.FindingIdentity
		location                    diag.Location
		pdfLocation                 diag.PDFLocation
		hasLocation, hasPDFLocation bool
	}
	groups := map[findingKey]int{}
	seen := map[findingKey]map[string]bool{}
	for _, d := range input {
		d.Items = slices.Clone(d.Items)
		d.ItemsOmitted = 0
		if d.Severity != "warning" || d.File == "" || d.Identity.Kind == "" {
			output = append(output, d)
			continue
		}
		key := findingKey{code: d.Code, file: d.File, identity: d.Identity}
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
