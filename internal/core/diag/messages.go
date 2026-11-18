// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// IOCause supplies named technical parameters, separate from product wording.
type IOCause struct{ Reason, NativeDetail, File string }

// IOFailure renders complete product explanations from classified adapter facts.
func IOFailure(code Code, facts IOCause) Diagnostic {
	var message string
	switch code {
	case PlanUnreadable, PlanInputInvalid, PlanTooLarge:
		message = fmt.Sprintf("Cannot read plan: %s.", facts.Reason)
	case FontUnreadable, FontInputInvalid:
		message = fmt.Sprintf("Cannot read font file: %s.", facts.Reason)
	case SourceUnreadable, SourceInputInvalid:
		message = fmt.Sprintf("Cannot read source file: %s.", facts.Reason)
	case OutputWriteFailed:
		message = fmt.Sprintf("Cannot write output PDF: %s.", facts.Reason)
	case OutputPublishFailed:
		message = fmt.Sprintf("Output publication reported an error: %s.", facts.Reason)
	case ReportWriteFailed:
		message = fmt.Sprintf("Report writing or publication reported an error: %s.", facts.Reason)
	case Interrupted:
		message = fmt.Sprintf("The operation was interrupted: %s.", facts.Reason)
	default:
		message = fmt.Sprintf("I/O operation failed: %s.", facts.Reason)
	}
	d := New(code, message)
	d.Detail = strings.ToValidUTF8(facts.NativeDetail, "�")
	d.File = facts.File
	return d
}

// SourceScratchError marks a failure in temporary source storage.
type SourceScratchError struct{ Cause error }

func (e *SourceScratchError) Error() string { return e.Cause.Error() }
func (e *SourceScratchError) Unwrap() error { return e.Cause }

// SourceScratchDiagnostic gives inspection and assembly the same diagnosis,
// independently of the native error that a scratch path and a source can share.
func SourceScratchDiagnostic(err error, file string) (Diagnostic, bool) {
	var scratch *SourceScratchError
	if !errors.As(err, &scratch) {
		return Diagnostic{}, false
	}
	detail := strings.ToValidUTF8(err.Error(), "�")
	for _, interruption := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, interruption) {
			return IOFailure(Interrupted, IOCause{Reason: interruption.Error(), NativeDetail: detail, File: file}), true
		}
	}
	d := New(SourceUnreadable, "Cannot use temporary storage for a private source snapshot. Choose a writable temporary directory with enough space.")
	d.File, d.Detail = file, detail
	return d, true
}

// XRefEntry is accepted original-entry evidence, independent of a backend object.
type XRefEntry struct {
	Object, Generation, Revision int
	Offset                       int64
}

// IgnoredXRefEntry owns the product explanation and factual identity of this finding.
func IgnoredXRefEntry(entry XRefEntry) Diagnostic {
	d := Report(SourceXRefEntryIgnored, explain("ignored_xref_entry", "An unreferenced zero-offset classic xref entry was ignored.", strconv.Itoa(entry.Object), strconv.Itoa(entry.Generation), strconv.Itoa(entry.Revision), strconv.FormatInt(entry.Offset, 10)))
	d.Identity.Phase = "source_admission"
	d.PDFLocation = &PDFLocation{Object: entry.Object, Path: fmt.Sprintf("xref/%d/revision/%d/generation/%d", entry.Offset, entry.Revision, entry.Generation)}
	return d
}
