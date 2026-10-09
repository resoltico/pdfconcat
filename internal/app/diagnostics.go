// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
)

// problem is a diagnostic together with the status it implies.
type problem struct {
	status     report.Status
	diagnostic report.Diagnostic
}

// Stages and codes produced by this package. Messages are not stable; codes are.
const (
	// stageInput is reading files the job names: plan, fonts and source PDFs.
	stageInput report.Stage = "input"
	// stageOutput is destination policy and file-role conflicts.
	stageOutput report.Stage = "output"
	// stageLayout is text placement; it shares the layout package's stage name.
	stageLayout report.Stage = report.Stage(layout.StageLayout)
	// stageAssemble is building and verifying the final PDF.
	stageAssemble report.Stage = "assemble"
	// stagePublish is making the PDF and the report visible.
	stagePublish report.Stage = "publish"

	codeInterrupted      report.Code = "interrupted"
	codeOutputMissing    report.Code = "output_missing"
	codeOutputInvalid    report.Code = "output_destination_invalid"
	codeArtifactTarget   report.Code = "artifact_target_invalid"
	codeAliasConflict    report.Code = "alias_conflict"
	codePlanUnreadable   report.Code = "plan_unreadable"
	codeBaseDirInvalid   report.Code = "base_dir_invalid"
	codeSourceUnreadable report.Code = "source_unreadable"
	codeSourceNotRegular report.Code = "source_not_regular"
	codeSourceChanged    report.Code = "source_changed"
	codeFontUnreadable   report.Code = "font_unreadable"
	codeFontInvalid      report.Code = "font_invalid"
	codeFontTooLarge     report.Code = "font_too_large"
	codeScratchFailed    report.Code = "scratch_failed"
	codeDiskFull         report.Code = "disk_full"
	codeJobInvalid       report.Code = "job_invalid"
	codePathInvalid      report.Code = "path_invalid"
	codeEngineStart      report.Code = "engine_unavailable"
	codeOperationFailed  report.Code = "operation_failed"
	codePublishFailed    report.Code = "publish_failed"
	codeReportWrite      report.Code = "report_write_failed"
	codeReportPublish    report.Code = "report_publish_failed"
	codeWorkingDirUTF8   report.Code = "working_directory_invalid_utf8"
	codeWorkingDir       report.Code = "working_directory_unavailable"
	codeCommandBad       report.Code = "command_unsupported"
	codeSchemaUnknown    report.Code = "schema_unknown"
	codeReportRead       report.Code = "report_read_failed"
	codeRenderFailed     report.Code = "render_failed"

	// maxMessageRunes bounds the free text of a diagnostic built from a foreign error.
	maxMessageRunes = 600
)

