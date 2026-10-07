// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"errors"

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
	path := absolutePath(env.WorkingDir, command.ReportFile)

	file, err := openInput(ctx, path)
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

		return readFailure(env, command, path, "cannot open the saved report: "+err.Error(), err)
	}

	saved, err := report.Decode(ctx, path, file)

	err = errors.Join(err, file.Close())
	if err != nil {
		return queryFailure(env, command, err)
	}

	response, err := saved.Query(requestOf(command))
	if err != nil {
		return queryFailure(env, command, err)
	}

	err = writeResponse(env.Stdout, command.Format, response)
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
		return emitError(env, command.Format, string(command.Name), reportErr.Status(), reportErr.Diagnostic)
	}

	found := classify(stageInput, err)

	return emitError(env, command.Format, string(command.Name), found.status, found.diagnostic)
}

// requestOf converts the parsed selectors into a query request.
func requestOf(command *cli.Command) report.Request {
	request := report.Request{View: command.View, Details: command.Details}

	if command.Part != "" {
		request.Part = &command.Part
	}

	if command.Page != 0 {
		request.Page = &command.Page
	}

	if command.HasOffset {
		request.Offset = &command.Offset
	}

	if command.HasLimit {
		request.Limit = &command.Limit
	}

	return request
}
