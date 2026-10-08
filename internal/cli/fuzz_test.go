// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// planShape says which members of a build or check Command a plan source may set.
	planShape struct {
		file, inline, operands, baseDir bool
	}

	// invariant returns a description of how an accepted Command breaks a grammar rule, or "" when it holds.
	invariant func(args []string, command *cli.Command) string
)

// OS arguments cannot contain NUL. A leading separator distinguishes no arguments from one empty argument.
const argSeparator = "\x00"

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	for _, arg := range args {
		if strings.Contains(arg, argSeparator) {
			panic("fuzz argv seed contains NUL, which cannot occur in an OS argument")
		}
	}

	return argSeparator + strings.Join(args, argSeparator)
}

func splitArgs(joined string) []string {
	if joined == "" {
		return nil
	}

	return strings.Split(strings.TrimPrefix(joined, argSeparator), argSeparator)
}

// FuzzParse checks that Parse never panics, is deterministic, and that whatever it accepts or rejects satisfies
// the grammar's invariants.
func FuzzParse(f *testing.F) {
	accepts, rejects := acceptedLines(), rejectedLines()

	for i := range accepts {
		f.Add(joinArgs(accepts[i].args))
	}

	for i := range rejects {
		f.Add(joinArgs(rejects[i].args))
	}

	f.Add(joinArgs([]string{argBuild, "-o", string([]byte{0xff})}))
	f.Add(joinArgs([]string{argBuild, "--", "--", argBlank}))
	f.Add(joinArgs([]string{argReport, "r", "--limit=" + strings.Repeat("9", 30)}))
	f.Add(joinArgs([]string{argBuild, "--plan=-", argFormatText, argFormatText}))
	f.Add(joinArgs([]string{argBuild, "-o", "control\x1f.pdf", argBlank}))

	f.Fuzz(func(t *testing.T, joined string) {
		args := splitArgs(joined)

		first, firstErr := cli.Parse(args)
		second, secondErr := cli.Parse(args)

		if !reflect.DeepEqual(first, second) || errorText(firstErr) != errorText(secondErr) {
			t.Fatalf("Parse(%q) is not deterministic", args)
		}

		if firstErr != nil {
			checkRejection(t, args, &first, firstErr)

			return
		}

		checkAcceptedEncoding(t, args)

		for _, check := range invariants() {
			if problem := check(args, &first); problem != "" {
				t.Fatalf("Parse(%q) = %+v: %s", args, first, problem)
			}
		}
	})
}

func errorText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func checkRejection(t *testing.T, args []string, command *cli.Command, err error) {
	t.Helper()

	usage, ok := errors.AsType[*cli.UsageError](err)
	if !ok || len(usage.Diagnostics) != 1 || !reflect.DeepEqual(*command, cli.Command{}) {
		t.Fatalf("Parse(%q) = %+v, %v: want the zero Command and one *UsageError diagnostic", args, command, err)
	}

	diagnostic := usage.Diagnostics[0]
	if diagnostic.Stage != report.StageUsage || diagnostic.Message == "" || !slices.Contains(cli.UsageCodes(), diagnostic.Code) {
		t.Fatalf("Parse(%q) diagnostic %+v is not a documented usage diagnostic", args, diagnostic)
	}

	if !locatedInside(diagnostic.Location, len(args)) {
		t.Fatalf("Parse(%q) location %+v is outside the arguments", args, diagnostic.Location)
	}

	if usage.Format == cli.FormatText && !mentionsTextFormat(args) {
		t.Fatalf("Parse(%q) renders its failure as text without a --format text", args)
	}
}

// locatedInside reports whether a location is absent or names an argument of count arguments.
func locatedInside(location *report.Location, count int) bool {
	if location == nil {
		return true
	}

	return location.ArgvIndex != nil && *location.ArgvIndex >= 0 && *location.ArgvIndex < count
}

// mentionsTextFormat reports whether the arguments contain a --format text in either spelling.
func mentionsTextFormat(args []string) bool {
	for i, arg := range args {
		if arg == argFormatText || (arg == argFormat && i+1 < len(args) && args[i+1] == "text") {
			return true
		}
	}

	return false
}

// invariants are the rules every accepted Command satisfies.
func invariants() []invariant {
	return []invariant{
		formatInvariant,
		commandInvariant,
		planSourceInvariant,
		operandInvariant,
		reportInvariant,
		strayInvariant,
		jobsInvariant,
	}
}

func formatInvariant(args []string, command *cli.Command) string {
	if command.Format == cli.FormatText && !mentionsTextFormat(args) {
		return "text selected without a --format text"
	}

	return ""
}

