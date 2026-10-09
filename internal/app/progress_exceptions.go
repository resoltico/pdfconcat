// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/report"
)

type cleanupRecord struct {
	Message     string             `json:"message"`
	Kind        string             `json:"kind"`
	AttemptID   string             `json:"attempt_id"`
	Publication report.Publication `json:"publication"`
	SavedRun    report.SavedRun    `json:"saved_run"`

	Sequence      uint64 `json:"sequence"`
	FormatVersion int    `json:"format_version"`
}

func cleanupOnStderr(ctx context.Context, env Env, rep *report.Report, message string) error {
	if env.ProgressRecord == nil || env.ProgressSequence == nil {
		return nil
	}

	return writeProgressException(ctx, env, cleanupRecord{
		FormatVersion: report.Version, Kind: "cleanup_warning", AttemptID: rep.AttemptID,
		Sequence: env.ProgressSequence(), Message: message, SavedRun: report.SavedRunOf(rep), Publication: rep.Publication,
	})
}

// The telemetry producer has already joined. The same owned transport bounds
// exceptional writes and refuses every later record after abandoned delivery.
func writeProgressException(ctx context.Context, env Env, value any) error {
	if env.ProgressRecord != nil {
		return env.ProgressRecord.WriteRecord(context.WithoutCancel(ctx), encodeLine(value))
	}

	return nil
}
