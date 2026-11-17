// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
	"github.com/resoltico/pdfconcat/internal/files"
)

type (
	// PartKind names a resolved physical span.
	PartKind = packet.PartKind
	// Status is the complete execution outcome.
	Status = diag.Status
	// Result is a complete normalized execution outcome.
	Result = outcome.Result
	// Diagnostic is the complete factual contract exposed by core.
	Diagnostic = diag.Diagnostic
	// Location is the complete factual contract exposed by core.
	Location = diag.Location
	// PDFLocation identifies a PDF object, page or dictionary entry.
	PDFLocation = diag.PDFLocation
	// Code is the complete factual contract exposed by core.
	Code = diag.Code
	// Severity is the complete factual contract exposed by core.
	Severity = diag.Severity
	// Class is the complete factual contract exposed by core.
	Class = diag.Class
	// Output is the complete factual contract exposed by core.
	Output = outcome.Output
	// Pages is the complete factual contract exposed by core.
	Pages = outcome.Pages
	// Report is the complete factual contract exposed by core.
	Report = outcome.Report
	// Text is the complete factual contract exposed by core.
	Text = outcome.Text
	// Span is the complete factual contract exposed by core.
	Span = outcome.Span
	// Section is the complete factual contract exposed by core.
	Section = outcome.Section
	// Part is the complete factual contract exposed by core.
	Part = outcome.Part
	// Size is the complete factual contract exposed by core.
	Size = packet.Size
	// PageNumbers is the complete factual contract exposed by core.
	PageNumbers = packet.PageNumbers
	// FieldRename is the complete factual contract exposed by core.
	FieldRename = packet.FieldRename
)

// Physical part kinds used in result presentation.
const (
	PartSource   = packet.PartSource
	PartPage     = packet.PartPage
	PartDivider  = packet.PartDivider
	PartContents = packet.PartContents
	PartPadding  = packet.PartPadding
)

func knownCount(n int64) *int64           { return &n }
func hasErrors(ds []diag.Diagnostic) bool { status := diag.Outcome(ds); return status != "ok" }
func ioDiagnostic(code diag.Code, err error, file string) diag.Diagnostic {
	if files.InvalidInput(err) {
		switch code {
		case diag.SourceUnreadable, diag.SourceInputInvalid:
			code = diag.SourceInputInvalid
		case diag.PlanUnreadable:
			code = diag.PlanInputInvalid
		case diag.FontUnreadable, diag.FontInputInvalid:
			code = diag.FontInputInvalid
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = diag.Interrupted
	}
	nativeDetail, cause := files.NativeCause(err)
	facts := diag.IOCause{Reason: cause.Error(), File: file}
	if nativeDetail {
		facts.NativeDetail = err.Error()
	}
	if file == "<stdin>" {
		facts.File = ""
	}
	return diag.IOFailure(code, facts)
}

func noteCancellation(c context.Context, r *outcome.Result) {
	if err := c.Err(); err != nil {
		for _, d := range r.Diagnostics {
			if d.Code == diag.Interrupted {
				return
			}
		}
		r.Diagnostics = append(r.Diagnostics, diag.Report(diag.Interrupted, diag.TechnicalFailure("input", err.Error())))
	}
}

func invalidRequest(hint Operation) Result {
	if hint != Check && hint != Build {
		hint = ""
	}
	r := outcome.New(string(hint))
	r.Diagnostics = []Diagnostic{diag.Report(diag.RequestInvalid, diag.InvalidApplicationRequest())}
	return r.Full()
}
