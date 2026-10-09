// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"errors"
	"math"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
)

func (p *parser) setFitTo(index int, value string) error {
	target, err := assembly.ParseFitTarget(value)
	if err != nil {
		return p.fail(CodeInvalidValue, index, "--fit-to needs A4 or Legal, not %q", value)
	}

	p.cmd.FitTo = target

	return nil
}

func (p *parser) setPlan(index int, value string) error {
	source := PlanFile
	if value == stdinPath {
		source = PlanStdin
	}

	err := p.claimSource(index, source)
	if err != nil {
		return err
	}

	if source == PlanFile {
		p.cmd.PlanPath = value
	}

	return nil
}

func (p *parser) setPlanJSON(index int, value string) error {
	err := p.claimSource(index, PlanInline)
	if err != nil {
		return err
	}

	p.cmd.PlanJSON = value

	return nil
}

func (p *parser) setFormat(index int, value string) error {
	switch value {
	case "json":
		p.cmd.Format = FormatJSON
	case "text":
		p.cmd.Format = FormatText
	default:
		return p.fail(CodeInvalidValue, index, "--format needs json or text, not %q", value)
	}

	return nil
}

func (p *parser) setHelp(index int, _ string) error {
	if p.version {
		return p.fail(CodeConflictingActions, index, "--help and --version cannot be combined")
	}

	p.help = true

	return nil
}

func (p *parser) setVersion(index int, _ string) error {
	if p.help {
		return p.fail(CodeConflictingActions, index, "--help and --version cannot be combined")
	}

	p.version = true

	return nil
}

func (p *parser) setJobs(index int, value string) error {
	number, err := report.ParseNumber(optJobs, value)
	if err != nil || number < 1 || number > math.MaxInt {
		return p.fail(CodeInvalidJobs, index, "--jobs needs a whole number of at least 1, not %q", value)
	}

	p.cmd.Jobs = int(number)

	return nil
}

func (p *parser) setPart(_ int, value string) error {
	p.cmd.Part = value
	return nil
}

func (p *parser) setView(_ int, value string) error {
	p.cmd.View = value
	return nil
}

func (p *parser) setPage(index int, value string) error {
	page, err := p.number(index, optPage, value)
	if err != nil {
		return err
	}

	p.cmd.Page, p.cmd.HasPage = page, true

	return nil
}

func (p *parser) setOffset(index int, value string) error {
	number, err := p.pagingNumber(index, optOffset, value)
	if err != nil {
		return err
	}

	p.cmd.Offset, p.cmd.HasOffset = number, true

	return nil
}

func (p *parser) setLimit(index int, value string) error {
	number, err := p.pagingNumber(index, optLimit, value)
	if err != nil {
		return err
	}

	p.cmd.Limit, p.cmd.HasLimit = number, true

	return nil
}

// pagingNumber parses a numeric token; semantic ranges belong to report.Request.Validate.
func (p *parser) pagingNumber(index int, option, value string) (int64, error) {
	return p.number(index, option, value)
}

// number parses a decimal whole number that fits 64 bits, or fails naming the option at index.
func (p *parser) number(index int, option, value string) (int64, error) {
	number, err := report.ParseNumber(option, value)
	if err != nil {
		return 0, p.fail(report.CodeInvalidNumber, index, "%s needs a whole number that fits 64 bits, not %q", option, value)
	}

	return number, nil
}

// finishers returns the requirements each command context checks once the whole line has been read.
func finishers() map[Name]func(*parser) error {
	return map[Name]func(*parser) error{
		NameBuild:   (*parser).finishPlanning,
		NameCheck:   (*parser).finishPlanning,
		NameReport:  (*parser).finishReport,
		NameSchema:  (*parser).finishSchema,
		NameHelp:    func(*parser) error { return nil },
		NameVersion: func(*parser) error { return nil },
		rootContext: (*parser).finishRoot,
	}
}

// finish applies the requirements that need the whole line and produces the result. A help request skips them.
func (p *parser) finish() error {
	if !p.help {
		return finishers()[p.context](p)
	}

	if p.context == NameReport {
		err := p.validateReportRequest()

		if found, ok := errors.AsType[*UsageError](err); ok {
			code := found.Diagnostics[0].Code
			if code != report.CodeDetailsNeedSelect && code != report.CodePagingNeedsView {
				return err
			}
		}
	}

	p.cmd = Command{Name: NameHelp, Format: p.cmd.Format, HelpFor: p.context}

	return nil
}

func (p *parser) finishRoot() error {
	if !p.version {
		return p.missingCommand()
	}

	p.cmd = Command{Name: NameVersion, Format: p.cmd.Format}

	return nil
}

func (p *parser) finishSchema() error {
	if p.cmd.SchemaName == "" {
		return p.fail(
			CodeMissingOperand,
			-1,
			"schema needs a name: pdfconcat schema %s, %s or %s",
			SchemaPlan,
			SchemaReport,
			SchemaResponse,
		)
	}

	return nil
}

func (p *parser) finishPlanning() error {
	if p.cmd.PlanSource == PlanNone {
		return p.fail(CodeMissingPlanSource, -1,
			"%s needs instructions: --plan FILE|-, --plan-json JSON, or PDF operands (with --blank for generated pages)", p.context)
	}

	index, given := p.seen[optBaseDir]
	if !given {
		return nil
	}

	if p.cmd.PlanSource == PlanFile {
		return p.fail(CodeBaseDirConflict, index,
			"--base-dir cannot be used with a named plan file: its relative paths resolve against the plan file's directory")
	}

	if p.cmd.PlanSource == PlanOperands {
		return p.fail(CodeBaseDirConflict, index,
			"--base-dir applies only to a plan from standard input or --plan-json; direct operands resolve against the working directory")
	}

	return nil
}

func (p *parser) finishReport() error {
	if p.cmd.ReportFile == "" {
		return p.fail(CodeMissingOperand, -1, "report needs the saved report: pdfconcat report FILE")
	}

	return p.validateReportRequest()
}

func (p *parser) validateReportRequest() error {
	err := p.cmd.ReportRequest().Validate()
	if err == nil {
		return nil
	}

	found, _ := report.AsError(err)
	index := -1

	if found.Diagnostic.Code == report.CodeSelectionConflict {
		for _, option := range []string{optPart, optPage, optView} {
			if at, given := p.seen[option]; given && at > index {
				index = at
			}
		}
	} else if at, given := p.seen[found.Option]; given {
		index = at
	}

	return p.fail(found.Diagnostic.Code, index, "%s", found.Diagnostic.Message)
}
