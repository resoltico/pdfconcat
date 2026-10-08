// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/resoltico/pdfconcat/internal/publish"
	"github.com/resoltico/pdfconcat/internal/report"
)

const codeRecoveryRefresh report.Code = "report_recovery_refresh_failed"

var errRecoveryIdentityChanged = errors.New("the retained recovery file is no longer the owned staged report")

// reportCommitFailure preserves the PDF and records current or explicitly planned recovery metadata.
func (p *pipeline) reportCommitFailure(ctx context.Context, result *publish.Result, failure *publish.ReportPublishError) error {
	defer func() {
		if closeErr := result.CloseRecovery(); closeErr != nil {
			p.warnings = append(p.warnings, closeErr.Error())
		}
	}()

	p.status = report.StatusFailed
	p.publication.ReportStatus = report.ReportFailed
	p.publication.RecoveryReport = failure.RecoveryPath
	p.publication.RecoveryState = report.RecoveryCurrent
	diagnostic := report.Diagnostic{
		Stage:   stagePublish,
		Code:    codeReportPublish,
		Path:    failure.Target,
		Message: recoveryMessage(failure),
		Cause:   reasonOf(failure.Err),
		Recovery: &report.Recovery{
			Action:       "recover_report",
			ReportFrom:   "unused_report_target",
			RecoveryFrom: "publication.recovery_report",
		},
	}

	err := p.refreshRecovery(ctx, result.RecoveryOwner, diagnostic)
	if err != nil {
		p.publication.RecoveryState = report.RecoveryPending
		if identityErr := p.verifyRetainedRecovery(result.RecoveryOwner); identityErr != nil {
			p.publication.RecoveryState = report.RecoveryUnavailable
			diagnostic.Recovery = nil
			diagnostic.Message = "PDF published, report not saved. Do not rebuild or copy the observed recovery path: " +
				"its owned identity is unavailable. " + failure.Err.Error()
		}

		p.builder.AddDiagnostic(secondaryOrder, report.Diagnostic{
			Stage: stagePublish, Code: codeRecoveryRefresh,
			Message: recoveryRefreshMessage(p.publication.RecoveryState, err),
		})
	}

	p.builder.AddDiagnostic(0, diagnostic)

	return errStopped
}

// refreshRecovery stages first and replaces only the still-owned retained file, after verifying identities.
func (p *pipeline) refreshRecovery(ctx context.Context, owner *publish.RecoveryOwner, diagnostic report.Diagnostic) error {
	// The failed report target is abandoned; recovery is guarded as a distinct destination.
	if err := p.registry.RetireReport(p.reportPath); err != nil {
		return fmt.Errorf("retire failed report target: %w", err)
	}

	if err := p.verifyRetainedRecovery(owner); err != nil {
		return err
	}

	snapshot := p.snapshot(report.StatusFailed)
	snapshot.Diagnostics = append(snapshot.Diagnostics, diagnostic)

	staged, err := p.stageFile(ctx, p.publication.RecoveryReport, snapshot)
	if err != nil {
		return err
	}

	staged.Verify = func() error { return p.verifyRetainedRecovery(owner) }
	err = staged.Publish(ctx, publish.Policy{Overwrite: true})
	p.reportCleanupWarning(staged.CleanupError())

	return p.finishRecoveryRefresh(err)
}

// finishRecoveryRefresh distinguishes visible metadata with a finalization failure from failed publication.
func (p *pipeline) finishRecoveryRefresh(err error) error {
	finalization, finalizationFailed := errors.AsType[*publish.FinalizationError](err)
	if finalizationFailed {
		p.warnings = append(p.warnings, finalization.Error()+"; recovery metadata is visible")
		return nil
	}

	return err
}

func (p *pipeline) verifyRetainedRecovery(owner *publish.RecoveryOwner) error {
	path := p.publication.RecoveryReport

	if owner == nil {
		return errRecoveryIdentityChanged
	}

	if err := owner.Verify(path); err != nil {
		return fmt.Errorf("%w: %w", errRecoveryIdentityChanged, err)
	}

	return p.verifyReport(path)
}

func recoveryRefreshMessage(state report.RecoveryState, err error) string {
	if state == report.RecoveryUnavailable {
		return "Recovery identity is unavailable; the observed file is not asserted complete and must not be copied. " +
			"Refresh failed: " + err.Error()
	}

	return "Recovery layout remains complete but its publication metadata is planned, not current; " +
		"the original report was not saved. Refresh failed: " + err.Error()
}
