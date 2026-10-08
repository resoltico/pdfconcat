// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

// errNotRegularReport rejects a saved report that is a pipe, device or directory: such a read can block
// forever, and a report is a file written by --report.
var errNotRegularReport = errors.New("a saved report must be a regular file")

// runQuery answers the report command from a saved report. The file is untrusted input, decoded under the
// report package's limits; no PDF is opened and nothing is written.
func runQuery(ctx context.Context, command *cli.Command, env Env) int {
	return runQueryWith(ctx, command, env, openInput)
}

func runQueryWith(ctx context.Context, command *cli.Command, env Env, open func(context.Context, string) (*os.File, error)) int {
	request := command.ReportRequest()
	if err := request.Validate(); err != nil {
		return queryFailure(env, command, err)
	}

	path := absolutePath(env.WorkingDir, command.ReportFile)

	file, err := open(ctx, path)
	if err != nil {
		nonregularError, ok := errors.AsType[*capture.NotRegularFileError](err)
		if ok {
			return readFailure(
				env,
				command,
				path,
				"cannot read the saved report: "+errNotRegularReport.Error()+"; "+nonregularError.Error(),
				err,
			)
		}

		return readFailure(env, command, path, "cannot open the saved report: "+reasonOf(err), err)
	}

	saved, err := report.Decode(ctx, path, file)

	err = errors.Join(err, file.Close())
	if err != nil {
		return queryFailure(env, command, err)
	}

	response, err := saved.Query(request)
	if err != nil {
		return queryFailure(env, command, err)
	}

	query := report.QueryResultOf(saved, response, env.Executable, path)

	err = writeResponse(env.Stdout, command.Format, report.NewResponse(query))
	if err != nil {
		return stdoutFailed(env, err)
	}

	return 0
}

// readFailure prints a report that could not be opened or inspected; an interruption keeps its own status.
func readFailure(env Env, command *cli.Command, path, message string, err error) int {
	if isInterrupted(err) {
		found := classify(report.StageRead, err)

		return emitError(env, command.Format, string(command.Name), found.status, found.diagnostic)
	}

	return emitError(env, command.Format, string(command.Name), report.StatusFailed, report.Diagnostic{
		Stage: report.StageRead, Code: codeReportRead, Path: path, Message: message,
	})
}

// queryFailure prints a decoding or query failure with the status the report package assigns it.
func queryFailure(env Env, command *cli.Command, err error) int {
	if reportErr, ok := report.AsError(err); ok {
		diagnostic := reportErr.Diagnostic
		if diagnostic.Location == nil && reportErr.Option != "" {
			diagnostic.Location = argumentLocation(env, reportErr.Option)
		}

		if diagnostic.Code == report.CodeUnsupportedVersion {
			diagnostic.Recovery = &report.Recovery{Action: "choose_new_report", ReportFrom: "unused_report_target"}
		}

		if diagnostic.Code == report.CodeAttemptMismatch {
			diagnostic.Stage = report.StageRead
			diagnostic.Recovery = &report.Recovery{Action: "inspect_report", ReportFrom: "original_argv.report_operand"}
		}

		return emitError(env, command.Format, string(command.Name), reportErr.Status(), diagnostic)
	}

	found := classify(stageInput, err)

	return emitError(env, command.Format, string(command.Name), found.status, found.diagnostic)
}

func argumentLocation(env Env, option string) *report.Location {
	for index, argument := range env.Arguments {
		if argument == option || strings.HasPrefix(argument, option+"=") {
			return &report.Location{File: "argv", ArgvIndex: &index}
		}
	}

	return nil
}
