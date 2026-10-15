// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package result defines result v3 and its full-report and capped-stdout views.
package result

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"io"
	"slices"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
)

// Schema covers result v3, full reports and version objects.
//
//go:embed result.schema.json
var Schema []byte

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

// Report records actual report-path publication.
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

// Part is the full-report entry for one flattened item.
type Part struct {
	Item          string            `json:"item"`
	Kind          string            `json:"kind"`
	Path          string            `json:"path,omitempty"`
	SHA256        string            `json:"sha256,omitempty"`
	Bytes         *int64            `json:"bytes,omitempty"`
	Pages         *int64            `json:"pages"`
	Count         int               `json:"count,omitzero"`
	OutputPages   *[2]int64         `json:"output_pages"`
	Size          *job.Size         `json:"size,omitempty"`
	Text          *Text             `json:"text"`
	FieldsRenamed []job.FieldRename `json:"fields_renamed,omitzero"`
}

// Result is the complete run outcome before stdout caps.
type Result struct {
	FormatVersion      int               `json:"format_version"`
	Command            string            `json:"command"`
	Status             string            `json:"status"`
	Output             *Output           `json:"output"`
	Pages              *Pages            `json:"pages"`
	FitTo              *job.Size         `json:"fit_to"`
	Diagnostics        []diag.Diagnostic `json:"diagnostics"`
	DiagnosticsOmitted int               `json:"diagnostics_omitted"`
	Report             *Report           `json:"report"`
	Parts              []Part            `json:"parts,omitzero"`
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
	return Result{FormatVersion: 3, Command: command, Status: "ok", Diagnostics: []diag.Diagnostic{}}
}

// Full normalizes diagnostics without caps and always includes report parts.
func (r Result) Full() Result {
	r.Diagnostics = coalesce(r.Diagnostics)
	r.Status, _ = diag.Outcome(r.Diagnostics)
	r.DiagnosticsOmitted = 0
	if r.Parts == nil {
		r.Parts = []Part{}
	}
	return r
}

// Summary omits parts and bounds both diagnostics and their consumer pointers.
func (r Result) Summary() Result {
	r = r.Full()
	r.Parts = nil
	if len(r.Diagnostics) > 20 {
		r.DiagnosticsOmitted = len(r.Diagnostics) - 20
		r.Diagnostics = r.Diagnostics[:20]
	}
	for i := range r.Diagnostics {
		if len(r.Diagnostics[i].Items) > 20 {
			r.Diagnostics[i].ItemsOmitted = len(r.Diagnostics[i].Items) - 20
			r.Diagnostics[i].Items = r.Diagnostics[i].Items[:20]
		}
	}
	return r
}

func coalesce(input []diag.Diagnostic) []diag.Diagnostic {
	output := make([]diag.Diagnostic, 0, len(input))
	groups := map[string]int{}
	seen := map[string]map[string]bool{}
	for _, d := range input {
		d.Items = slices.Clone(d.Items)
		d.ItemsOmitted = 0
		if d.Severity != "warning" || d.File == "" {
			output = append(output, d)
			continue
		}
		key := string(d.Code) + "\x00" + d.File
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
		if a.Severity == b.Severity {
			return 0
		}
		if a.Severity == "error" {
			return -1
		}
		return 1
	})
	return output
}

// JSON writes one complete JSON document followed by a newline.
func JSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// Render writes the capped JSON or text stdout view.
func Render(w io.Writer, r Result, format string) error {
	r = r.Summary()
	if format != "text" {
		return JSON(w, r)
	}
	if _, err := fmt.Fprintf(w, "%s %s\n", r.Command, r.Status); err != nil {
		return err
	}
	if r.Pages != nil && r.Pages.Total != nil {
		if _, err := fmt.Fprintf(w, "Pages: %d\n", *r.Pages.Total); err != nil {
			return err
		}
	}
	if r.Output != nil {
		if _, err := fmt.Fprintf(w, "Output: %s (published: %t)\n", r.Output.Path, r.Output.Published); err != nil {
			return err
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(w, "%s %s: %s\n", d.Severity, d.Code, d.Message); err != nil {
			return err
		}
	}
	if r.DiagnosticsOmitted > 0 {
		if _, err := fmt.Fprintf(w, "%d more diagnostics; use --report for full details.\n", r.DiagnosticsOmitted); err != nil {
			return err
		}
	}
	if r.Report != nil {
		if _, err := fmt.Fprintf(w, "Report: %s (written: %t)\n", r.Report.Path, r.Report.Written); err != nil {
			return err
		}
	}
	return nil
}
