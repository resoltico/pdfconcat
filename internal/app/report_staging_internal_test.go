// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

type canceledReportWriter struct{}

func (canceledReportWriter) Write([]byte) (int, error) { return 0, context.Canceled }

func TestReportSerializationCancellationRetainsInterruptedStatus(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(checkName)
	builder.SetPhases(report.Phases{
		Instructions: report.PhaseComplete, InputInspection: report.PhaseComplete,
		Layout: report.PhaseComplete, OutputVerification: report.PhaseNotRun,
	})

	zero := int64(0)
	builder.SetCounts(report.Counts{SourcePages: &zero, GeneratedPages: &zero, TotalPages: &zero})

	_, err := report.Write(canceledReportWriter{}, builder.Build(report.StatusOK), report.MaxReportBytes)
	if err == nil {
		t.Fatal("serialization unexpectedly completed")
	}

	current := &pipeline{}

	result := current.reportProblem(err)
	if result.status != report.StatusInterrupted || result.diagnostic.Code != codeInterrupted {
		t.Fatalf("wrapped cancellation classified as %+v", result)
	}
}

func TestCanceledReportStagingDoesNotSerializeOrCreateContent(t *testing.T) {
	t.Parallel()

	current := &pipeline{reportPath: t.TempDir() + "/report.json"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// This invalid report would fail validation if serialization ran.
	_, err := current.stageFile(ctx, current.reportPath, &report.Report{})

	result := current.reportProblem(err)
	if result.status != report.StatusInterrupted {
		t.Fatalf("pre-serialization cancellation: %+v", result)
	}
}