func commandInvariant(_ []string, command *cli.Command) string {
	known := slices.Contains(
		[]cli.Name{cli.NameBuild, cli.NameCheck, cli.NameReport, cli.NameSchema, cli.NameVersion, cli.NameHelp},
		command.Name,
	)
	schemaKnown := slices.Contains([]string{cli.SchemaPlan, cli.SchemaReport, cli.SchemaResponse}, command.SchemaName)
	topic := command.HelpFor == "" || cli.Help(command.HelpFor).Command == command.HelpFor

	switch {
	case !known:
		return "unknown command"
	case !topic || (command.Name != cli.NameHelp && command.HelpFor != ""):
		return "help topic is invalid or set for another command"
	case command.Name == cli.NameSchema && !schemaKnown:
		return "schema name is invalid"
	default:
		return ""
	}
}

// planShapes says which members each plan source may set; PlanNone is the absence of instructions.
func planShapes() map[cli.PlanSource]planShape {
	return map[cli.PlanSource]planShape{
		cli.PlanNone:     {},
		cli.PlanFile:     {file: true},
		cli.PlanStdin:    {baseDir: true},
		cli.PlanInline:   {inline: true, baseDir: true},
		cli.PlanOperands: {operands: true},
	}
}

func planSourceInvariant(_ []string, command *cli.Command) string {
	planning := command.Name == cli.NameBuild || command.Name == cli.NameCheck
	if planning != (command.PlanSource != cli.PlanNone) {
		return "build and check have exactly one plan source, other commands none"
	}

	shape := planShapes()[command.PlanSource]

	switch {
	case command.PlanPath != "" && !shape.file, command.PlanJSON != "" && !shape.inline:
		return "a plan source carries another source's value"
	case len(command.Operands) > 0 && !shape.operands, command.BaseDir != "" && !shape.baseDir:
		return "operands or base directory with a source that does not take them"
	default:
		return requiredPlanValue(command)
	}
}

// requiredPlanValue checks that a named plan has its path and a direct sequence its operands.
func requiredPlanValue(command *cli.Command) string {
	namedWithoutPath := command.PlanSource == cli.PlanFile && command.PlanPath == ""
	directWithoutOperands := command.PlanSource == cli.PlanOperands && len(command.Operands) == 0

	if namedWithoutPath || directWithoutOperands {
		return "a plan source lacks its value"
	}

	return ""
}

func operandInvariant(args []string, command *cli.Command) string {
	previous := -1

	for _, operand := range command.Operands {
		switch {
		case operand.Position <= previous || operand.Position >= len(args):
			return "operand positions are not strictly increasing inside the arguments"
		case operand.Blank && (operand.Path != "" || args[operand.Position] != argBlank):
			return "a blank operand is not the --blank argument"
		case !operand.Blank && (operand.Path == "" || operand.Path != args[operand.Position]):
			return "a path operand is not its argument"
		default:
			previous = operand.Position
		}
	}

	return ""
}

// reportInvariant is the report command's selection and paging rules.
func reportInvariant(_ []string, command *cli.Command) string {
	selections := 0

	for _, selected := range []bool{command.Part != "", command.HasPage, command.View != ""} {
		if selected {
			selections++
		}
	}

	switch {
	case command.Name == cli.NameReport && command.ReportFile == "", selections > 1, command.HasPage && command.Page < 1:
		return "report selection rules are violated"
	case command.View != "" && command.View != report.ViewParts && command.View != report.ViewDiagnostics:
		return "unknown view"
	default:
		return pagingProblem(command)
	}
}

func pagingProblem(command *cli.Command) string {
	switch {
	case (command.HasOffset || command.HasLimit) && command.View == "":
		return "paging without a view"
	case command.Offset < 0, command.Limit < 0, !command.HasOffset && command.Offset != 0, !command.HasLimit && command.Limit != 0:
		return "inconsistent paging"
	default:
		return ""
	}
}

// strayInvariant requires the members of other commands to stay zero.
func strayInvariant(_ []string, command *cli.Command) string {
	planning := command.Name == cli.NameBuild || command.Name == cli.NameCheck
	reporting := command.Name == cli.NameReport

	switch {
	case !planning && planningMembersSet(command):
		return "build options on another command"
	case !reporting && reportMembersSet(command):
		return "report options on another command"
	case !planning && !reporting && command.Details:
		return "details outside build, check, and report"
	default:
		return ""
	}
}

func planningMembersSet(command *cli.Command) bool {
	return command.Output != "" || command.ReportPath != "" || command.Overwrite || command.Jobs != 0
}

// jobsInvariant requires an explicit job count to be positive.
func jobsInvariant(_ []string, command *cli.Command) string {
	if command.Jobs < 0 {
		return "negative job count"
	}

	return ""
}

func reportMembersSet(command *cli.Command) bool {
	selected := command.ReportFile != "" || command.Part != "" || command.HasPage || command.View != ""

	return selected || command.HasOffset || command.HasLimit
}

func checkAcceptedEncoding(t *testing.T, args []string) {
	t.Helper()

	for _, arg := range args {
		if !utf8.ValidString(arg) {
			t.Fatal("invalid UTF8 argv was accepted")
		}
	}
}
