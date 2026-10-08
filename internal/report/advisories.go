// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// DiagnosticCounts counts captured diagnostic records after aggregation, never pages or consumers.
type DiagnosticCounts struct {
	DiagnosticCount int `json:"diagnostic_count"`
	ErrorCount      int `json:"error_count"`
	WarningCount    int `json:"warning_count"`
}

// FinalizeDiagnostics assigns construction defaults and derives counts/context for a captured snapshot.
// It is for controlled report production, never decoding or repairing untrusted wire evidence.
func (r *Report) FinalizeDiagnostics() {
	for i := range r.Diagnostics {
		diagnostic := &r.Diagnostics[i]
		if diagnostic.Severity == "" {
			diagnostic.Severity = SeverityError
		}

		if diagnostic.Severity == SeverityWarning {
			diagnostic.ConsequenceContext = consequenceContext(r.Publication)
		}
	}

	slices.SortStableFunc(r.Diagnostics, func(left, right Diagnostic) int { return cmp.Compare(left.Severity, right.Severity) })

	counts := diagnosticCounts(r.Diagnostics)
	r.DiagnosticCount, r.ErrorCount, r.WarningCount = counts.DiagnosticCount, counts.ErrorCount, counts.WarningCount
}

func diagnosticCounts(records []Diagnostic) DiagnosticCounts {
	counts := DiagnosticCounts{DiagnosticCount: len(records)}
	for i := range records {
		record := &records[i]
		switch record.Severity {
		case SeverityWarning:
			counts.WarningCount++
		case SeverityError:
			counts.ErrorCount++
		default:
		}
	}

	return counts
}

// layoutWarnings projects the stored measurements once per shared resolved style.
func layoutWarnings(snapshot *Report) []Diagnostic {
	consumers := make(map[int][]string)
	first := make(map[int]*Part)

	for i := range snapshot.Parts {
		part := &snapshot.Parts[i]
		if part.Style != nil {
			style := *part.Style

			consumers[style] = append(consumers[style], part.ID)
			if first[style] == nil {
				first[style] = part
			}
		}
	}

	warnings := make([]Diagnostic, 0, len(snapshot.Styles))

	for i := range snapshot.Styles {
		text := snapshot.Styles[i].Text
		if text == nil || text.Overflow != textOverflowAllow || len(text.Findings) == 0 || first[i] == nil {
			continue
		}

		location := partLocation(first[i])
		warnings = append(warnings, Diagnostic{
			Severity: SeverityWarning, Stage: Stage(phaseLayout), Code: "generated_text_overflow",
			Location: location, Consumers: consumers[i],
			Message: "Allowed text placement has measured overflow: " + text.Findings[0].Detail +
				". Adjust anchor, offset, width, text or size, or retain this placement intentionally; inspect the style findings.",
			Recovery: &Recovery{Action: "edit_input", Location: location},
		})
	}

	return warnings
}

func partLocation(part *Part) *Location {
	location := &Location{
		File: part.Origin.File, Offset: part.Origin.Offset,
		Line: part.Origin.Line, Column: part.Origin.Column, Pointer: part.ID,
	}
	if argument, ok := strings.CutPrefix(part.ID, "argv:"); ok {
		index, err := strconv.Atoi(argument)
		if err == nil {
			location.Pointer, location.ArgvIndex = "", &index
		}
	}

	return location
}

func (r *Report) relevantCounts(ids []string) DiagnosticCounts {
	counts := DiagnosticCounts{}

	for i := range r.Diagnostics {
		diagnostic := &r.Diagnostics[i]
		if !diagnosticRefersTo(diagnostic, ids) {
			continue
		}

		counts.DiagnosticCount++
		if diagnostic.Severity == SeverityWarning {
			counts.WarningCount++
		} else {
			counts.ErrorCount++
		}
	}

	return counts
}

func diagnosticRefersTo(d *Diagnostic, ids []string) bool {
	for _, id := range ids {
		if slices.Contains(d.Consumers, id) {
			return true
		}

		if d.Location != nil && d.Location.Pointer == id {
			return true
		}

		if d.Location != nil && d.Location.ArgvIndex != nil && id == "argv:"+strconv.Itoa(*d.Location.ArgvIndex) {
			return true
		}
	}

	return false
}

func selectedDiagnosticCounts(saved *Report, response Response) DiagnosticCounts {
	switch content := response.content.(type) {
	case *Summary:
		return diagnosticCounts(saved.Diagnostics)
	case *PartResponse:
		return saved.relevantCounts([]string{content.Part.ID})
	case *PageResponse:
		return saved.relevantCounts([]string{content.Part.ID})
	case *ViewResponse[PartView]:
		ids := make([]string, len(content.Records))
		for i := range content.Records {
			ids[i] = content.Records[i].ID
		}

		return saved.relevantCounts(ids)
	case *ViewResponse[DiagnosticView]:
		counts := DiagnosticCounts{DiagnosticCount: len(content.Records)}
		for i := range content.Records {
			record := &content.Records[i]
			if record.Severity == SeverityWarning {
				counts.WarningCount++
			} else {
				counts.ErrorCount++
			}
		}

		return counts
	default:
		return DiagnosticCounts{}
	}
}

func consequenceContext(publication Publication) string {
	if publication.Published {
		return "committed"
	}

	return "predicted"
}
