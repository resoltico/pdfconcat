// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package main wires the pdfconcat command-line executable.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/plan"
)

// Process exit statuses.
const (
	exitOperationalFailure = 1
	exitUsage              = 2
)

// Build metadata, populated by release ldflags.
var (
	version    = "dev"
	commit     = "none"
	commitDate = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:])

	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string) int {
	command, err := cli.Parse(args)
	if err != nil {
		return reportFailure(err)
	}

	switch command.Action {
	case cli.ActionHelp:
		return printText(cli.HelpText())
	case cli.ActionVersion:
		return printText(cli.VersionText(cli.BuildInfo{Version: version, Commit: commit, CommitDate: commitDate}))
	case cli.ActionPrintSchema:
		return printText(plan.Schema())
	case cli.ActionAssemble:
		return assemble(ctx, &command.Request)
	default:
		return reportFailure(fmt.Errorf("internal error: unhandled command action %d", command.Action))
	}
}

func assemble(ctx context.Context, request *cli.Request) int {
	engine, err := pdfengine.NewPDFCPU()
	if err != nil {
		return reportFailure(fmt.Errorf("initialize PDF engine: %w", err))
	}

	runner := app.New(engine, app.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Progress: progressStream()})

	err = runner.Run(ctx, *request)
	if err != nil {
		return reportFailure(err)
	}

	return 0
}

// progressStream returns stderr when it is an interactive terminal, so progress
// never pollutes redirected logs.
func progressStream() io.Writer {
	info, err := os.Stderr.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}

	return os.Stderr
}

func printText(text string) int {
	if _, err := os.Stdout.WriteString(text); err != nil {
		return exitOperationalFailure
	}

	return 0
}

func reportFailure(err error) int {
	if usage, ok := errors.AsType[*cli.UsageError](err); ok {
		fmt.Fprintln(os.Stderr, "pdfconcat:", usage)
		fmt.Fprintln(os.Stderr, "Run 'pdfconcat --help' for usage.")

		return exitUsage
	}

	fmt.Fprintln(os.Stderr, "pdfconcat:", err)

	return exitOperationalFailure
}
