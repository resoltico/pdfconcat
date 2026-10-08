// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"fmt"
)

type (
	// Code is a stable, machine-readable failure category.
	Code string

	// PolicyReason identifies an instruction-policy refusal without parsing its message.
	PolicyReason string

	// Error is a failure with a stable code and, where known, the identity of the file it concerns.
	Error struct {
		// Err is the underlying cause.
		Err error
		// Code is the stable failure category.
		Code Code
		// Path is the file concerned, or empty.
		Path string
		// Reason is nonempty for assembly-plan policy failures.
		Reason PolicyReason
		// Source is the index into AssembleRequest.Sources, or NoSource.
		Source int
		// Run is the offending compact-order index, or NoRun for non-run failures.
		Run int
	}
)

// Failure codes. They are part of the package contract: callers branch on them and reports carry them.
const (
	// CodeUnreadable means a file could not be opened or is not a regular file.
	CodeUnreadable Code = "pdf_unreadable"
	// CodeInvalid means the PDF is malformed or fails validation.
	CodeInvalid Code = "pdf_invalid"
	// CodeEncrypted means the PDF is encrypted; encrypted sources are not supported, even with an empty password.
	CodeEncrypted Code = "pdf_encrypted"
	// CodeUnsupportedRendering means source rendering depends on document state the assembler cannot reconcile.
	CodeUnsupportedRendering Code = "pdf_rendering_unsupported"
	// CodeSignatureUnsupported rejects signature values that assembly would invalidate.
	CodeSignatureUnsupported Code = "pdf_signature_unsupported"
	// CodeFormUnsupported rejects form semantics the backend cannot preserve faithfully.
	CodeFormUnsupported Code = "pdf_form_unsupported"
	// CodeNoPages means the PDF has no pages.
	CodeNoPages Code = "pdf_no_pages"
	// CodePageGeometry means the first or last page has missing or malformed page boxes, rotation or UserUnit.
	CodePageGeometry Code = "pdf_page_geometry_invalid"
	// CodePartialRange means a source whose page-local objects, forms or destinations must be re-imported
	// per occurrence was used for only some of its pages.
	CodePartialRange Code = "pdf_partial_range_unsupported"
	// CodeLegacyDestsRepeated means more than one legacy catalog /Dests source occurrence is requested.
	CodeLegacyDestsRepeated Code = "pdf_legacy_dests_repeated"
	// CodeRequestInvalid means the assembly request is inconsistent: a bad index, range, count or total.
	CodeRequestInvalid Code = "assemble_request_invalid"
	// CodeAssemblyFailed means the PDF backend failed while importing, reordering or writing.
	CodeAssemblyFailed Code = "assemble_failed"
	// CodeOutputInvalid means the written PDF failed validation.
	CodeOutputInvalid Code = "output_invalid"
	// CodeOutputPageCount means the written PDF does not contain the expected number of pages.
	CodeOutputPageCount Code = "output_page_count_mismatch"
	// CodeCanceled means the context was canceled or its deadline passed.
	CodeCanceled Code = "canceled"

	// NoSource marks failures not attributable to an inspected source.
	NoSource = -1
	// NoRun marks failures not attributable to a compact-order run.
	NoRun = -1

	// ReasonOrder identifies an empty page order. The other reasons distinguish instruction refusals
	// from the internally derived total invariant.
	ReasonOrder         PolicyReason = "order"
	ReasonCount         PolicyReason = "count"
	ReasonSource        PolicyReason = "source"
	ReasonRange         PolicyReason = "range"
	ReasonResource      PolicyReason = "resource"
	ReasonWholeSource   PolicyReason = "whole_source"
	ReasonLegacyDests   PolicyReason = "legacy_destinations"
	ReasonPageLimit     PolicyReason = "page_limit"
	ReasonTotalMismatch PolicyReason = "total_mismatch"

	keyNames                  = "Names"
	keyType                   = "Type"
	causeDetailFormat         = "%w: %v"
	pageTreeRootFailureFormat = "page tree root: %w"
	causeNumberFormat         = "%w: %d"
	nullPDFObject             = "null"
	readFailureFormat         = "read: %v"
	errorContainingFormat     = "got %v, want an error containing %q"
	singlePageTreeBody        = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	emptyCatalogBody          = "<< /Type /Catalog >>"
)

// Error formats the code, the file and the cause.
func (e *Error) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}

	return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.Err)
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// CodeOf returns the code of the first Error in err's chain, or the empty code.
func CodeOf(err error) Code {
	if engineErr, found := errors.AsType[*Error](err); found {
		return engineErr.Code
	}

	return ""
}

func newError(code Code, source int, path string, err error) *Error {
	return &Error{Code: code, Source: source, Path: path, Err: err, Run: NoRun}
}
