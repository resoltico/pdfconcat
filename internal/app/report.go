// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// report describes a finished or previewed assembly.
type report struct {
	Output      string       `json:"output"`
	DryRun      bool         `json:"dryRun"`
	Pages       int          `json:"pages"`
	SourcePages int          `json:"sourcePages"`
	BlankPages  int          `json:"blankPages"`
	Parts       []reportPart `json:"parts,omitempty"`
}

// reportPart is one ordered contribution to the output.
type reportPart struct {
	Type      string       `json:"type"`
	FirstPage int          `json:"firstPage"`
	Pages     int          `json:"pages"`
	Path      string       `json:"path,omitempty"`
	Blank     *reportBlank `json:"blank,omitempty"`
}

// reportBlank describes a resolved blank page style.
type reportBlank struct {
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	Background string  `json:"background,omitempty"`
	Text       string  `json:"text,omitempty"`
}

func newCreatedReport(output string, layout *assembly.Layout) report {
	return report{
		Output:      output,
		Pages:       layout.TotalPages,
		SourcePages: layout.SourcePages,
		BlankPages:  layout.BlankPages,
	}
}

func newDryRunReport(output string, layout *assembly.Layout) report {
	summary := newCreatedReport(output, layout)
	summary.DryRun = true
	summary.Parts = make([]reportPart, 0, len(layout.Parts))

	for index := range layout.Parts {
		summary.Parts = append(summary.Parts, newReportPart(&layout.Parts[index]))
	}

	return summary
}

func newReportPart(part *assembly.Part) reportPart {
	entry := reportPart{FirstPage: part.FirstPage, Pages: part.Pages}

	switch part.Kind {
	case assembly.PDF:
		entry.Type, entry.Path = "pdf", part.Path
	case assembly.Blank:
		entry.Type = "blank"
		entry.Blank = &reportBlank{
			Width:  float64(part.Blank.Dim.Width),
			Height: float64(part.Blank.Dim.Height),
			Text:   part.Blank.Text.Value,
		}

		if part.Blank.Background.IsSet() {
			entry.Blank.Background = part.Blank.Background.OrElse(assembly.Color{}).String()
		}
	default:
		entry.Type = "unknown"
	}

	return entry
}

// report writes the report to stdout as JSON or text.
func (r *Runner) report(summary report, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(r.streams.Stdout)
		encoder.SetIndent("", "  ")

		err := encoder.Encode(summary)
		if err != nil {
			return fmt.Errorf("write report: %w", err)
		}

		return nil
	}

	_, err := fmt.Fprint(r.streams.Stdout, summary.text())
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	return nil
}

func (s report) text() string {
	var out strings.Builder

	if !s.DryRun {
		fmt.Fprintf(&out, "Created %s (%d pages; %d generated blank page(s)).\n", s.Output, s.Pages, s.BlankPages)
		return out.String()
	}

	width := len(strconv.Itoa(s.Pages))
	fmt.Fprintf(&out, "Output: %s\n", s.Output)

	for _, part := range s.Parts {
		pages := strconv.Itoa(part.FirstPage)
		if part.Pages > 1 {
			pages += "-" + strconv.Itoa(part.FirstPage+part.Pages-1)
		}

		fmt.Fprintf(&out, "%*s%-*s  %s\n", width, "", 2*width+1, pages, part.describe())
	}

	fmt.Fprintf(&out, "Total: %d pages (%d from PDFs, %d generated blank).\n", s.Pages, s.SourcePages, s.BlankPages)
	out.WriteString("Dry run: no output created.\n")

	return out.String()
}

func (p reportPart) describe() string {
	if p.Type == "pdf" {
		return fmt.Sprintf("%s (%d pages)", p.Path, p.Pages)
	}

	description := fmt.Sprintf("blank %.0fx%.0fpt", p.Blank.Width, p.Blank.Height)
	if p.Blank.Background != "" {
		description += " on " + p.Blank.Background
	}

	if p.Blank.Text != "" {
		description += " " + strconv.Quote(p.Blank.Text)
	}

	return description
}
