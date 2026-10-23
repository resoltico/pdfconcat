// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core"
)

// Host supplies process capabilities without coupling the CLI to process globals.
type Host struct {
	WorkingDir      func() (string, error)
	ReleaseOverride string
}

func operationHint(name string) core.Operation {
	if name == "check" {
		return core.Check
	}
	if name == "build" {
		return core.Build
	}
	return ""
}

// Run adapts arguments, streams and host capabilities to the shared core.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, host Host) int {
	command, failure := Parse(args)
	render := func(r core.Result) int {
		if err := Render(stdout, r, command.Format); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return core.ExitCode(r)
	}
	if failure != nil {
		return render(*failure)
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
		data, err := core.Schema(core.SchemaKind(command.Schema))
		if err == nil {
			_, err = stdout.Write(append(data, '\n'))
		}
		return query(err)
	}
	if command.Name == "version" {
		v, err := core.Version(host.ReleaseOverride)
		if err == nil {
			if command.Format == "text" {
				_, err = fmt.Fprintf(stdout, "pdfconcat %s (%s, %s, %s)\n", v.Version, v.Commit, v.Go, v.Platform)
			} else {
				err = JSON(stdout, v)
			}
		}
		return query(err)
	}
	if host.WorkingDir == nil {
		return render(core.InvocationFailure(operationHint(command.Name), core.WorkingDirectoryUnavailable, "Cannot determine working directory: no host provider.", ""))
	}
	cwd, err := host.WorkingDir()
	if err != nil {
		message := strings.ToValidUTF8(err.Error(), "�")
		message = strings.ReplaceAll(message, "\x00", `\x00`)
		return render(core.InvocationFailure(operationHint(command.Name), core.WorkingDirectoryUnavailable, "Cannot determine working directory: "+message, ""))
	}

	var input core.Input
	if command.Plan != "" {
		if command.Plan == "-" {
			data, err := readStdin(ctx, stdin, core.MaxPlanBytes)
			if err != nil {
				kind := core.PlanUnreadable
				if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					kind = core.Interrupted
				}
				return render(core.InvocationFailure(operationHint(command.Name), kind, strings.ReplaceAll(strings.ToValidUTF8(err.Error(), "�"), "\x00", `\x00`), ""))
			}
			input = core.PlanBytes{Data: data, Label: "<stdin>", BaseDir: command.BaseDir}
		} else {
			input = core.PlanFile{Path: command.Plan}
		}
	} else {
		items := make(core.Items, 0, len(command.Operands))
		for _, operand := range command.Operands {
			ref := fmt.Sprintf("argv:%d", operand.Index)
			if operand.Blank {
				items = append(items, core.BlankItem{Ref: ref})
			} else {
				items = append(items, core.SourceItem{Ref: ref, Path: operand.Path})
			}
		}
		input = items
	}

	request := core.Request{Operation: operationHint(command.Name), Input: input, WorkingDir: cwd, Output: command.Output, Report: command.Report, Overwrite: command.Overwrite}
	if command.FitTo != "" {
		size, _ := core.ParseSheet(command.FitTo)
		request.FitTo = &size
	}
	return render(core.Execute(ctx, request))
}
