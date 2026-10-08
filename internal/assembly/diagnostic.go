// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import (
	"errors"
	"fmt"
)

type (
	// Stage names the phase that produced a diagnostic.
	Stage string

	// Code is a stable snake_case diagnostic identifier. Messages are not stable; codes are.
	Code string

	// Error is one located, coded problem found while resolving a job. Its shape matches the plan
	// decoder's error: stage, code, location, and an actionable message.
	Error struct {
		// Consumers lists every affected flattened contribution in instruction order.
		Consumers []Origin
		// Err is the underlying cause, matched by [errors.Is] and [errors.As].
		Err error
		// Message states what is wrong and how to repair it.
		Message string
		Code    Code
		Stage   Stage
		// Related locates further contributions with the same problem, in input order, at most MaxRelated.
		Related []Location
		// Location is where the problem was written; for a shared problem, its first contribution.
		Location Location
		// Affected is the number of contributions the problem applies to.
		Affected int64
	}

	// Errors is every problem one resolution step found, in input order. It is the error those steps
	// return, so a caller reports all problems rather than the first.
	Errors []*Error
)

const (
	// StageJob is the structural validity of the job tree and its limits.
	StageJob Stage = "job"
	// StagePath is path and font-file resolution.
	StagePath Stage = "path"
	// StageStyle is the values of the non-geometric appearance fields.
	StageStyle Stage = "style"
	// StageGeometry is page sizes and page totals.
	StageGeometry Stage = "geometry"

	// CodeInvalidJob means the job tree breaks a structural rule or limit.
	CodeInvalidJob Code = "job_invalid"
	// CodeBaseNotAbsolute means a base directory is not absolute.
	CodeBaseNotAbsolute Code = "base_not_absolute"
	// CodePathDriveRelative means a Windows path names a drive without a root (C:x).
	CodePathDriveRelative Code = "path_drive_relative"
	// CodePathRootedWithoutDrive means a Windows path has a root but no drive (\x or /x).
	CodePathRootedWithoutDrive Code = "path_rooted_without_drive"
	// CodeStyleInvalid means an appearance value is outside its domain.
	CodeStyleInvalid Code = "style_invalid"
	// CodeSizeUnresolved means a blank inherits a page size and no source page is available.
	CodeSizeUnresolved Code = "size_unresolved"
	// CodeSizeOutOfRange means the inherited page size is outside what a generated page can have.
	CodeSizeOutOfRange Code = "size_out_of_range"
	// CodeSourceEmpty means a source reports no pages.
	CodeSourceEmpty Code = "source_empty"
	// CodePageTotalExceeded means a page total passes the backend's integer boundary or the generated-page limit.
	CodePageTotalExceeded Code = "page_total_exceeded"
	// CodeFontUnavailable means no content identity was supplied for a font the job uses.
	CodeFontUnavailable Code = "font_unavailable"

	// MaxRelated bounds Error.Related.
	MaxRelated = 8
)

// NewSharedError builds an error about a problem shared by several contributions. It is located at the
// first of origins, with the following origins (at most MaxRelated) as related locations; member is
// appended to each node's pointer. template supplies Stage, Code, Err, Message, and Affected.
func NewSharedError(src Source, origins []Origin, member string, template Error) *Error {
	shared := template
	shared.Location = Locate(src, origins[0], member)

	for _, origin := range origins[1:min(len(origins), MaxRelated+1)] {
		shared.Related = append(shared.Related, Locate(src, origin, member))
	}

	return &shared
}

// Error formats the diagnostic as "location: message [code]".
func (d *Error) Error() string {
	return fmt.Sprintf("%s: %s [%s]", d.Location, d.Message, d.Code)
}

// Unwrap returns the underlying cause.
func (d *Error) Unwrap() error {
	return d.Err
}

// Error formats the first diagnostic and counts the rest.
func (l Errors) Error() string {
	switch len(l) {
	case 0:
		return "no diagnostics"
	case 1:
		return l[0].Error()
	default:
		return fmt.Sprintf("%s (and %d more problems)", l[0], len(l)-1)
	}
}

// Unwrap returns the diagnostics, so [errors.Is] and [errors.As] see every cause.
func (l Errors) Unwrap() []error {
	errs := make([]error, len(l))
	for index, diagnostic := range l {
		errs[index] = diagnostic
	}

	return errs
}

// Diagnostics returns the diagnostics carried by err: those of an [Errors], or err alone as an
// uncoded error when it is any other error, and nil for a nil error.
func Diagnostics(err error) Errors {
	if err == nil {
		return nil
	}

	list, ok := errors.AsType[Errors](err)
	if ok {
		return list
	}

	return Errors{{Err: err, Message: err.Error(), Affected: 1}}
}

// errorAt builds an Error located at origin, with member appended to the node's pointer.
func errorAt(src Source, origin Origin, member string, stage Stage, code Code, cause error, format string, args ...any) *Error {
	return &Error{
		Err:      cause,
		Message:  fmt.Sprintf(format, args...),
		Code:     code,
		Stage:    stage,
		Location: Locate(src, origin, member),
		Affected: 1,
	}
}
