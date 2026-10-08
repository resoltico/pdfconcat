// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package main is the pdfconcat executable: it connects the process (signals, streams, working directory,
// build metadata) to internal/app and exits with the code the command decides.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

// Release or snapshot version, set at link time with -ldflags "-X main.version=...".
// Ordinary builds use the project version; the toolchain supplies commit provenance (see app.ResolveBuild).
var version string

func main() {
	os.Exit(run())
}

func run() int {
	// A write to a closed standard output must come back as an error the command can report, not end the
	// process with SIGPIPE before it can say what it already did.
	signal.Ignore(syscall.SIGPIPE)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// After the first signal restore the default behavior, so a second one ends a command that is slow to stop.
	go func() {
		<-ctx.Done()
		stop()
	}()

	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "" // the command reports the missing working directory when it needs one
	}

	executable := usableExecutable(os.Executable())

	info, _ := debug.ReadBuildInfo()

	env := app.Env{
		Executable: executable,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Progress:   progressStream(),
		WorkingDir: workingDir,
		Build:      app.ResolveBuild(version, info),
	}

	return app.New(func() (app.Engine, error) {
		engine, engineErr := pdfengine.New()
		if engineErr != nil {
			return nil, fmt.Errorf("create the PDF engine: %w", engineErr)
		}

		return engine, nil
	}).Run(ctx, os.Args[1:], env)
}

// progressStream returns standard error when it is an interactive terminal on any supported operating
// system, so progress never reaches a redirected log or standard output.
func progressStream() io.Writer {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		return os.Stderr
	}

	return nil
}

// usableExecutable rejects a path returned alongside an error; it may be relative or incomplete.
func usableExecutable(path string, err error) string {
	if err != nil || !utf8.ValidString(path) {
		return ""
	}

	return path
}
