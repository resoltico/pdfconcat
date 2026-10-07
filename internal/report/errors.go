// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"errors"
	"fmt"
)

type (
	// Stage names the phase that produced a diagnostic: lower-case snake_case.
	Stage string

	// Code is a stable snake_case diagnostic identifier. Messages are not stable; codes are.
	Code string

	// Error is a coded, located report failure: a saved report that cannot be read, or a query that
	// cannot be answered. The command layer turns Diagnostic into a structured error report and Status
	// into the exit code.
	Error struct {
		Err        error
		status     Status
		Diagnostic Diagnostic
	}
)

// Stages name the phase that produced a diagnostic; codes are the stable identifiers of what went wrong.
const (
	// StageUsage is a rejected command line or query.
	StageUsage Stage = "usage"
	// StageRead is reading a file, cancellation, and the byte limit.
	StageRead Stage = "read"
	// StageWrite is producing or writing a report.
	StageWrite Stage = "write"
	// StageSyntax is the JSON lexical and grammar rules.
	StageSyntax Stage = "syntax"
	// StageShape is member names, types, domains, and references.
	StageShape Stage = "shape"
	// StageLimit is the declared resource bounds.
	StageLimit Stage = "limit"

	CodeInterrupted        Code = "report_interrupted"
	CodeReadFailed         Code = "report_read_failed"
	CodeWriteFailed        Code = "report_write_failed"
	CodeTooLarge           Code = "report_too_large"
	CodeEmpty              Code = "report_json_empty"
	CodeSyntax             Code = "report_json_syntax"
	CodeDuplicateMember    Code = "report_json_duplicate_member"
	CodeTrailingData       Code = "report_json_trailing_data"
	CodeNotObject          Code = "report_not_object"
	CodeUnknownMember      Code = "report_unknown_member"
	CodeNull               Code = "report_null_not_allowed"
	CodeMissingMember      Code = "report_missing_member"
	CodeWrongType          Code = "report_wrong_type"
	CodeUnsupportedVersion Code = "report_unsupported_version"
	CodeWrongKind          Code = "report_wrong_kind"
	CodeLimitNodes         Code = "report_limit_nodes"
	CodeLimitDepth         Code = "report_limit_depth"
	CodeInvalidValue       Code = "report_invalid_value"
	CodeDanglingReference  Code = "report_dangling_reference"
	CodeDuplicateID        Code = "report_duplicate_id"
	CodeInvalidRange       Code = "report_invalid_range"

	CodeSelectionConflict Code = "report_selection_conflict"
	CodePagingNeedsView   Code = "report_paging_needs_view"
	CodeDetailsNeedSelect Code = "report_details_need_selection"
	CodeInvalidPaging     Code = "report_invalid_paging"
	CodeInvalidNumber     Code = "report_invalid_number"
	CodeUnknownView       Code = "report_unknown_view"
	CodePartNotFound      Code = "report_part_not_found"
	CodePageOutOfRange    Code = "report_page_out_of_range"
	CodeLayoutIncomplete  Code = "report_layout_incomplete"
)

// Error formats the failure as "file: message [code]".
func (e *Error) Error() string {
	where := ""
	if e.Diagnostic.Location != nil {
		where = e.Diagnostic.Location.String() + ": "
	}

	return fmt.Sprintf("%s%s [%s]", where, e.Diagnostic.Message, e.Diagnostic.Code)
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// Status is StatusInvalid for rejected input or usage, StatusFailed for a read failure, and
// StatusInterrupted for cancellation.
func (e *Error) Status() Status { return e.status }

// String formats the location as "file:line:column (byte N) at pointer".
func (l Location) String() string {
	text := l.File

	if l.Line > 0 {
		text = fmt.Sprintf("%s:%d:%d", text, l.Line, l.Column)
	}

	if l.Offset != nil {
		text = fmt.Sprintf("%s (byte %d)", text, *l.Offset)
	}

	switch {
	case l.ArgvIndex != nil:
		text = fmt.Sprintf("%s argv:%d", text, *l.ArgvIndex)
	case l.Pointer != "":
		text += " at " + l.Pointer
	default:
	}

	return text
}

func newError(status Status, stage Stage, code Code, location *Location, cause error, format string, args ...any) *Error {
	return &Error{
		Err:    cause,
		status: status,
		Diagnostic: Diagnostic{
			Stage: stage, Code: code, Location: location,
			Message: fmt.Sprintf(format, args...),
		},
	}
}

func invalid(stage Stage, code Code, location *Location, format string, args ...any) *Error {
	return newError(StatusInvalid, stage, code, location, nil, format, args...)
}

func usage(code Code, format string, args ...any) *Error {
	return invalid(StageUsage, code, nil, format, args...)
}

// AsError returns the *Error in err's chain, if any.
func AsError(err error) (*Error, bool) {
	var found *Error

	return found, errors.As(err, &found) && found != nil
}
