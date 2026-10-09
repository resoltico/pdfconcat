// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package cli defines and parses PDFConcat's command-line grammar and describes it as structured help.
// It performs no filesystem access and writes no output: Parse turns arguments into a typed Command or a
// *UsageError, and Help returns the documentation of a command as data.
package cli

import (
	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// Name identifies a command.
	Name string

	// Format is the rendering of a command's standard output.
	Format uint8

	// ProgressMode selects optional stderr observations for build and check.
	ProgressMode string

	// PlanSource says where the instructions of a build or check come from.
	PlanSource uint8

	// Command is a parsed command line. Only the fields that apply to Name are set.
	Command struct {
		// Name is the command. A help request is NameHelp whatever way it was asked for.
		Name Name
		// PlanPath is the --plan file; set only for PlanFile.
		PlanPath string
		// PlanJSON is the --plan-json text; set only for PlanInline.
		PlanJSON string
		// BaseDir is --base-dir; set only for PlanStdin and PlanInline.
		BaseDir string
		// Output is -o/--output, relative to the working directory; empty when the plan names the output.
		Output string
		// FitTo is the optional portrait target, overriding the plan declaration.
		FitTo    assembly.FitTarget
		Progress ProgressMode
		// ReportPath is --report for build and check, relative to the working directory.
		ReportPath string
		// ReportFile is the saved report that the report command reads.
		ExpectAttempt string
		ReportFile    string
		// Part is --part, the id of one contribution.
		Part string
		// View is --view: "parts" or "diagnostics".
		View string
		// SchemaName is the schema command's operand: "plan", "report" or "response".
		SchemaName string
		// HelpFor is the command a help request is about; empty for the root help.
		HelpFor Name
		// Operands are the direct PDF paths and --blank directives in command-line order, for PlanOperands.
		// Each Position is the operand's zero-based index in the arguments, excluding the executable.
		Operands []assembly.Operand
		// Page is --page, a 1-based output page; HasPage preserves explicit zero.
		Page int64
		// Offset and Limit page a View; HasOffset and HasLimit say whether they were given.
		Offset int64
		Limit  int64
		// Jobs is --jobs; 0 means unset, and an explicit value is at least 1.
		Jobs int
		// Format is the rendering requested with --format; JSON unless a valid --format text was given.
		Format Format
		// PlanSource selects which of PlanPath, PlanJSON, and Operands carries the instructions.
		PlanSource PlanSource
		// Details is --details: the complete result for build and check, full records for report.
		Details bool
		// Overwrite is --overwrite.
		Overwrite bool
		// HasPage, HasOffset and HasLimit distinguish a given 0 from an absent option.
		HasPage, HasOffset, HasLimit bool
	}
)

const (
	// NameBuild assembles a PDF.
	NameBuild Name = "build"
	// NameCheck validates a job without creating a PDF.
	NameCheck Name = "check"
	// NameReport queries a saved report.
	NameReport Name = "report"
	// NameSchema prints a JSON Schema.
	NameSchema Name = "schema"
	// NameVersion prints version information.
	NameVersion Name = "version"
	// NameHelp prints help.
	NameHelp Name = "help"

	// rootContext is the position before a command is named; it accepts only --help, --version, and --format.
	rootContext Name = ""

	// FormatJSON is compact JSON, the default.
	FormatJSON Format = 0
	// FormatText is human-readable text.
	FormatText Format = 1

	// ProgressAuto retains interactive text with quiet redirected stderr.
	ProgressAuto ProgressMode = "auto"
	// ProgressJSON requests machine observations on stderr.
	ProgressJSON ProgressMode = "json"
	// ProgressNone disables optional observations.
	ProgressNone ProgressMode = "none"

	// PlanNone is the absence of instructions: report, schema, version, and help commands.
	PlanNone PlanSource = 0
	// PlanFile is --plan FILE.
	PlanFile PlanSource = 1
	// PlanStdin is --plan -.
	PlanStdin PlanSource = 2
	// PlanInline is --plan-json JSON.
	PlanInline PlanSource = 3
	// PlanOperands is a direct sequence of PDF paths and --blank.
	PlanOperands PlanSource = 4

	// SchemaPlan is the schema command's name for the plan schema.
	SchemaPlan = "plan"
	// SchemaReport is the schema command's name for the saved-report schema.
	SchemaReport = "report"
	// SchemaResponse describes every structured command response.
	SchemaResponse = "response"
)

// ReportRequest adapts parsed option presence into the report package's pure request contract.
func (c *Command) ReportRequest() report.Request {
	request := report.Request{View: c.View, Details: c.Details, ExpectAttempt: c.ExpectAttempt}
	if c.Part != "" {
		request.Part = &c.Part
	}

	if c.HasPage {
		request.Page = &c.Page
	}

	if c.HasOffset {
		request.Offset = &c.Offset
	}

	if c.HasLimit {
		request.Limit = &c.Limit
	}

	return request
}
