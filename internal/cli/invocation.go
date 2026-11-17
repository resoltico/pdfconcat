// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"strings"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
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
	PlanUnreadable              InvocationFaultKind = "plan_unreadable"
	Interrupted                 InvocationFaultKind = "interrupted"
)

// InvocationFailure adapts closed transport failures without execution or I/O.
func InvocationFailure(hint app.Operation, kind InvocationFaultKind, message, reference string) app.Result {
	if hint != "" && hint != app.Check && hint != app.Build || !validInvocationText(message) || !validInvocationText(reference) {
		return invalidInvocation(hint)
	}
	switch kind {
	case UnknownCommand, UnknownOption, MissingValue, InvalidValue, Conflict, MissingInput, WorkingDirectoryUnavailable, PlanUnreadable, Interrupted:
		r := outcome.New(string(hint))
		codes := map[InvocationFaultKind]diag.Code{
			UnknownCommand: usageUnknownCommand, UnknownOption: usageUnknownOption,
			MissingValue: usageMissingValue, InvalidValue: usageInvalidValue,
			Conflict: usageConflict, MissingInput: usageMissingInput,
			WorkingDirectoryUnavailable: diag.WorkingDirectoryUnavailable,
			PlanUnreadable:              diag.PlanUnreadable, Interrupted: diag.Interrupted,
		}
		d := diag.New(codes[kind], message)
		if reference != "" {
			d.Items = []string{reference}
		}
		r.Diagnostics = []app.Diagnostic{d}
		return r.Full()
	default:
		return invalidInvocation(hint)
	}
}

func validInvocationText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
func invalidInvocation(hint app.Operation) app.Result {
	if hint != app.Check && hint != app.Build {
		hint = ""
	}
	r := outcome.New(string(hint))
	r.Diagnostics = []diag.Diagnostic{diag.New(diag.RequestInvalid, "Invalid application request.")}
	return r.Full()
}

// ExitCode maps the complete outcome, independently of capped diagnostics.
func ExitCode(status diag.Status) int {
	switch status {
	case diag.OK:
		return 0
	case diag.StatusInvalid:
		return 2
	case diag.StatusFailed:
		return 1
	case diag.StatusInterrupted:
		return 130
	default:
		panic("unhandled execution outcome: " + status)
	}
}
