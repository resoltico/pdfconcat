// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

type (
	// Stage names the phase that produced a diagnostic.
	Stage string

	// Code is a stable snake_case diagnostic identifier. Messages are not stable; codes are.
	Code string

	// Error is one located, coded plan diagnostic. Decoding stops at the first structural fault, so an
	// Error is the first problem found, not necessarily the only one.
	Error struct {
		// Err is the underlying cause, if any (an I/O error, or the context's error on interruption).
		Err error
		// Message states what is wrong and what is expected.
		Message  string
		Code     Code
		Stage    Stage
		Location assembly.Location
	}
)

const (
	// StageRead is I/O, cancellation, and the byte limit.
	StageRead Stage = "read"
	// StageSyntax is the JSON lexical and grammar rules.
	StageSyntax Stage = "syntax"
	// StageShape is member names, types, domains, and item shapes.
	StageShape Stage = "shape"
	// StageLimit is the declared resource bounds.
	StageLimit Stage = "limit"

	// CodeInterrupted means the read was cancelled.
	CodeInterrupted Code = "interrupted"
	// CodeReadFailed means the plan could not be read.
	CodeReadFailed Code = "read_failed"
	// CodeTooLarge means the plan exceeds the byte limit.
	CodeTooLarge Code = "plan_too_large"
	// CodeEmpty means the plan has no JSON value.
	CodeEmpty Code = "json_empty"
	// CodeSyntax means the plan is not well-formed JSON text (a lexical or grammar fault, including invalid UTF-8 and unpaired surrogates).
	CodeSyntax Code = "json_syntax"
	// CodeDuplicateMember means an object has two members with the same name once escapes are decoded.
	CodeDuplicateMember Code = "json_duplicate_member"
	// CodeTrailingData means data other than whitespace follows the plan object.
	CodeTrailingData Code = "json_trailing_data"
	// CodeNotObject means the plan is not a JSON object.
	CodeNotObject Code = "plan_not_object"
	// CodeUnknownMember means a member name is not part of the format, or has the wrong case.
	CodeUnknownMember Code = "plan_unknown_member"
	// CodeNull means a null appears where a value is required (omit the member instead).
	CodeNull Code = "plan_null_not_allowed"
	// CodeWrongType means a value has the wrong JSON type.
	CodeWrongType Code = "plan_wrong_type"
	// CodeOutOfRange means a number is outside its range or not finite.
	CodeOutOfRange Code = "plan_out_of_range"
	// CodeNotInteger means a number must be a whole number and is not.
	CodeNotInteger Code = "plan_not_integer"
	// CodeBadValue means a string or value is not one the format accepts.
	CodeBadValue Code = "plan_bad_value"
	// CodeMissingMember means a required member is absent.
	CodeMissingMember Code = "plan_missing_member"
	// CodeAmbiguousItem means an object item fits neither the blank nor the group shape.
	CodeAmbiguousItem Code = "plan_ambiguous_item"
	// CodeUnsupportedVer means the plan version is not the one this build reads.
	CodeUnsupportedVer Code = "plan_unsupported_version"
	// CodeEmptyItems means an items array has no entries.
	CodeEmptyItems Code = "plan_empty_items"
	// CodeLimitNodes means the plan has more structural nodes than the limit.
	CodeLimitNodes Code = "plan_limit_nodes"
	// CodeLimitDepth means directory groups nest deeper than the limit.
	CodeLimitDepth Code = "plan_limit_depth"
	// CodeLimitContribs means the plan has more PDF and blank entries than the limit.
	CodeLimitContribs Code = "plan_limit_contributions"
	// CodeLimitPages means the plan generates more blank pages than the limit.
	CodeLimitPages Code = "plan_limit_generated_pages"
)

// Error formats the diagnostic as "location: message [code]".
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s [%s]", e.Location, e.Message, e.Code)
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error {
	return e.Err
}
