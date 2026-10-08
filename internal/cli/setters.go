// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import (
	"math"

	"github.com/resoltico/pdfconcat/internal/report"
)

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

func (p *parser) setPart(index int, value string) error {
	err := p.claimSelection(index, optPart)
	if err != nil {
		return err
	}

	p.cmd.Part = value

	return nil
}

func (p *parser) setView(index int, value string) error {
	err := p.claimSelection(index, optView)
	if err != nil {
		return err
	}

	if value != report.ViewParts && value != report.ViewDiagnostics {
		return p.fail(report.CodeUnknownView, index, "--view needs %s or %s, not %q", report.ViewParts, report.ViewDiagnostics, value)
	}

	p.cmd.View = value

	return nil
}

func (p *parser) setPage(index int, value string) error {
	err := p.claimSelection(index, optPage)
	if err != nil {
		return err
	}

	page, err := p.number(index, optPage, value)
	if err != nil {
		return err
	}

	if page < 1 {
		return p.fail(report.CodeInvalidNumber, index, "--page needs a whole number of at least 1 (pages are 1-based), not %q", value)
	}

	p.cmd.Page = page

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

// pagingNumber parses a non-negative paging value. Range limits such as a maximum page size belong to the
// report package; the command line rejects only what is not a whole non-negative 64-bit number.
func (p *parser) pagingNumber(index int, option, value string) (int64, error) {
	number, err := p.number(index, option, value)
	if err != nil {
		return 0, err
	}

	if number < 0 {
		return 0, p.fail(report.CodeInvalidPaging, index, "%s must not be negative, not %q", option, value)
	}

	return number, nil
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
	if p.help {
		p.cmd = Command{Name: NameHelp, Format: p.cmd.Format, HelpFor: p.context}

		return nil
	}

	return finishers()[p.context](p)
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

	if p.cmd.Details && p.cmd.View == "" && p.cmd.Part == "" && p.cmd.Page == 0 {
		return p.fail(report.CodeDetailsNeedSelect, p.seen[optDetails],
			"--details expands the records you select; add --part ID, --page N, or --view parts|diagnostics")
	}

	if p.cmd.View != "" {
		return nil
	}

	for _, option := range []string{optOffset, optLimit} {
		index, given := p.seen[option]
		if given {
			return p.fail(report.CodePagingNeedsView, index, "%s pages a --view; add --view parts or --view diagnostics", option)
		}
	}

	return nil
}
