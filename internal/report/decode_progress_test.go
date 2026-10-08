// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestReportDecoderProvidesStorageForReaderProgress(t *testing.T) {
	t.Parallel()

	_, err := report.Decode(
		report.DecoderTestContext(t.Context(), t),
		savedReport,
		report.ReaderRequiringStorage(strings.NewReader(completeCheck)),
	)
	if err != nil {
		t.Fatalf("valid report from progress-dependent reader failed: %v", err)
	}
}

func TestReportDecoderChecksFinalReadCancellationBeforeStructuralFault(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := report.Decode(ctx, savedReport, &cancelAfterRead{cancel: cancel, data: []byte("null")})
	found, ok := report.AsError(err)

	if !ok || found.Diagnostic.Code != report.CodeInterrupted || found.Status().ExitCode() != 130 || !errors.Is(err, context.Canceled) {
		t.Fatalf("final-read cancellation lost priority at scan handoff: %v", err)
	}
}

func TestReportDecoderRejectsNullRootBeforeContainerOperations(t *testing.T) {
	t.Parallel()

	_, err := report.Decode(
		report.DecoderTestContext(t.Context(), t),
		savedReport,
		report.ReaderRequiringStorage(strings.NewReader("null")),
	)
	found, ok := report.AsError(err)

	if !ok || found.Diagnostic.Code != report.CodeNotObject || found.Status().ExitCode() != 2 {
		t.Fatalf("root null must be rejected before container-dependent decoding: %v", err)
	}
}
