// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

type (
	// Status is the complete execution outcome.
	Status = diag.Status
	// Result is a complete normalized execution outcome.
	Result = result.Result
	// Diagnostic is the complete factual contract exposed by core.
	Diagnostic = diag.Diagnostic
	// Location is the complete factual contract exposed by core.
	Location = diag.Location
	// Code is the complete factual contract exposed by core.
	Code = diag.Code
	// Severity is the complete factual contract exposed by core.
	Severity = diag.Severity
	// Class is the complete factual contract exposed by core.
	Class = diag.Class
	// Output is the complete factual contract exposed by core.
	Output = result.Output
	// Pages is the complete factual contract exposed by core.
	Pages = result.Pages
	// Report is the complete factual contract exposed by core.
	Report = result.Report
	// Text is the complete factual contract exposed by core.
	Text = result.Text
	// Span is the complete factual contract exposed by core.
	Span = result.Span
	// Section is the complete factual contract exposed by core.
	Section = result.Section
	// Part is the complete factual contract exposed by core.
	Part = result.Part
	// Size is the complete factual contract exposed by core.
	Size = packet.Size
	// PageNumbers is the complete factual contract exposed by core.
	PageNumbers = packet.PageNumbers
	// FieldRename is the complete factual contract exposed by core.
	FieldRename = packet.FieldRename
	// VersionInfo is the complete factual contract exposed by core.
	VersionInfo = result.Version
)

// InvocationFaultKind is a closed transport-failure classification.
type InvocationFaultKind string

// Invocation fault constants preserve existing transport diagnostic meanings.
const (
	UnknownCommand              InvocationFaultKind = "usage_unknown_command"
	UnknownOption               InvocationFaultKind = "usage_unknown_option"
	MissingValue                InvocationFaultKind = "usage_missing_value"
	InvalidValue                InvocationFaultKind = "usage_invalid_value"
	Conflict                    InvocationFaultKind = "usage_conflict"
	MissingInput                InvocationFaultKind = "usage_missing_input"
	WorkingDirectoryUnavailable InvocationFaultKind = "working_directory_unavailable"
)

// InvocationFailure adapts closed transport failures without execution or I/O.
func InvocationFailure(hint Operation, kind InvocationFaultKind, message, reference string) Result {
	if hint != "" && hint != Check && hint != Build || !metadata(message) || !metadata(reference) {
		return invalidRequest(hint)
	}
	switch kind {
	case UnknownCommand, UnknownOption, MissingValue, InvalidValue, Conflict, MissingInput, WorkingDirectoryUnavailable:
		r := result.New(string(hint))
		codes := map[InvocationFaultKind]diag.Code{
			UnknownCommand: diag.UsageUnknownCommand, UnknownOption: diag.UsageUnknownOption,
			MissingValue: diag.UsageMissingValue, InvalidValue: diag.UsageInvalidValue,
			Conflict: diag.UsageConflict, MissingInput: diag.UsageMissingInput,
			WorkingDirectoryUnavailable: diag.WorkingDirectoryUnavailable,
		}
		d := diag.New(codes[kind], message)
		if reference != "" {
			d.Items = []string{reference}
		}
		r.Diagnostics = []Diagnostic{d}
		return r.Full()
	default:
		return invalidRequest(hint)
	}
}

// ExitCode uses complete diagnostics, independently of presentation caps.
func ExitCode(r Result) int { _, exit := diag.Outcome(r.Diagnostics); return exit }
