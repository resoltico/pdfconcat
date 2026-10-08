// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

type (
	// RecoveryState describes the retained recovery file, separately from the original report target.
	RecoveryState string
	// Status is the outcome class of a command.
	Status string

	// PhaseState says how far one phase of a run got.
	PhaseState string

	// PartKind distinguishes a source PDF from generated pages.
	PartKind string

	// SizeOrigin says where a generated page's size came from.
	SizeOrigin string

	// WriteState is the write state of the requested full report.
	WriteState string
)

const (
	unknownMetadataValue = "unknown"
	attemptNotWritten    = "not_written"
	// RecoveryCurrent means the retained file describes the actual committed failure.
	RecoveryCurrent RecoveryState = "current"
	// RecoveryPending means complete layout data remains but publication metadata could not be refreshed.
	RecoveryPending RecoveryState = "publication_pending"
	// RecoveryUnavailable means the retained file identity was lost and its content must not be trusted.
	RecoveryUnavailable RecoveryState = "unavailable"
	// StatusOK is success, exit 0.
	StatusOK Status = "ok"
	// StatusInvalid is invalid instructions or a usage error, exit 2.
	StatusInvalid Status = "invalid"
	// StatusFailed is an I/O, backend, or publication failure, exit 1.
	StatusFailed Status = "failed"
	// StatusInterrupted is cancellation, exit 130.
	StatusInterrupted Status = "interrupted"

	// PhaseNotRun means the phase never started.
	PhaseNotRun PhaseState = "not_run"
	// PhaseIncomplete means the phase stopped early.
	PhaseIncomplete PhaseState = "incomplete"
	// PhaseComplete means the phase finished without stopping early.
	PhaseComplete PhaseState = "complete"

	// PartPDF is a source PDF.
	PartPDF PartKind = "pdf"
	// PartBlank is generated pages.
	PartBlank PartKind = "blank"

	// SizeExplicit is a size written in the job.
	SizeExplicit SizeOrigin = "explicit"
	// SizeFollowingSource is a size inherited from the next source page.
	SizeFollowingSource SizeOrigin = "following_source"
	// SizePrecedingSource is a size inherited from the previous source page.
	SizePrecedingSource SizeOrigin = "preceding_source"

	// ReportNotRequested means no report was requested.
	ReportNotRequested WriteState = "not_requested"
	// ReportWritten means the requested report was written.
	ReportWritten WriteState = "written"
	// ReportFailed means the requested report could not be written.
	ReportFailed WriteState = "failed"

	exitFailure     = 1
	exitUsage       = 2
	exitInterrupted = 130
)

// ExitCode is the process exit code for the status; StatusFailed and any unknown status are a failure.
func (s Status) ExitCode() int {
	if s == StatusOK {
		return 0
	}

	if s == StatusInvalid {
		return exitUsage
	}

	if s == StatusInterrupted {
		return exitInterrupted
	}

	return exitFailure
}
