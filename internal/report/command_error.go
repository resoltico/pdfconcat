// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"io"
	"slices"
	"unicode/utf8"
)

// CommandError describes a command rejected before a job begins, without job lifecycle fields.
type CommandError struct {
	Command            *string          `json:"command"`
	Next               *[]string        `json:"next"`
	Kind               string           `json:"kind"`
	Status             Status           `json:"status"`
	ExecutableFrom     string           `json:"executable_from,omitempty"`
	Diagnostics        []DiagnosticView `json:"diagnostics"`
	TruncatedFields    []string         `json:"truncated_fields,omitempty"`
	FormatVersion      int              `json:"format_version"`
	DiagnosticCount    int              `json:"diagnostic_count"`
	ErrorCount         int              `json:"error_count"`
	WarningCount       int              `json:"warning_count"`
	DiagnosticsOmitted int              `json:"diagnostics_omitted,omitzero"`
	NextOmitted        bool             `json:"next_omitted,omitzero"`
}

// NewCommandError creates a compact command failure.
func NewCommandError(command string, status Status, diagnostics []Diagnostic, executable string) *CommandError {
	diagnostics = slices.Clone(diagnostics)
	for i := range diagnostics {
		if diagnostics[i].Severity == "" {
			diagnostics[i].Severity = SeverityError
		}
	}

	counts := diagnosticCounts(diagnostics)
	shown := min(len(diagnostics), PreviewDiagnostics)

	result := &CommandError{
		FormatVersion: Version, Kind: "error", Status: status,
		DiagnosticCount: counts.DiagnosticCount, ErrorCount: counts.ErrorCount, WarningCount: counts.WarningCount,
		Diagnostics: make([]DiagnosticView, shown), DiagnosticsOmitted: len(diagnostics) - shown,
	}
	if command != "" {
		result.Command = &command
	}

	help := []string{"--help"}
	if command != "" {
		help = []string{command, "--help"}
	}

	next := append([]string{executable}, help...)
	result.Next = &next

	for index := range shown {
		result.Diagnostics[index] = previewDiagnosticView(&diagnostics[index])
	}

	if executable == "" || !utf8.ValidString(executable) || !result.fits() {
		result.Next = nil
		result.NextOmitted = true
		result.ExecutableFrom = invokingExecutableReference
	}

	if !result.fits() {
		result.boundDiagnostics(command)
	}

	return result
}

// RenderText writes the current command fault and its recovery guidance.
func (e *CommandError) RenderText(w io.Writer) error {
	out := &textWriter{writer: w}
	out.linef("command %s", e.Status)

	for index := range e.Diagnostics {
		out.diagnostic(&e.Diagnostics[index])
	}

	if len(e.TruncatedFields) > 0 {
		out.linef("truncated previews: %v", e.TruncatedFields)
	}

	if e.DiagnosticsOmitted > 0 {
		out.linef("diagnostics omitted: %d", e.DiagnosticsOmitted)
	}

	if e.Next != nil {
		out.linef("help: %v", *e.Next)
	}

	if e.NextOmitted {
		out.linef("help argv omitted: use invoking_executable and command help")
	}

	return out.err
}

func (e *CommandError) fits() bool {
	encoded, err := Encode(e)
	// The fixed human rendering is shorter than this typed JSON: tags and string escaping cover
	// every rendered value, and omitted/truncated JSON metadata covers the corresponding text cues.
	return err == nil && len(encoded) < SummaryBytes
}

func (e *CommandError) boundDiagnostics(command string) {
	if e.Command != nil {
		if bounded, cut := BoundPreview(*e.Command); cut {
			e.Command = &bounded
			e.TruncatedFields = append(e.TruncatedFields, memberCommand)
		}
	}

	summary := &Summary{Command: command, Diagnostics: e.Diagnostics, TruncatedFields: e.TruncatedFields}
	for index := range summary.Diagnostics {
		summary.boundDiagnostic(index)

		recovery := summary.Diagnostics[index].Recovery
		if recovery != nil && recovery.LocationFrom != "" {
			recovery.LocationFrom = "original_argv"
			if diagnosticsLocation := e.Diagnostics[index].Location; diagnosticsLocation != nil && diagnosticsLocation.ArgvIndex == nil {
				recovery.LocationFrom = queryReportReference
			}
		}
	}

	e.Diagnostics = summary.Diagnostics
	e.TruncatedFields = summary.TruncatedFields

	for len(e.Diagnostics) > 1 && !e.fits() {
		summary.omitDiagnostic()
		e.Diagnostics = summary.Diagnostics
		e.TruncatedFields = summary.TruncatedFields
		e.DiagnosticsOmitted++
	}

	shrinkDiagnosticMessages(e.Diagnostics, e.fits)
}
