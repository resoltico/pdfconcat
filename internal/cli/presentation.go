// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/resoltico/pdfconcat/internal/core"
)

// Summary omits parts and bounds both diagnostics and their consumer pointers.
func Summary(r core.Result) core.Result {
	r = r.Full()
	r.Parts = nil
	r.Sections = nil
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

// JSON writes a complete JSON document and newline.
func JSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// Render writes the capped JSON or text stdout view.
func Render(w io.Writer, r core.Result, format string) error {
	if format != "text" {
		return JSON(w, Summary(r))
	}
	rows, omitted := previewRows(r)
	r = Summary(r)
	if _, err := fmt.Fprintf(w, "%s %s\n", r.Command, r.Status); err != nil {
		return err
	}
	if r.Pages != nil && r.Pages.Total != nil {
		if _, err := fmt.Fprintf(w, "Pages: %d\n", *r.Pages.Total); err != nil {
			return err
		}
	}
	if r.Output != nil {
		if _, err := fmt.Fprintf(w, "Output: %s (published: %t)\n", strconv.Quote(r.Output.Path), r.Output.Published); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
	}
	if omitted > 0 {
		if _, err := fmt.Fprintf(w, "%d more preview rows; use --report for full details.\n", omitted); err != nil {
			return err
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintln(w, diagnosticText(d)); err != nil {
			return err
		}
	}
	if r.DiagnosticsOmitted > 0 {
		if _, err := fmt.Fprintf(w, "%d more diagnostics; use --report for full details.\n", r.DiagnosticsOmitted); err != nil {
			return err
		}
	}
	if r.Report != nil {
		if _, err := fmt.Fprintf(w, "Report: %s (written: %t)\n", strconv.Quote(r.Report.Path), r.Report.Written); err != nil {
			return err
		}
	}
	return nil
}

// diagnosticText keeps each diagnostic on one terminal-safe line.
func diagnosticText(d core.Diagnostic) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %s", d.Severity, d.Code, printableMessage(d.Message))
	file := d.File
	if d.Location != nil && d.Location.File != "" {
		file = d.Location.File
	}
	if file != "" {
		fmt.Fprintf(&b, " [file %s", strconv.Quote(file))
		if d.Location != nil && d.Location.Line > 0 {
			fmt.Fprintf(&b, ":%d:%d", d.Location.Line, d.Location.Column)
		}
		b.WriteByte(']')
	}
	pointer := ""
	if d.Location != nil && d.Location.Ref != "" {
		pointer = d.Location.Ref
		fmt.Fprintf(&b, " [pointer %s]", strconv.Quote(pointer))
	}
	var items []string
	for _, item := range d.Items {
		if item != pointer {
			items = append(items, strconv.Quote(item))
		}
	}
	if len(items) > 0 || d.ItemsOmitted > 0 {
		b.WriteString(" [items ")
		b.WriteString(strings.Join(items, ", "))
		if d.ItemsOmitted > 0 {
			if len(items) > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%d more; use --report for full details", d.ItemsOmitted)
		}
		b.WriteByte(']')
	}
	return b.String()
}

func printableMessage(message string) string {
	var b strings.Builder
	for _, r := range message {
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		} else {
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		}
	}
	return b.String()
}

func previewRows(r core.Result) ([]string, int) {
	var rows []string
	pages := func(span *[2]int64) string {
		if span == nil {
			return "unknown"
		}
		return fmt.Sprintf("%d–%d", span[0], span[1])
	}
	for _, section := range r.Sections {
		if len(rows) == 20 {
			break
		}
		target := "unknown"
		if section.TargetPage != nil {
			target = strconv.FormatInt(*section.TargetPage, 10)
		}
		rows = append(rows, fmt.Sprintf("Section %s: target %s, pages %s", strconv.Quote(section.Title), target, pages(section.OutputPages)))
	}
	for _, part := range r.Parts {
		if len(rows) == 20 {
			break
		}
		switch part.Kind {
		case core.PartSource:
			source := "unknown"
			if part.Spans != nil {
				mapping := *part.Spans
				var spans []string
				for _, span := range mapping[:min(len(mapping), 20)] {
					spans = append(spans, fmt.Sprintf("%d–%d", span.Source[0], span.Source[1]))
				}
				source = strings.Join(spans, ",")
				if len(mapping) > 20 {
					source += fmt.Sprintf(" (%d more spans; use --report)", len(mapping)-20)
				}
			}
			rows = append(rows, fmt.Sprintf("Source %s: selected %s → pages %s", strconv.Quote(part.Path), source, pages(part.OutputPages)))
		case core.PartPadding:
			rows = append(rows, fmt.Sprintf("Padding before %s: pages %s (odd_start)", strconv.Quote(part.BeforeItem), pages(part.OutputPages)))
		case core.PartPage, core.PartDivider, core.PartContents:
			rows = append(rows, fmt.Sprintf("%s %s: pages %s", part.Kind, strconv.Quote(part.Item), pages(part.OutputPages)))
		}
	}
	return rows, len(r.Sections) + len(r.Parts) - len(rows)
}
