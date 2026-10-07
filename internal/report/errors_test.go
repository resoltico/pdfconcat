// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"fmt"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestAsErrorFindsAReportErrorInAChain(t *testing.T) {
	t.Parallel()

	_, err := mustDecode(t, completeCheck).Query(report.Request{Part: new("nope")})

	found, ok := report.AsError(fmt.Errorf("while querying: %w", err))
	if !ok || found == nil || found.Diagnostic.Code != report.CodePartNotFound {
		t.Errorf("got %v, %v", found, ok)
	}
}

func TestAsErrorDoesNotReportANilReportError(t *testing.T) {
	t.Parallel()

	var missing *report.Error

	if found, ok := report.AsError(missing); ok || found != nil {
		t.Errorf("a nil *Error carries no diagnostic: %v, %v", found, ok)
	}

	if found, ok := report.AsError(nil); ok || found != nil {
		t.Errorf("no error is not a report error: %v, %v", found, ok)
	}
}
