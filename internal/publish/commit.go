// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/capture"
)

type (
	// PDF describes the verified staged PDF to publish.
	PDF struct {
		// Verify, when set, runs immediately before the commit; a failure aborts publication.
		Verify func() error
		// AfterCommit is a test boundary hook, called once only after the PDF becomes visible.
		AfterCommit func()
		// Staged is the complete staged file, in the same filesystem as Destination.
		Staged string
		// Destination is the output path.
		Destination string
		// Overwrite allows replacing an existing regular file.
		Overwrite bool
	}

	// Result is the committed state after Commit, valid even when Commit returns an error.
	Result struct {
		// ReportCleanupError reports cleanup failure after the report became visible, without undoing publication.
		ReportCleanupError error
		// RecoveryOwner pins the retained file until the caller finishes its recovery verification.
		RecoveryOwner *RecoveryOwner
		// RecoveryPath is the absolute path of the one complete report kept after a late report
		// failure; empty otherwise.
		RecoveryPath string
		// RecoveryInstruction says how to finish saving the report without rebuilding.
		RecoveryInstruction string
		// PDFPublished is true once the PDF is visible at its destination. It is never rolled back.
		PDFPublished bool
		// ReportPublished is true once the report is visible at its target.
		ReportPublished bool
	}

	// ReportPublishError reports that the PDF was published but the report could not be.
	ReportPublishError struct {
		// Err is the publication failure.
		Err error
		// Target is the report destination.
		Target string
		// RecoveryPath is the retained complete report.
		RecoveryPath string
		// Instruction is the repair that does not rebuild the PDF.
		Instruction string
	}
)

// Error states that the PDF is published and what to do next.
func (e *ReportPublishError) Error() string {
	return fmt.Sprintf(
		"the PDF was published, but the report %q could not be: %v; the complete report is kept at %q; %s",
		e.Target, e.Err, e.RecoveryPath, e.Instruction)
}

// Unwrap exposes the publication failure.
func (e *ReportPublishError) Unwrap() error { return e.Err }

// existing is how publication treats a file already at the destination.
func (pdf PDF) existing() existingFile {
	return Policy{Overwrite: pdf.Overwrite}.existing()
}

// Commit publishes the PDF and then the report, the one fixed order in which the PDF cannot be
// misdescribed. report may be nil. Before the PDF is touched it honors cancellation, runs
// Verify, and re-checks both destinations; any failure there discards the staged report and
// publishes nothing. Once the PDF is visible nothing rolls it back and cancellation is ignored:
// if the report then fails to publish, its staged file is kept as the single recovery file and
// the error is a *ReportPublishError carrying a safe, rebuild-free instruction.
func Commit(ctx context.Context, pdf PDF, report *Staged, policy Policy) (Result, error) {
	return commitWith(ctx, realOperations(), pdf, report, policy)
}

func commitWith(ctx context.Context, ops operations, pdf PDF, report *Staged, policy Policy) (Result, error) {
	err := checkBeforeCommit(ctx, pdf, report, policy)
	if err != nil {
		return Result{}, errors.Join(err, discard(report))
	}

	var result Result

	pdfErr := commitFileChecked(ctx, ops, pdf.Staged, pdf.Destination, pdf.existing(), pdf.Verify)

	var finalization *FinalizationError

	result.PDFPublished = pdfErr == nil || errors.As(pdfErr, &finalization)
	if !result.PDFPublished {
		return result, errors.Join(pdfErr, discard(report))
	}

	if pdf.AfterCommit != nil {
		pdf.AfterCommit()
	}

	if report == nil {
		return result, pdfErr
	}

	// Past the PDF commit the report is published even if ctx is now canceled: the PDF cannot
	// be undone, so the report must describe it.
	guard := report.Verify
	report.Verify = func() error {
		if aliasErr := rejectArtifactAlias(pdf.Destination, report.target); aliasErr != nil {
			return aliasErr
		}

		if guard != nil {
			return guard()
		}

		return nil
	}
	renamed, reportErr := report.publishRetainingContext(context.WithoutCancel(ctx), policy)
	result.ReportPublished = renamed || reportErr == nil
	result.ReportCleanupError = report.CleanupError()

	if reportErr == nil || renamed {
		return result, errors.Join(pdfErr, reportErr)
	}

	report.settled = true
	result.RecoveryPath = report.path
	result.RecoveryOwner = report.owner
	report.owner = nil
	result.RecoveryInstruction = recoveryInstruction(runtime.GOOS, report.path, report.target)

	return result, errors.Join(pdfErr, &ReportPublishError{
		Target:       report.target,
		RecoveryPath: report.path,
		Instruction:  result.RecoveryInstruction,
		Err:          reportErr,
	})
}

func checkBeforeCommit(ctx context.Context, pdf PDF, report *Staged, policy Policy) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf(publicationFailureFormat, pdf.Destination, err)
	}

	if report != nil {
		if aliasErr := rejectArtifactAlias(pdf.Destination, report.target); aliasErr != nil {
			return aliasErr
		}

		return Preflight(report.target, policy)
	}

	return nil
}

func discard(report *Staged) error {
	if report == nil {
		return nil
	}

	return report.Discard()
}

// recoveryInstruction returns the command that saves a kept report without overwriting anything, in the
// command language of goos: PowerShell on Windows, a POSIX shell everywhere else. Taking goos
// as a parameter lets every host test the text of every platform.
func recoveryInstruction(goos, recovery, target string) string {
	if goos == "windows" {
		return windowsRecoveryInstruction(recovery, target)
	}

	return shellRecoveryInstruction(recovery, target)
}

func windowsRecoveryInstruction(recovery, target string) string {
	return fmt.Sprintf("do not rebuild; choose a distinct unused report target. If this target is safe, in PowerShell run: "+
		"[System.IO.File]::Copy(%s, %s, $false) (then delete the recovery file)",
		powershellQuote(recovery), powershellQuote(target))
}

func shellRecoveryInstruction(recovery, target string) string {
	return fmt.Sprintf("do not rebuild; choose a distinct unused report target. If this target is safe, run: "+
		"cp -n -- %s %s (then delete the recovery file)",
		shellQuote(recovery), shellQuote(target))
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// rejectArtifactAlias compares actual filesystem objects, including case, Unicode and hard-link aliases.
func rejectArtifactAlias(output, target string) error {
	if output == target {
		return &DestinationError{Subject: "report", Path: target, Reason: "aliases the output PDF"}
	}

	pdfIdentity, pdfErr := capture.IdentityOf(output)
	reportIdentity, reportErr := capture.IdentityOf(target)

	if pdfErr != nil && !errors.Is(pdfErr, os.ErrNotExist) {
		return fmt.Errorf("inspect protected PDF %q: %w", output, pdfErr)
	}

	if reportErr != nil && !errors.Is(reportErr, os.ErrNotExist) {
		return fmt.Errorf("inspect report target %q: %w", target, reportErr)
	}

	if pdfErr == nil && reportErr == nil && pdfIdentity == reportIdentity {
		return &DestinationError{Subject: "report", Path: target, Reason: "aliases the output PDF"}
	}

	return nil
}
