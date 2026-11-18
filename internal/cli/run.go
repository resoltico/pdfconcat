// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/legal"
)

// Host supplies process capabilities without coupling the CLI to process globals.
type Host struct {
	BuildMetadata     BuildMetadata
	InspectionWorkers int
	WorkingDir        func() (string, error)
	SourceDateEpoch   func() (string, bool)
	ReleaseOverride   string
}

func operationHint(name string) app.Operation {
	if name == "check" {
		return app.Check
	}
	if name == "build" {
		return app.Build
	}
	return ""
}

// Run adapts arguments, streams and host capabilities to the shared app.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, host Host) int {
	command, failure := Parse(args)
	render := func(r app.Result) int {
		if err := Render(stdout, stderr, r, command.JSON); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return ExitCode(r.Status)
	}
	if failure != nil {
		if command.JSON {
			return render(*failure)
		}
		for _, d := range Summary(*failure).Diagnostics {
			if _, err := fmt.Fprintln(stderr, diagnosticText(d, Summary(*failure).Report)); err != nil {
				return 1
			}
		}
		help := "pdfconcat help"
		if knownCommand(command.Name) {
			help += " " + command.Name
		}
		if _, err := fmt.Fprintf(stderr, "Run %q for usage.\n", help); err != nil {
			return 1
		}
		return ExitCode(failure.Status)
	}
	query := func(err error) int {
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if command.Help {
		_, err := io.WriteString(stdout, Help(command.Name))
		return query(err)
	}
	if command.Name == "schema" {
		data, err := app.Schema(app.SchemaKind(command.Schema))
		if err == nil {
			_, err = stdout.Write(append(data, '\n'))
		}
		return query(err)
	}
	if command.Name == "licenses" {
		text, err := legal.Text()
		if err == nil {
			_, err = io.WriteString(stdout, text)
		}
		return query(err)
	}
	if command.Name == "version" {
		v, err := Version(host.BuildMetadata, host.ReleaseOverride)
		if err == nil {
			if !command.JSON {
				_, err = fmt.Fprintf(stdout, "pdfconcat %s (%s, %s, %s)\n", v.Version, v.Commit, v.Go, v.Platform)
			} else {
				err = JSON(stdout, v)
			}
		}
		return query(err)
	}
	if host.WorkingDir == nil {
		return render(InvocationFailure(operationHint(command.Name), WorkingDirectoryUnavailable, "Cannot determine working directory: no host provider.", ""))
	}
	cwd, err := host.WorkingDir()
	if err != nil {
		message := strings.ToValidUTF8(err.Error(), "�")
		message = strings.ReplaceAll(message, "\x00", `\x00`)
		return render(InvocationFailure(operationHint(command.Name), WorkingDirectoryUnavailable, "Cannot determine working directory: "+message, ""))
	}

	var input app.Input
	if command.Plan != "" {
		if command.Plan == "-" {
			data, err := readStdin(ctx, stdin, app.MaxPlanBytes)
			if err != nil {
				kind := PlanUnreadable
				if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					kind = Interrupted
				}
				return render(InvocationFailure(operationHint(command.Name), kind, strings.ReplaceAll(strings.ToValidUTF8(err.Error(), "�"), "\x00", `\x00`), ""))
			}
			input = app.PlanBytes{Data: data, Label: "<stdin>", BaseDir: command.BaseDir}
		} else {
			input = app.PlanFile{Path: command.Plan}
		}
	} else {
		items := make(app.Items, 0, len(command.Operands))
		for _, operand := range command.Operands {
			ref := fmt.Sprintf("argv:%d", operand.Index)
			if operand.Blank {
				items = append(items, app.BlankItem{Ref: ref})
			} else {
				items = append(items, app.SourceItem{Ref: ref, Path: operand.Path})
			}
		}
		input = items
	}

	var epoch *string
	if host.SourceDateEpoch != nil {
		if value, present := host.SourceDateEpoch(); present {
			epoch = &value
		}
	}
	request := app.Request{InspectionWorkers: host.InspectionWorkers, SourceDateEpoch: epoch, Operation: operationHint(command.Name), Input: input, WorkingDir: cwd, Output: command.Output, Report: command.Report, Overwrite: command.Overwrite}
	request.Fitting = command.Fitting
	return render(app.Execute(ctx, request))
}
