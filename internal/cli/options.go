// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import "slices"

type (
	// optionKind says how an option takes its argument.
	optionKind uint8

	// optionSpec is the single description of an option: the parser, the help, and the documentation all
	// derive from it.
	optionSpec struct {
		// apply records the option in the parser; value is empty for flags and actions.
		apply func(p *parser, index int, value string) error
		name  string
		// short is the one-letter alias, if any.
		short string
		// value is the placeholder shown in help for a value option.
		value   string
		summary string
		// commands are the command contexts the option applies to.
		commands []Name
		kind     optionKind
		// allowStdin accepts a separate "-" value; allowEmpty accepts an empty value.
		allowStdin, allowEmpty bool
	}
)

const (
	optPlan      = "--plan"
	optPlanJSON  = "--plan-json"
	optBaseDir   = "--base-dir"
	optOutput    = "--output"
	optOverwrite = "--overwrite"
	optReport    = "--report"
	optDetails   = "--details"
	optJobs      = "--jobs"
	optFormat    = "--format"
	optPart      = "--part"
	optPage      = "--page"
	optView      = "--view"
	optOffset    = "--offset"
	optLimit     = "--limit"
	optBlank     = "--blank"
	optHelp      = "--help"
	optVersion   = "--version"

	shortOutput = "-o"
	shortHelp   = "-h"

	endOfOptions = "--"
	stdinPath    = "-"

	// kindValue takes one value, attached with = or as the next argument.
	kindValue optionKind = 0
	// kindFlag takes no value.
	kindFlag optionKind = 1
	// kindAction takes no value and requests help or version output instead of an operation.
	kindAction optionKind = 2
	// kindDirective is --blank: an ordered operand, not an option that may be given once.
	kindDirective optionKind = 3
)

// appliesTo reports whether the option is valid for the command context.
func (s *optionSpec) appliesTo(context Name) bool {
	return slices.Contains(s.commands, context)
}

// label is the option as help shows it: "-o, --output FILE".
func (s *optionSpec) label() string {
	return optionLabel(s.name, s.short, s.value)
}

// optionLabel joins an option's spellings and value placeholder.
func optionLabel(name, short, value string) string {
	text := name
	if short != "" {
		text = short + ", " + name
	}

	if value != "" {
		text += " " + value
	}

	return text
}

// commandNames lists the commands in the order help shows them.
func commandNames() []Name {
	return []Name{NameBuild, NameCheck, NameReport, NameSchema, NameVersion, NameHelp}
}

// optionSpecs returns every option in help order.
func optionSpecs() []optionSpec {
	return slices.Concat(planningOptions(), reportOptions(), commonOptions())
}

// planningOptions are the options of build and check.
func planningOptions() []optionSpec {
	planning := []Name{NameBuild, NameCheck}

	return []optionSpec{
		{
			name: optPlan, value: "FILE|-", commands: planning, allowStdin: true, apply: (*parser).setPlan,
			summary: "Read the plan from FILE, or from standard input with -.",
		},
		{
			name: optPlanJSON, value: "JSON", commands: planning, allowEmpty: true, apply: (*parser).setPlanJSON,
			summary: "Take the whole plan as one inline JSON argument (small jobs).",
		},
		{
			name: optBlank, kind: kindDirective, commands: planning,
			summary: "Direct operands only: insert one generated page here. Repeat for several.",
		},
		{
			name: optBaseDir, value: "DIR", commands: planning,
			summary: "Base for relative paths of a plan from - or --plan-json (default: working directory).",
			apply:   func(p *parser, _ int, value string) error { p.cmd.BaseDir = value; return nil },
		},
		{
			name: optOutput, short: shortOutput, value: "FILE", commands: planning,
			summary: "Output PDF, relative to the working directory; overrides the plan's output.",
			apply:   func(p *parser, _ int, value string) error { p.cmd.Output = value; return nil },
		},
		{
			name: optOverwrite, kind: kindFlag, commands: planning,
			summary: "Replace an existing output or report file.",
			apply:   func(p *parser, _ int, _ string) error { p.cmd.Overwrite = true; return nil },
		},
		{
			name: optReport, value: "FILE", commands: planning,
			summary: "Save the complete result for later queries with pdfconcat report.",
			apply:   func(p *parser, _ int, value string) error { p.cmd.ReportPath = value; return nil },
		},
		{
			name: optJobs, value: "N", commands: planning, apply: (*parser).setJobs,
			summary: "Concurrent source inspections, at least 1 (default: min(4, CPUs)).",
		},
		{
			name: optDetails, kind: kindFlag, commands: planning,
			summary: "Print the complete result instead of the bounded summary.",
			apply:   func(p *parser, _ int, _ string) error { p.cmd.Details = true; return nil },
		},
	}
}

// reportOptions are the options of the report command.
func reportOptions() []optionSpec {
	reporting := []Name{NameReport}

	return []optionSpec{
		{
			name: optPart, value: "ID", commands: reporting, apply: (*parser).setPart,
			summary: "Show one contribution by id, such as /items/42 or argv:3.",
		},
		{
			name: optPage, value: "N", commands: reporting, apply: (*parser).setPage,
			summary: "Show the contribution covering output page N (1-based).",
		},
		{
			name: optView, value: "parts|diagnostics", commands: reporting, apply: (*parser).setView,
			summary: "List contributions or diagnostics, paged.",
		},
		{
			name: optOffset, value: "N", commands: reporting, apply: (*parser).setOffset,
			summary: "First record of a --view (default 0).",
		},
		{
			name: optLimit, value: "N", commands: reporting, apply: (*parser).setLimit,
			summary: "Records per --view page, 1 to 100 (default 20).",
		},
		{
			name: optDetails, kind: kindFlag, commands: reporting,
			summary: "Expand the selected records to their full resolved values.",
			apply:   func(p *parser, _ int, _ string) error { p.cmd.Details = true; return nil },
		},
	}
}

// commonOptions are the options of several or all commands.
func commonOptions() []optionSpec {
	everywhere := slices.Concat([]Name{rootContext}, commandNames())
	rendering := []Name{rootContext, NameBuild, NameCheck, NameReport, NameVersion, NameHelp}

	return []optionSpec{
		{
			name: optFormat, value: "json|text", commands: rendering, apply: (*parser).setFormat,
			summary: "Render standard output as compact JSON (default) or human-readable text.",
		},
		{
			name: optVersion, kind: kindAction, commands: []Name{rootContext}, apply: (*parser).setVersion,
			summary: "Print version information (same as the version command).",
		},
		{
			name: optHelp, short: shortHelp, kind: kindAction, commands: everywhere, apply: (*parser).setHelp,
			summary: "Show help for the command.",
		},
	}
}
