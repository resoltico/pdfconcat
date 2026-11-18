// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/resoltico/pdfconcat/internal/app"
	result "github.com/resoltico/pdfconcat/internal/wire"
)

// Summary omits parts and bounds both diagnostics and their consumer pointers.
func Summary(input app.Result) result.Result {
	r := result.Project(input)
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

// Render writes a capped JSON document or a text summary with separate diagnostics.
func Render(w, stderr io.Writer, r app.Result, asJSON bool) error {
	if asJSON {
		return JSON(w, Summary(r))
	}
	rows, omitted := previewRows(r)
	summary := Summary(r)
	if _, err := fmt.Fprintf(w, "%s %s\n", summary.Command, summary.Status); err != nil {
		return err
	}
	if summary.Pages != nil && summary.Pages.Total != nil {
		if _, err := fmt.Fprintf(w, "Pages: %d\n", *summary.Pages.Total); err != nil {
			return err
		}
	}
	if summary.Output != nil {
		if _, err := fmt.Fprintf(w, "Output: %s (published: %t)\n", strconv.Quote(summary.Output.Path), summary.Output.Published); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
	}
	if omitted > 0 {
		if _, err := fmt.Fprintf(w, "%d more preview rows; %s\n", omitted, reportHint(summary.Report)); err != nil {
			return err
		}
	}
	for _, d := range summary.Diagnostics {
		if _, err := fmt.Fprintln(stderr, diagnosticText(d, summary.Report)); err != nil {
			return err
		}
	}
	if summary.DiagnosticsOmitted > 0 {
		if _, err := fmt.Fprintf(stderr, "%d more diagnostics; %s\n", summary.DiagnosticsOmitted, reportHint(summary.Report)); err != nil {
			return err
		}
	}
	if summary.Report != nil {
		if _, err := fmt.Fprintf(w, "Report: %s (written: %t)\n", strconv.Quote(summary.Report.Path), summary.Report.Written); err != nil {
			return err
		}
	}
	return nil
}

// reportHint describes access to uncapped details using actual report visibility.
func reportHint(report *result.Report) string {
	if report == nil {
		return "use --report for full details."
	}
	if report.Written {
		return "see the saved report for uncapped processing details."
	}
	return "full report unavailable; resolve the report failure before requesting it again."
}

// diagnosticText keeps each diagnostic on one terminal-safe line.
func diagnosticText(d app.Diagnostic, report *result.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %s", d.Severity, d.Code, printableMessage(d.Message))
	if location := d.PDFLocation; location != nil {
		if location.Object > 0 {
			fmt.Fprintf(&b, " [PDF object %d]", location.Object)
		}
		if location.Page > 0 {
			fmt.Fprintf(&b, " [PDF page %d]", location.Page)
		}
		if location.Path != "" {
			fmt.Fprintf(&b, " [PDF path %s]", strconv.Quote(location.Path))
		}
	}
	switch d.Code.String() {
	case "output_required":
		b.WriteString(" Use -o FILE or plan output.")
	case "page_size_required":
		b.WriteString(" Use --fit-to SIZE or an explicit plan page size.")
	}
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
			fmt.Fprintf(&b, "%d more; %s", d.ItemsOmitted, strings.TrimSuffix(reportHint(report), "."))
		}
		b.WriteByte(']')
	}
	if d.Detail != "" {
		fmt.Fprintf(&b, " [detail %s]", strconv.Quote(d.Detail))
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

func previewRows(r app.Result) ([]string, int) {
	report := result.Project(r).Report
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
		case app.PartSource:
			source := "unknown"
			if part.Spans != nil {
				mapping := *part.Spans
				var spans []string
				for _, span := range mapping[:min(len(mapping), 20)] {
					spans = append(spans, fmt.Sprintf("%d–%d", span.Source[0], span.Source[1]))
				}
				source = strings.Join(spans, ",")
				if len(mapping) > 20 {
					source += fmt.Sprintf(" (%d more spans; %s)", len(mapping)-20, strings.TrimSuffix(reportHint(report), "."))
				}
			}
			rows = append(rows, fmt.Sprintf("Source %s: selected %s → pages %s", strconv.Quote(part.Path), source, pages(part.OutputPages)))
		case app.PartPadding:
			rows = append(rows, fmt.Sprintf("Padding before %s: pages %s (odd_start)", strconv.Quote(part.BeforeItem), pages(part.OutputPages)))
		case app.PartPage, app.PartDivider, app.PartContents:
			rows = append(rows, fmt.Sprintf("%s %s: pages %s", part.Kind, strconv.Quote(part.Item), pages(part.OutputPages)))
		}
	}
	return rows, len(r.Sections) + len(r.Parts) - len(rows)
}
