// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package app runs PDFConcat commands: it connects the parsed command line to plan decoding, resource
// capture, inspection, layout, assembly, report production and publication, and decides the outcome that
// every command prints and the process exits with.
//
// # Pipeline
//
// A build or check runs these stages in order and stops at the first stage that fails, reporting every
// problem that stage found:
//
//	decode the job once (file, stdin, inline JSON, or command-line operands)
//	flatten it: resolve paths and appearance values, keeping every origin
//	check the destination and register every plan, font, source, output and report file, so that no
//	  file is used in two roles
//	capture fonts and load them (before any PDF is touched)
//	capture each distinct source into a private workspace and inspect the copy, with bounded concurrency
//	resolve page geometry; check retains only placement geometry, while build shapes and writes
//	  each distinct generated page immediately into the shared resource document
//	check: describe the layout in a report
//	build: assemble the sources and completed resource into a staged file, stage the report,
//	  recheck both destinations, publish the PDF and then the report, remove the workspace
//
// # Outcomes and exit codes
//
// Every command ends in a [report.Status]: ok (0), invalid instructions (2), I/O, backend or publication
// failure (1), or interruption (130). Standard output carries a compact JSON summary by default, the
// complete report with --details, or the same content as text with --format text. A failure report
// requested with --report is written to a new file only until every input is known; afterwards
// --overwrite governs it exactly as it governs the PDF.
//
// # Publication
//
// The PDF is published before the report. If the PDF is published and the report cannot be, the staged
// report stays at a recovery path, the outcome is a failure with published true, and the diagnostic gives
// a rebuild-free recovery command. A signal that arrives after the PDF is visible does not change the
// outcome: the result describes what was committed.
package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// Engine is the PDF capability the pipeline needs. [pdfengine.Engine] implements it; tests substitute
	// a fake to inject failures at each stage.
	Engine interface {
		// Inspect reads one captured source and reports its facts.
		Inspect(ctx context.Context, path string) (pdfengine.SourceInfo, error)
		// Assemble writes and verifies the final document described by request.
		Assemble(ctx context.Context, request *pdfengine.AssembleRequest) error
	}

	// EngineFactory creates the engine on first use, so commands that never touch a PDF (help, version,
	// schema, report) do not initialize one.
	EngineFactory func() (Engine, error)

	// BuildInfo is the release metadata of the executable.
	BuildInfo struct {
		Version    string
		Commit     string
		CommitDate string
	}

	// Env is everything a command may touch outside its arguments. Nothing in this package reads a
	// process-global stream, directory or environment variable.
	Env struct {
		Stdin  io.Reader
		Stdout io.Writer
		// Stderr receives scratch-cleanup warnings and the committed state when Stdout is broken.
		Stderr io.Writer
		// Progress receives stage lines; nil unless standard error is an interactive terminal.
		Progress io.Writer
		// WorkingDir is the absolute directory relative command-line paths resolve against.
		Executable string
		WorkingDir string
		Build      BuildInfo
		Arguments  []string
	}

	// App runs commands.
	App struct {
		newEngine EngineFactory
		// beforeCommit, when set, runs after the report is staged and before publication begins. Only
		// tests set it (export_test.go): it is where a test changes the world between the last check and the commit.
		beforeCommit func()
		// afterPDFCommit is the deterministic test-only boundary after PDF visibility, before report checks.
		afterPDFCommit func()
	}
)

// New returns an App that creates its engine with newEngine when a command first needs it.
func New(newEngine EngineFactory) *App {
	return &App{newEngine: newEngine}
}

// Run parses args (without the executable name) and executes the command. It returns the process exit code.
func (a *App) Run(ctx context.Context, args []string, env Env) int {
	env.Arguments = args

	command, err := cli.Parse(args)
	if err != nil {
		return usageFailure(env, err)
	}

	return a.Execute(ctx, &command, env)
}

// Execute runs a parsed command and returns the process exit code.
func (a *App) Execute(ctx context.Context, command *cli.Command, env Env) int {
	diagnostic := workingDirectoryDiagnostic(command.Name, env.WorkingDir)
	if diagnostic != nil && command.Name != cli.NameBuild && command.Name != cli.NameCheck {
		return emitError(env, command.Format, string(command.Name), report.StatusFailed, *diagnostic)
	}

	switch command.Name {
	case cli.NameBuild, cli.NameCheck:
		return a.assemble(ctx, command, env)
	case cli.NameReport:
		return runQuery(ctx, command, env)
	case cli.NameSchema:
		return runSchema(command, env)
	case cli.NameVersion:
		return runVersion(command, env)
	case cli.NameHelp:
		return runHelp(command, env)
	default:
		return emitError(env, command.Format, binaryName, report.StatusInvalid, report.Diagnostic{
			Stage: report.StageUsage, Code: codeCommandBad, Message: fmt.Sprintf("command %q is not supported", string(command.Name)),
		})
	}
}

// workingDirectoryDiagnostic validates the pathname only for commands that resolve filesystem paths.
func workingDirectoryDiagnostic(command cli.Name, dir string) *report.Diagnostic {
	if command != cli.NameBuild && command != cli.NameCheck && command != cli.NameReport {
		return nil
	}

	if !utf8.ValidString(dir) {
		return &report.Diagnostic{
			Stage: stageInput, Code: codeWorkingDirUTF8,
			Message: "the working directory pathname is not valid UTF-8; run from a directory whose pathname is valid Unicode",
		}
	}

	if !filepath.IsAbs(dir) {
		return &report.Diagnostic{
			Stage: stageInput, Code: codeWorkingDir,
			Message: "the working directory is unavailable (it may have been deleted); run from an existing directory",
		}
	}

	return nil
}
