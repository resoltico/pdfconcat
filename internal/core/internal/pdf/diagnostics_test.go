// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/fault"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

func TestBackendDiagnosticsSeparateFailureFromValidation(t *testing.T) {
	cause := model.WithValidationErrorObject(errors.New("invalid retained structure"), 12)
	d := diagnostic(cause, diag.SourceInvalid)
	if d.Code != diag.SourceInvalid || d.Detail != "invalid retained structure" || d.PDFLocation == nil || d.PDFLocation.Object != 12 || d.Message == d.Detail {
		t.Fatal(d)
	}
	wrapped := &policyError{finding: d, err: cause}
	if got := diagnostic(wrapped, diag.AssemblyFailed); got.Detail != d.Detail || got.PDFLocation != d.PDFLocation || got.Code != d.Code {
		t.Fatal("wrapper lost evidence", got)
	}
	d = diagnostic(fault.Panic{Err: errors.New("backend fault")}, diag.SourceInvalid)
	if d.Code != diag.SourceProcessingFailed || d.Detail != "backend fault" {
		t.Fatal(d)
	}
	if status, exit := diag.Outcome([]diag.Diagnostic{d}); status != diag.StatusFailed || exit != 1 {
		t.Fatal(status, exit)
	}
	d = diagnostic(fmt.Errorf("read: %w", context.Canceled), diag.SourceInvalid)
	if d.Code != diag.Interrupted {
		t.Fatal(d)
	}
	d.File, d.Items = "/source.pdf", []string{"/items/0"}
	d = diagnostic(&policyError{finding: d, err: context.Canceled}, diag.AssemblyFailed)
	if d.Code != diag.Interrupted || d.File != "/source.pdf" || len(d.Items) != 1 {
		t.Fatal("cancellation lost its source context", d)
	}
}
