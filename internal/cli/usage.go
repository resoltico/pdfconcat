// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/report"
)

// UsageError reports a rejected command line. The process exits with status 2. Parsing stops at the first
// fault, so Diagnostics holds exactly that one problem.
type UsageError struct {
	// Command is the command being parsed; empty when none was recognized.
	Command Name
	// Diagnostics are the located problems, each with stage "usage".
	Diagnostics []report.Diagnostic
	// Format is how to render the failure: text only when a valid --format text came before the fault.
	Format Format
}

// Diagnostic codes of command-line failures. Codes are stable; messages are not. Report-query rules that
// the report package also states use that package's codes, so a rule has one code wherever it is caught.
const (
	CodeMissingCommand     report.Code = "usage_missing_command"
	CodeUnknownCommand     report.Code = "usage_unknown_command"
	CodeCommandNotFirst    report.Code = "usage_command_not_first"
	CodeUnknownOption      report.Code = "usage_unknown_option"
	CodeInapplicableOption report.Code = "usage_inapplicable_option"
	CodeDuplicateOption    report.Code = "usage_duplicate_option"
	CodeMissingValue       report.Code = "usage_missing_value"
	CodeDashValue          report.Code = "usage_dash_value"
	CodeUnexpectedValue    report.Code = "usage_unexpected_value"
	CodeEmptyValue         report.Code = "usage_empty_value"
	CodeInvalidUTF8        report.Code = "usage_invalid_utf8"
	CodeInvalidValue       report.Code = "usage_invalid_value"
	CodeInvalidJobs        report.Code = "usage_invalid_jobs"
	CodeConflictingActions report.Code = "usage_conflicting_actions"
	CodePlanSourceConflict report.Code = "usage_plan_source_conflict"
	CodeMissingPlanSource  report.Code = "usage_missing_plan_source"
	CodeBaseDirConflict    report.Code = "usage_base_dir_conflict"
	CodeMissingOperand     report.Code = "usage_missing_operand"
	CodeUnexpectedOperand  report.Code = "usage_unexpected_operand"
	CodeStdinOperand       report.Code = "usage_stdin_operand"
	CodeUnknownSchema      report.Code = "usage_unknown_schema"

	// argvFile is the Location file of a command-line fault, the same source name as jobs compiled from operands.
	argvFile = "argv"
)

// UsageCodes lists every diagnostic code Parse can produce, in a stable order.
func UsageCodes() []report.Code {
	return []report.Code{
		CodeMissingCommand, CodeUnknownCommand, CodeCommandNotFirst, CodeUnknownOption, CodeInapplicableOption,
		CodeDuplicateOption, CodeMissingValue, CodeDashValue, CodeUnexpectedValue, CodeEmptyValue,
		CodeInvalidUTF8, CodeInvalidValue, CodeInvalidJobs, CodeConflictingActions, CodePlanSourceConflict, CodeMissingPlanSource,
		CodeBaseDirConflict, CodeMissingOperand, CodeUnexpectedOperand, CodeStdinOperand, CodeUnknownSchema,
		report.CodeSelectionConflict, report.CodePagingNeedsView, report.CodeDetailsNeedSelect, report.CodeUnknownView,
		report.CodeInvalidNumber, report.CodeInvalidPaging,
	}
}

// Error formats the first diagnostic as "argv:N: message [code]".
func (e *UsageError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "invalid command line"
	}

	first := e.Diagnostics[0]
	if first.Location != nil && first.Location.ArgvIndex != nil {
		return fmt.Sprintf("%s:%d: %s [%s]", argvFile, *first.Location.ArgvIndex, first.Message, first.Code)
	}

	return fmt.Sprintf("%s [%s]", first.Message, first.Code)
}

// usageError builds the error for one fault; index is the argument it concerns, or negative for none.
func usageError(command Name, format Format, code report.Code, index int, message string) *UsageError {
	diagnostic := report.Diagnostic{Stage: report.StageUsage, Code: code, Message: message}

	if index >= 0 {
		location := &report.Location{ArgvIndex: &index}
		location.File = argvFile
		diagnostic.Location = location
	}

	return &UsageError{Command: command, Format: format, Diagnostics: []report.Diagnostic{diagnostic}}
}