// isInterrupted reports whether err is a cancellation.
func isInterrupted(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// classify turns an error from capture, the engine, typesetting or publication into a diagnostic at stage
// and the status that error implies. Cancellation wins over every other classification.
func classify(stage report.Stage, err error) problem {
	found := classifyKind(stage, err)

	if found.status == report.StatusFailed && capture.IsDiskFull(err) {
		found.diagnostic.Code = codeDiskFull
	}

	return found
}

func classifyKind(stage report.Stage, err error) problem {
	diagnostic := report.Diagnostic{Severity: report.SeverityError, Stage: stage, Message: boundedMessage(err.Error())}

	var (
		alias      *capture.AliasError
		target     *capture.ArtifactTargetError
		notRegular *capture.NotRegularFileError
		changed    *capture.SourceChangedError
		unreadable *capture.SourceError
		scratch    *capture.ScratchError
		engine     *pdfengine.Error
	)

	switch {
	case isInterrupted(err):
		diagnostic.Code, diagnostic.Message = codeInterrupted, "the command was interrupted before it finished"

		return problem{diagnostic: diagnostic, status: report.StatusInterrupted}
	case errors.As(err, &alias):
		diagnostic.Code, diagnostic.Path = codeAliasConflict, alias.Path

		return problem{diagnostic: diagnostic, status: report.StatusInvalid}
	case errors.As(err, &target):
		diagnostic.Code, diagnostic.Path = codeArtifactTarget, target.Path

		return problem{diagnostic: diagnostic, status: report.StatusInvalid}
	case errors.As(err, &notRegular):
		diagnostic.Code, diagnostic.Path = codeSourceNotRegular, notRegular.Path

		return problem{diagnostic: diagnostic, status: report.StatusInvalid}
	case errors.As(err, &changed):
		diagnostic.Code, diagnostic.Path = codeSourceChanged, changed.Path

		return problem{diagnostic: diagnostic, status: report.StatusFailed}
	case errors.As(err, &scratch):
		diagnostic.Code = codeScratchFailed

		return problem{diagnostic: diagnostic, status: report.StatusFailed}
	case errors.As(err, &engine):
		diagnostic.Code, diagnostic.Message = report.Code(engine.Code), boundedMessage(engine.Err.Error())
		if engine.Reason != "" && engine.Reason != pdfengine.ReasonTotalMismatch {
			return problem{diagnostic: diagnostic, status: report.StatusInvalid}
		}

		return problem{diagnostic: diagnostic, status: engineStatus(engine.Code)}
	case errors.As(err, &unreadable):
		diagnostic.Code, diagnostic.Path = codeSourceUnreadable, unreadable.Path
		diagnostic.Message = unreadable.Operation + ": " + reasonOf(unreadable.Err)

		return problem{diagnostic: diagnostic, status: report.StatusFailed}
	default:
		diagnostic.Code = codeOperationFailed

		return problem{diagnostic: diagnostic, status: report.StatusFailed}
	}
}

// engineStatus maps an engine code to a status. A source used in a way the engine cannot honor is an
// invalid instruction; every other engine failure (an unreadable, malformed or encrypted source, a backend
// or verification failure) is an input or backend failure. Cancellation never reaches here: classify
// recognizes it first.
func engineStatus(code pdfengine.Code) report.Status {
	if code == pdfengine.CodePartialRange || code == pdfengine.CodeLegacyDestsRepeated ||
		code == pdfengine.CodeSignatureUnsupported || code == pdfengine.CodeFormUnsupported || code == pdfengine.CodeFitUnsupported {
		return report.StatusInvalid
	}

	return report.StatusFailed
}

// reasonOf is the operating system's reason inside an error, without the path it already names elsewhere.
func reasonOf(err error) string {
	pathErr, ok := errors.AsType[*fs.PathError](err)
	if ok {
		return boundedMessage(pathErr.Err.Error())
	}

	return boundedMessage(err.Error())
}

// boundedMessage cuts a foreign error message to a size a diagnostic can carry.
func boundedMessage(text string) string {
	runes := []rune(text)
	if len(runes) <= maxMessageRunes {
		return text
	}

	return string(runes[:maxMessageRunes]) + "…"
}

// planProblem classifies a plan decoding failure. Anything but a *plan.Error is an unexpected failure.
func planProblem(err error) problem {
	planErr, ok := errors.AsType[*plan.Error](err)
	if !ok {
		return classify(stageInput, err)
	}

	diagnostic := planDiagnostic(planErr)

	if planErr.Code == plan.CodeInterrupted {
		return problem{diagnostic: diagnostic, status: report.StatusInterrupted}
	}

	if planErr.Code == plan.CodeReadFailed {
		return problem{diagnostic: diagnostic, status: report.StatusFailed}
	}

	return problem{diagnostic: diagnostic, status: report.StatusInvalid}
}

// planDiagnostic adapts a decoder failure to the application's report format.
func planDiagnostic(err *plan.Error) report.Diagnostic {
	return report.Diagnostic{
		Severity: report.SeverityError,
		Stage:    report.Stage(err.Stage),
		Code:     report.Code(err.Code),
		Location: report.LocationOf(err.Location),
		Message:  err.Message,
	}
}

// sharedMessage appends the other places a shared problem applies to.
func sharedMessage(item *assembly.Error) string {
	message := item.Message
	if item.Affected <= 1 && len(item.Related) == 0 {
		return message
	}

	also := make([]string, 0, len(item.Related))

	for _, related := range item.Related {
		also = append(also, related.Pointer)
	}

	message += fmt.Sprintf(" (%d entries use this", item.Affected)
	if len(also) > 0 {
		message += "; also at " + strings.Join(also, ", ")
	}

	return message + ")"
}
