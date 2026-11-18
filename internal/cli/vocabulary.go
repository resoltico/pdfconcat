// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import "github.com/resoltico/pdfconcat/internal/core/diag"

var (
	usageUnknownCommand = diag.BoundaryCode("usage_unknown_command", diag.Definition{Severity: diag.Error, Class: diag.Invalid, Description: "first argument is not a command"})
	usageUnknownOption  = diag.BoundaryCode("usage_unknown_option", diag.Definition{Severity: diag.Error, Class: diag.Invalid, Description: "option not defined for the command"})
	usageMissingValue   = diag.BoundaryCode("usage_missing_value", diag.Definition{Severity: diag.Error, Class: diag.Invalid, Description: "option needs a value"})
	usageInvalidValue   = diag.BoundaryCode("usage_invalid_value", diag.Definition{Severity: diag.Error, Class: diag.Invalid, Description: "option value not allowed (`--fit-to`, `--fit-tolerance`, `schema` name, \u2026)"})
	usageConflict       = diag.BoundaryCode("usage_conflict", diag.Definition{Severity: diag.Error, Class: diag.Invalid, Description: "`--plan` with PDF or `--blank` operands; `--base-dir` without `--plan -`; repeated single-use option"})
	usageMissingInput   = diag.BoundaryCode("usage_missing_input", diag.Definition{Severity: diag.Error, Class: diag.Invalid, Description: "neither `--plan` nor PDF or `--blank` operands"})
)

// InvocationEntries returns the CLI-owned diagnostic catalog for reference generation.
func InvocationEntries() []diag.Entry {
	codes := []diag.Code{usageUnknownCommand, usageUnknownOption, usageMissingValue, usageInvalidValue, usageConflict, usageMissingInput}
	entries := make([]diag.Entry, len(codes))
	for index, code := range codes {
		entries[index] = diag.Entry{Code: code, Definition: code.Definition()}
	}
	return entries
}
