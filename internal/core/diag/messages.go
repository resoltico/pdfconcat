// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"fmt"
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

// XRefEntry is accepted original-entry evidence, independent of a backend object.
type XRefEntry struct {
	Object, Generation, Revision int
	Offset                       int64
}

// IgnoredXRefEntry owns the product explanation and factual identity of this finding.
func IgnoredXRefEntry(entry XRefEntry) Diagnostic {
	d := New(SourceXRefEntryIgnored, "An unreferenced zero-offset classic xref entry was ignored.")
	d.PDFLocation = &PDFLocation{Object: entry.Object, Path: fmt.Sprintf("xref/%d/revision/%d/generation/%d", entry.Offset, entry.Revision, entry.Generation)}
	return d
}
