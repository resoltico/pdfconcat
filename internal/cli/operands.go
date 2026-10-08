// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import (
	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
)

// operandHandlers returns what each command context does with an operand: a path of a direct sequence, the
// saved report, a schema name, a help topic, or nothing.
func operandHandlers() map[Name]func(*parser, int, string) error {
	return map[Name]func(*parser, int, string) error{
		NameBuild:   (*parser).pathOperand,
		NameCheck:   (*parser).pathOperand,
		NameReport:  (*parser).reportFile,
		NameSchema:  (*parser).schemaName,
		NameHelp:    (*parser).helpTarget,
		NameVersion: (*parser).versionOperand,
		rootContext: (*parser).rootOperand,
	}
}

// operand handles an argument that is not an option, or any argument after "--".
func (p *parser) operand(index int, arg string) error {
	if arg == stdinPath && !p.endOptions {
		return p.fail(CodeStdinOperand, index,
			`"-" is not an operand: standard input is only --plan -; write ./- for a file named -`)
	}

	if arg == "" {
		return p.fail(CodeEmptyValue, index, "an operand must not be empty")
	}

	return operandHandlers()[p.context](p, index, arg)
}

func (p *parser) rootOperand(index int, arg string) error {
	return p.fail(CodeCommandNotFirst, index, "%q comes before a command; the command goes first: pdfconcat COMMAND [options]", arg)
}

func (p *parser) versionOperand(index int, arg string) error {
	return p.fail(CodeUnexpectedOperand, index, "version takes no operands, not %q", arg)
}

// pathOperand adds a PDF path to the direct sequence.
func (p *parser) pathOperand(index int, arg string) error {
	return p.addOperand(index, assembly.Operand{Path: arg, Position: index})
}

// blank handles the --blank directive, which is an ordered operand of a direct sequence.
func (p *parser) blank(index int, arg argument) error {
	if arg.attached {
		return p.fail(CodeUnexpectedValue, index,
			"--blank takes no value; to give generated pages text or a style use a plan (pdfconcat schema plan)")
	}

	return p.addOperand(index, assembly.Operand{Position: index, Blank: true})
}

func (p *parser) addOperand(index int, operand assembly.Operand) error {
	err := p.claimSource(index, PlanOperands)
	if err != nil {
		return err
	}

	p.cmd.Operands = append(p.cmd.Operands, operand)

	return nil
}

func (p *parser) reportFile(index int, arg string) error {
	if p.cmd.ReportFile != "" {
		return p.fail(CodeUnexpectedOperand, index, "report takes one FILE; %q is an extra operand", arg)
	}

	p.cmd.ReportFile = arg

	return nil
}

func (p *parser) schemaName(index int, arg string) error {
	if p.cmd.SchemaName != "" {
		return p.fail(CodeUnexpectedOperand, index, "schema takes one name; %q is an extra operand", arg)
	}

	if arg != SchemaPlan && arg != SchemaReport && arg != SchemaResponse {
		return p.fail(CodeUnknownSchema, index, "unknown schema %q; use %s, %s or %s", arg, SchemaPlan, SchemaReport, SchemaResponse)
	}

	p.cmd.SchemaName = arg

	return nil
}

func (p *parser) helpTarget(index int, arg string) error {
	if p.cmd.HelpFor != rootContext {
		return p.fail(CodeUnexpectedOperand, index, "help takes one command; %q is an extra operand", arg)
	}

	name, found := commandNamed(arg)
	if !found {
		return p.fail(CodeUnknownCommand, index, "unknown command %q; commands: %s", arg, joinNames(commandNames()))
	}

	p.cmd.HelpFor = name

	return nil
}

// claimSource fixes where the instructions come from. The three sources are mutually exclusive.
func (p *parser) claimSource(index int, source PlanSource) error {
	current := p.cmd.PlanSource

	if current == PlanNone {
		p.cmd.PlanSource, p.sourceAt = source, index

		return nil
	}

	if current == source {
		return nil
	}

	return p.fail(CodePlanSourceConflict, index,
		"%s cannot be combined with %s (argv:%d); give exactly one of --plan, --plan-json, or direct operands",
		sourceLabel(source), sourceLabel(current), p.sourceAt)
}

func sourceLabel(source PlanSource) string {
	if source == PlanInline {
		return optPlanJSON
	}

	if source == PlanOperands {
		return "direct operands"
	}

	return optPlan
}

// claimSelection enforces that --part, --page, and --view are mutually exclusive.
func (p *parser) claimSelection(index int, option string) error {
	if p.selected != "" {
		return p.fail(report.CodeSelectionConflict, index,
			"%s cannot be combined with %s (argv:%d); choose one of --part, --page, or --view", option, p.selected, p.selectedAt)
	}

	p.selected, p.selectedAt = option, index

	return nil
}
