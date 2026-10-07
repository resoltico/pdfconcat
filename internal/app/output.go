// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// committedState is the bounded record written to standard error when standard output cannot be
	// written after a command already did its work: it says what was published, so the caller is
	// never left to guess.
	committedState struct {
		RecoveryBasename      string               `json:"recovery_basename,omitempty"`
		RecoveryDirectoryFrom string               `json:"recovery_directory_from,omitempty"`
		Kind                  string               `json:"kind"`
		Command               string               `json:"command"`
		Status                report.Status        `json:"status"`
		Output                string               `json:"output,omitempty"`
		ReportStatus          report.WriteState    `json:"report_status"`
		ReportPath            string               `json:"report_path,omitempty"`
		RecoveryReport        string               `json:"recovery_report,omitempty"`
		RecoveryState         report.RecoveryState `json:"recovery_state,omitempty"`
		StdoutError           string               `json:"stdout_error"`
		Truncated             bool                 `json:"previews_truncated,omitzero"`
		Published             bool                 `json:"published"`
	}

	// versionInfo is the payload of the version command.
	versionInfo struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
		Go      string `json:"go"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}
)

const (
	// binaryName prefixes warnings written to standard error.
	binaryName = "pdfconcat"
	// committedStateKind is the kind of a committedState record.
	committedStateKind = "committed_state"
)

// encodeLine is the compact JSON of value and a newline. The values encoded here are plain data that always
// encode; the fallback exists so that no encoding failure can end a command without output.
func encodeLine(value any) []byte {
	data, err := report.Encode(value)
	if err != nil {
		return []byte(`{"kind":"encoding_failed"}` + "\n")
	}

	return append(data, '\n')
}

// writeResponse writes a result to w in the requested format: compact JSON followed by a newline, or text.
func writeResponse(w io.Writer, format cli.Format, response report.Response) error {
	if format == cli.FormatText {
		return response.RenderText(w)
	}

	_, err := w.Write(encodeLine(response))
	if err != nil {
		return fmt.Errorf("write result: %w", err)
	}

	return nil
}

// finishCommand prints the report of a build or check and returns the exit code. Standard output carries
// the bounded summary, or the complete report with --details. If standard output cannot be written, the
// committed state goes to standard error, and a run that had succeeded is a failure.
func finishCommand(env Env, command *cli.Command, rep *report.Report) int {
	response := report.NewResponse(rep.Summary())
	if command.Details {
		response = report.NewResponse(rep)
	}

	code := rep.Status.ExitCode()

	err := writeResponse(env.Stdout, command.Format, response)
	if err == nil {
		return code
	}

	stateOnStderr(env, command.Name, rep, err)

	return max(code, 1)
}

// stateOnStderr reports what the command did when standard output failed.
func stateOnStderr(env Env, command cli.Name, rep *report.Report, cause error) {
	state := committedState{
		Kind:           committedStateKind,
		Command:        string(command),
		Status:         rep.Status,
		Published:      rep.Publication.Published,
		Output:         rep.Publication.Output,
		ReportStatus:   rep.Publication.ReportStatus,
		ReportPath:     rep.Publication.ReportPath,
		RecoveryReport: rep.Publication.RecoveryReport,
		RecoveryState:  rep.Publication.RecoveryState,
		StdoutError:    boundedMessage(cause.Error()),
	}

	if len(encodeLine(state)) <= report.SummaryBytes {
		writeStderr(env, encodeLine(state))
		return
	}

	summary := rep.Summary()
	state.Output = summary.Publication.Output
	state.ReportPath = summary.Publication.ReportPath
	state.RecoveryReport = summary.Publication.RecoveryReport

	state.RecoveryBasename, state.RecoveryDirectoryFrom = summary.RecoveryBasename, summary.RecoveryDirectoryFrom
	if _, truncated := report.BoundPreview(state.RecoveryReport); truncated {
		state.RecoveryBasename = filepath.Base(rep.Publication.RecoveryReport)
		state.RecoveryDirectoryFrom = "original_report_argument"
		state.RecoveryReport = ""
		state.Truncated = true
	}

	for _, value := range []*string{&state.Command, &state.Output, &state.ReportPath, &state.StdoutError} {
		preview, truncated := report.BoundPreview(*value)
		*value = preview
		state.Truncated = state.Truncated || truncated
	}

	state.Truncated = state.Truncated || len(summary.TruncatedFields) > 0
	writeStderr(env, encodeLine(state))
}

// stdoutFailed tells standard error that the result could not be written, and returns the failure status.
func stdoutFailed(env Env, err error) int {
	writeStderr(env, []byte(binaryName+": cannot write standard output: "+boundedMessage(err.Error())+"\n"))

	return 1
}

// warn writes one warning line to standard error.
func warn(env Env, message string) {
	writeStderr(env, []byte(binaryName+": warning: "+message+"\n"))
}

// writeStderr writes to standard error; a broken stream has nowhere further to report, so the error is dropped.
func writeStderr(env Env, data []byte) {
	if env.Stderr == nil {
		return
	}

	_, writeErr := env.Stderr.Write(data)
	_ = writeErr
}

// emitError prints a structured failure that happened before any layout existed and returns its exit code.
func emitError(env Env, format cli.Format, name string, status report.Status, diagnostics ...report.Diagnostic) int {
	rep := report.NewErrorReport(name, status, diagnostics...)

	err := writeResponse(env.Stdout, format, report.NewResponse(rep.Summary()))
	if err != nil {
		_ = stdoutFailed(env, err)
	}

	return status.ExitCode()
}

// usageFailure prints a rejected command line.
func usageFailure(env Env, err error) int {
	usage, ok := errors.AsType[*cli.UsageError](err)
	if !ok {
		return emitError(env, cli.FormatJSON, binaryName, report.StatusInvalid,
			report.Diagnostic{Stage: report.StageUsage, Code: codeCommandBad, Message: err.Error()})
	}

	name := string(usage.Command)
	if name == "" {
		name = binaryName
	}

	return emitError(env, usage.Format, name, report.StatusInvalid, usage.Diagnostics...)
}

// runSchema prints the raw embedded JSON Schema of the plan or of a report.
func runSchema(command *cli.Command, env Env) int {
	var text string

	switch command.SchemaName {
	case cli.SchemaPlan:
		text = plan.Schema()
	case cli.SchemaReport:
		text = report.Schema()
	default:
		return emitError(env, command.Format, string(command.Name), report.StatusInvalid, report.Diagnostic{
			Stage: report.StageUsage, Code: codeSchemaUnknown,
			Message: fmt.Sprintf("unknown schema %q; use %s or %s", command.SchemaName, cli.SchemaPlan, cli.SchemaReport),
		})
	}

	_, err := io.WriteString(env.Stdout, text)
	if err != nil {
		return stdoutFailed(env, err)
	}

	return 0
}

// runVersion prints the release metadata and the toolchain and platform it was built for.
func runVersion(command *cli.Command, env Env) int {
	info := versionInfo{
		Version: env.Build.Version, Commit: env.Build.Commit, Date: env.Build.CommitDate,
		Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
	}

	return emit(env, func(out io.Writer) error {
		if command.Format == cli.FormatText {
			_, err := fmt.Fprintf(out, "%s %s\ncommit: %s\ndate: %s\n%s %s/%s\n",
				binaryName, info.Version, info.Commit, info.Date, info.Go, info.OS, info.Arch)
			if err != nil {
				return fmt.Errorf("write version: %w", err)
			}

			return nil
		}

		return writeJSONLine(out, info)
	})
}

// runHelp prints the structured help of the root or of one command.
func runHelp(command *cli.Command, env Env) int {
	doc := cli.Help(command.HelpFor)

	return emit(env, func(out io.Writer) error {
		if command.Format == cli.FormatText {
			err := doc.RenderText(out)
			if err != nil {
				return fmt.Errorf("write help: %w", err)
			}

			return nil
		}

		return writeJSONLine(out, doc)
	})
}

// emit runs a renderer against standard output and maps a write failure to exit 1.
func emit(env Env, render func(io.Writer) error) int {
	err := render(env.Stdout)
	if err != nil {
		return stdoutFailed(env, err)
	}

	return 0
}

// writeJSONLine writes value as compact JSON and a newline.
func writeJSONLine(w io.Writer, value any) error {
	_, err := w.Write(encodeLine(value))
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}
