// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package cli defines and parses PDFConcat's public command-line grammar.
package cli

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// BuildInfo contains release metadata shown by --version.
type BuildInfo struct {
	Version    string
	Commit     string
	CommitDate string
}

// StdinPlan is the --plan value that reads the plan from standard input.
const StdinPlan = "-"

// Request is the typed assembly request produced by command-line parsing.
type Request struct {
	// Output is the destination from -o; empty when the plan names it.
	Output string
	// PlanPath is the --plan value, or empty for a direct sequence.
	PlanPath string
	// Sequence is the direct command-line sequence; empty when PlanPath is set.
	Sequence assembly.Sequence
	// Blank holds the run-wide blank-page defaults given by --blank-* options.
	Blank assembly.BlankStyle
	// Overwrite permits replacing an existing output.
	Overwrite bool
	// DryRun inspects and reports without creating output.
	DryRun bool
	// JSON selects machine-readable reporting.
	JSON bool
}

// Action is what the process should do for a parsed command line.
type Action uint8

// Actions of a parsed command line.
const (
	ActionAssemble Action = iota + 1
	ActionHelp
	ActionVersion
	ActionPrintSchema
)

// Command is a parsed command line.
type Command struct {
	Action  Action
	Request Request
}

// UsageError reports a malformed command line; the process exits with status 2.
type UsageError struct {
	Message string
}

// Error returns the message.
func (e *UsageError) Error() string {
	return e.Message
}

// Usagef formats a UsageError.
func Usagef(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}
