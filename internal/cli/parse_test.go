// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// accepted is a command line and the Command it must produce.
	accepted struct {
		want *cli.Command
		name string
		args []string
	}

	// rejected is a command line and the single usage diagnostic it must produce.
	rejected struct {
		name string
		// mention is a substring the message must contain: the option and what it expects.
		mention string
		code    report.Code
		args    []string
		// index is the argv position of the fault; negative means the diagnostic has no location.
		index int
		// text is whether the failure must render as text.
		text bool
	}
)

const (
	planOption = "--plan"
	jobFile    = "job.json"
)

// argv splits a command line on spaces. Arguments that contain spaces or are empty are written as slices.
func argv(line string) []string {
	return strings.Fields(line)
}

func pdf(position int, path string) assembly.Operand {
	return assembly.Operand{Path: path, Position: position}
}

func blank(position int) assembly.Operand {
	return assembly.Operand{Position: position, Blank: true}
}

func accept(name, line string, want *cli.Command) accepted {
	return accepted{name: name, args: argv(line), want: want}
}

func acceptedLines() []accepted {
	return slices.Concat(acceptedPlanning(), acceptedOperands(), acceptedReport(), acceptedRootCommands(), acceptedHelp())
}

func acceptedPlanning() []accepted {
	return []accepted{
		accept("plan file", "build --plan job.json", new(cli.Command{Name: cli.NameBuild, PlanSource: cli.PlanFile, PlanPath: jobFile})),
		accept("plan file attached", "check --plan=--odd.json",
			new(cli.Command{Name: cli.NameCheck, PlanSource: cli.PlanFile, PlanPath: "--odd.json"})),
		accept("plan from stdin with base dir and output", "build --plan - --base-dir /project -o out.pdf",
			new(cli.Command{Name: cli.NameBuild, PlanSource: cli.PlanStdin, BaseDir: "/project", Output: outputFile})),
		accept("stdin sentinel attached, base dir first", "build --base-dir=/p --plan=-",
			new(cli.Command{Name: cli.NameBuild, PlanSource: cli.PlanStdin, BaseDir: "/p"})),
		{
			name: "inline plan", args: []string{argBuild, argPlanJSON, `{"version":1}`, "--base-dir=/p", "--output=o.pdf"},
			want: new(cli.Command{
				Name: cli.NameBuild, PlanSource: cli.PlanInline, PlanJSON: `{"version":1}`, BaseDir: "/p", Output: "o.pdf",
			}),
		},
		accept("empty inline plan is the decoder's to reject", "check --plan-json=",
			new(cli.Command{Name: cli.NameCheck, PlanSource: cli.PlanInline})),
		accept("every build option", "build --plan p.json --output=o.pdf --overwrite --report r.json --details --jobs 3 --format text",
			new(cli.Command{
				Name: cli.NameBuild, PlanSource: cli.PlanFile, PlanPath: "p.json", Output: "o.pdf", Overwrite: true,
				ReportPath: reportFile, Details: true, Jobs: 3, Format: cli.FormatText,
			})),
	}
}

func acceptedOperands() []accepted {
	return []accepted{
		accept("operands with blank", "build -o out.pdf a.pdf --blank b.pdf", new(cli.Command{
			Name: cli.NameBuild, PlanSource: cli.PlanOperands, Output: outputFile,
			Operands: []assembly.Operand{pdf(3, sourceAPath), blank(4), pdf(5, "b.pdf")},
		})),
		accept("repeated blank", "check --blank --blank a.pdf", new(cli.Command{
			Name: cli.NameCheck, PlanSource: cli.PlanOperands, Operands: []assembly.Operand{blank(1), blank(2), pdf(3, sourceAPath)},
		})),
		accept("operands may precede options", "build a.pdf -o out.pdf", new(cli.Command{
			Name: cli.NameBuild, PlanSource: cli.PlanOperands, Output: outputFile, Operands: []assembly.Operand{pdf(1, sourceAPath)},
		})),
		accept("end of options makes every argument a path", "build -o out.pdf -- --blank ./--blank - -- -o", new(cli.Command{
			Name: cli.NameBuild, PlanSource: cli.PlanOperands, Output: outputFile,
			Operands: []assembly.Operand{pdf(4, argBlank), pdf(5, "./--blank"), pdf(6, "-"), pdf(7, "--"), pdf(8, "-o")},
		})),
		accept("after the end of options a help flag is a path", "build -- --help", new(cli.Command{
			Name: cli.NameBuild, PlanSource: cli.PlanOperands, Operands: []assembly.Operand{pdf(2, argHelp)},
		})),
		accept("check with a destination", "check -o out.pdf --overwrite a.pdf", new(cli.Command{
			Name: cli.NameCheck, PlanSource: cli.PlanOperands, Output: outputFile, Overwrite: true,
			Operands: []assembly.Operand{pdf(4, sourceAPath)},
		})),
	}
}

func acceptedReport() []accepted {
	return []accepted{
		accept("report summary", "report r.json", new(cli.Command{Name: cli.NameReport, ReportFile: reportFile})),
		accept("report file after options", "report --details --part=/items/1 --format=text r.json",
			new(cli.Command{Name: cli.NameReport, ReportFile: reportFile, Details: true, Part: "/items/1", Format: cli.FormatText})),
		accept("report literal file", "report -- --r.json", new(cli.Command{Name: cli.NameReport, ReportFile: "--r.json"})),
		accept("report part", "report r.json --part /items/42",
			new(cli.Command{Name: cli.NameReport, ReportFile: reportFile, Part: "/items/42"})),
		accept("report page", "report r.json --page=5001", new(cli.Command{Name: cli.NameReport, ReportFile: reportFile, Page: 5001})),
		accept("report view with paging", "report r.json --view diagnostics --offset 0 --limit 20", new(cli.Command{
			Name: cli.NameReport, ReportFile: reportFile, View: "diagnostics", HasOffset: true, HasLimit: true, Limit: 20,
		})),
		accept("a page size outside 1 to 100 is the report package's to judge", "report r.json --view=parts --limit=0",
			new(cli.Command{Name: cli.NameReport, ReportFile: reportFile, View: "parts", HasLimit: true})),
	}
}

func acceptedRootCommands() []accepted {
	return []accepted{
		accept("schema plan", "schema plan", new(cli.Command{Name: cli.NameSchema, SchemaName: "plan"})),
		accept("schema report", "schema report", new(cli.Command{Name: cli.NameSchema, SchemaName: argReport})),
		accept("version command", "version", new(cli.Command{Name: cli.NameVersion})),
		accept("version command in text", "version --format text", new(cli.Command{Name: cli.NameVersion, Format: cli.FormatText})),
		accept("explicit json format", "version --format=json", new(cli.Command{Name: cli.NameVersion})),
		accept("version flag", argVersion, new(cli.Command{Name: cli.NameVersion})),
		accept("version flag after format", "--format=text --version", new(cli.Command{Name: cli.NameVersion, Format: cli.FormatText})),
	}
}

func acceptedHelp() []accepted {
	return []accepted{
		accept("root help", argHelp, new(cli.Command{Name: cli.NameHelp})),
		accept("root help short", "-h", new(cli.Command{Name: cli.NameHelp})),
		accept("help command", argHelpCommand, new(cli.Command{Name: cli.NameHelp})),
		accept("help for a command", "help build", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameBuild})),
		{
			name: "help for its own command", args: []string{argHelpCommand, argHelpCommand},
			want: new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameHelp}),
		},
		accept("help flag on a command", "report --help", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameReport})),
		accept("help flag after a valid plan", "build --plan a.json -h", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameBuild})),
		accept("help flag needs no plan source", "check --help", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameCheck})),
		accept("help does not enforce what a build would", "build --base-dir=d a.pdf --help",
			new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameBuild})),
		accept("help in text", "build --help --format text",
			new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameBuild, Format: cli.FormatText})),
		accept("help of version", "version --help", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameVersion})),
		accept("help of schema", "schema -h", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameSchema})),
		accept("help flag on help", "help build --help", new(cli.Command{Name: cli.NameHelp, HelpFor: cli.NameHelp})),
	}
}

func TestParseAccepts(t *testing.T) {
	t.Parallel()

	for _, test := range acceptedLines() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := cli.Parse(test.args)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", test.args, err)
			}

			if !reflect.DeepEqual(got, *test.want) {
				t.Errorf("Parse(%q)\n got  %+v\n want %+v", test.args, got, test.want)
			}
		})
	}
}

// reject is a rejection of a command line given as a space-separated string.
func reject(name, line string, code report.Code, index int, mention string) rejected {
	return rejected{name: name, args: argv(line), code: code, index: index, mention: mention}
}

// rejectText is a rejection that must render as text.
func rejectText(name, line string, code report.Code, index int) rejected {
	return rejected{name: name, args: argv(line), code: code, index: index, text: true}
}

func rejectedLines() []rejected {
	return slices.Concat(
		commandRejections(), optionRejections(), applicabilityRejections(), valueRejections(), planRejections(), reportRejections(),
		faultPrecedenceRejections(),
	)
}

func commandRejections() []rejected {
	return []rejected{
		{name: "no arguments", code: cli.CodeMissingCommand, index: -1, mention: "commands:"},
		rejectText("only format", "--format text", cli.CodeMissingCommand, -1),
		reject("unknown command", "bogus", cli.CodeUnknownCommand, 0, `"bogus"`),
		{name: "empty command", args: []string{""}, code: cli.CodeUnknownCommand, index: 0},
		reject("unknown help topic", "help bogus", cli.CodeUnknownCommand, 1, ""),
		{
			name: "operand before a command", args: argv("--format text build"), code: cli.CodeCommandNotFirst, index: 2,
			mention: "command goes first", text: true,
		},
		reject("end of options before a command", "-- build", cli.CodeCommandNotFirst, 1, ""),
		reject("help and version", "--help --version", cli.CodeConflictingActions, 1, argVersion),
		reject("version and help", "--version -h", cli.CodeConflictingActions, 1, ""),
		reject("version takes no operand", "version x", cli.CodeUnexpectedOperand, 1, ""),
		reject("help takes one command", "help build check", cli.CodeUnexpectedOperand, 2, ""),
		reject("schema needs a name", "schema", cli.CodeMissingOperand, -1, "plan"),
		reject("schema name is exact", "schema Plan", cli.CodeUnknownSchema, 1, "plan, report or response"),
		reject("schema takes one name", "schema plan report", cli.CodeUnexpectedOperand, 2, ""),
		reject("report needs a file", argReport, cli.CodeMissingOperand, -1, "FILE"),
		reject("report takes one file", "report a b", cli.CodeUnexpectedOperand, 2, ""),
		{name: "empty operand", args: []string{argBuild, ""}, code: cli.CodeEmptyValue, index: 1},
		reject("stdin is not a path", "build -", cli.CodeStdinOperand, 1, "./-"),
		reject("stdin is not a report", "report -", cli.CodeStdinOperand, 1, ""),
	}
}

func optionRejections() []rejected {
	return []rejected{
		reject("unknown option", unknownOptionLine, cli.CodeUnknownOption, 1, `"--bogus"`),
		reject("unknown option names what applies", unknownOptionLine, cli.CodeUnknownOption, 1, "consult command help"),
		reject("unknown short option", "build -x", cli.CodeUnknownOption, 1, ""),
		reject("short option cannot attach", "build -o=out.pdf a.pdf", cli.CodeUnknownOption, 1, ""),
		reject("short option cannot be glued", "build -oout.pdf", cli.CodeUnknownOption, 1, ""),
		reject("attached value keeps only the name", "build --bogus=secret", cli.CodeUnknownOption, 1, `"--bogus"`),
		reject("removed dry run", "build --dry-run a.pdf", cli.CodeUnknownOption, 1, ""),
		reject("removed json", "build --json a.pdf", cli.CodeUnknownOption, 1, ""),
		reject("removed print schema", "--print-schema", cli.CodeUnknownOption, 0, ""),
		reject("removed blank styling", "build --blank-text x a.pdf", cli.CodeUnknownOption, 1, ""),
		reject("unknown option at the root", "--bogus", cli.CodeUnknownOption, 0, "before a command"),
		reject("duplicate plan", "build --plan a --plan=b", cli.CodeDuplicateOption, 3, "argv:1 and argv:3"),
		reject("duplicate output under both spellings", "build -o a --output b x.pdf", cli.CodeDuplicateOption, 3, ""),
		reject("duplicate flag", "build --overwrite --overwrite", cli.CodeDuplicateOption, 2, ""),
		reject("duplicate details", "report r --details --details", cli.CodeDuplicateOption, 3, ""),
		reject("duplicate help", "build -h --help", cli.CodeDuplicateOption, 2, ""),
		rejectText("duplicate format renders in the first format", "--format text --format json", cli.CodeDuplicateOption, 2),
		reject("flag with a value", "build --overwrite=yes a.pdf", cli.CodeUnexpectedValue, 1, "--overwrite takes no value"),
		reject("details with a value", "report r --details=1", cli.CodeUnexpectedValue, 2, ""),
		reject("action with a value", "build --help=build", cli.CodeUnexpectedValue, 1, ""),
		reject("version flag with a value", "--version=1", cli.CodeUnexpectedValue, 0, ""),
		reject("blank with text", "build --blank=Appendix", cli.CodeUnexpectedValue, 1, "use a plan"),
	}
}

func applicabilityRejections() []rejected {
	return []rejected{
		reject("report option on build", "build --part x", cli.CodeInapplicableOption, 1, "(it applies to report)"),
		reject("build option on report", "report r.json --plan x", cli.CodeInapplicableOption, 2, "(it applies to build, check)"),
		reject("blank on report", "report --blank", cli.CodeInapplicableOption, 1, ""),
		reject("details on schema", "schema plan --details", cli.CodeInapplicableOption, 2, ""),
		reject("plan on version", "version --plan x", cli.CodeInapplicableOption, 1, ""),
		reject("offset on check", "check --offset 1", cli.CodeInapplicableOption, 1, ""),
		reject("plan at the root", "--plan x", cli.CodeInapplicableOption, 0, "not valid before a command"),
		reject("version flag belongs to the root", "build --version", cli.CodeInapplicableOption, 1, ""),
		reject("schema has no format", "schema --format text plan", cli.CodeInapplicableOption, 1, argFormat),
	}
}

func valueRejections() []rejected {
	return []rejected{
		reject("missing value", "build --plan", cli.CodeMissingValue, 1, planUsageSyntax),
		reject("missing short value", "build a.pdf -o", cli.CodeMissingValue, 2, ""),
		reject("option as value", "build -o --blank a.pdf", cli.CodeDashValue, 1, "--output=--blank"),
		reject("dash value", "build --report -r.json", cli.CodeDashValue, 1, ""),
		reject("stdin sentinel only for plan", "build --output -", cli.CodeDashValue, 1, ""),
		reject("end of options as value", "build --base-dir -- a.pdf", cli.CodeDashValue, 1, ""),
		reject("negative separate number", "report r --view parts --limit -1", cli.CodeDashValue, 4, "--limit=-1"),
		reject("empty attached path", "build --output=", cli.CodeEmptyValue, 1, ""),
		{name: "empty separate path", args: []string{argBuild, "-o", "", sourceAPath}, code: cli.CodeEmptyValue, index: 1},
		reject("empty plan path", "build --plan=", cli.CodeEmptyValue, 1, ""),
		reject("empty part", "report r --part=", cli.CodeEmptyValue, 2, ""),
		reject("empty format", "--format=", cli.CodeEmptyValue, 0, ""),
		reject("unknown format", "version --format=xml", cli.CodeInvalidValue, 1, "json or text"),
		reject("format is exact case", "build --format JSON", cli.CodeInvalidValue, 1, ""),
		reject("jobs zero", "build --jobs 0 a.pdf", cli.CodeInvalidJobs, 1, "at least 1"),
		reject("jobs negative", "build --jobs=-3 a.pdf", cli.CodeInvalidJobs, 1, ""),
		reject("jobs not a number", "check --jobs many", cli.CodeInvalidJobs, 1, ""),
		reject("jobs with a sign", "check --jobs=+2", cli.CodeInvalidJobs, 1, ""),
		{name: "jobs with a space", args: []string{argCheck, argJobs, " 2"}, code: cli.CodeInvalidJobs, index: 1},
		reject("jobs overflow", "check --jobs=99999999999999999999", cli.CodeInvalidJobs, 1, ""),
	}
}

func planRejections() []rejected {
	return []rejected{
		reject("no instructions", argBuild, cli.CodeMissingPlanSource, -1, planUsageSyntax),
		reject("options but no instructions", "check -o out.pdf --", cli.CodeMissingPlanSource, -1, ""),
		reject("operand then plan", "build a.pdf --plan p.json", cli.CodePlanSourceConflict, 2, "direct operands (argv:1)"),
		reject("plan then operand", "build --plan p.json a.pdf", cli.CodePlanSourceConflict, 3, "--plan (argv:1)"),
		reject("plan then blank", "build --plan=- --blank", cli.CodePlanSourceConflict, 2, ""),
		reject("plan then inline plan", "check --plan p --plan-json {}", cli.CodePlanSourceConflict, 3,
			"--plan-json cannot be combined with --plan"),
		reject("inline plan then plan", "check --plan-json={} --plan=-", cli.CodePlanSourceConflict, 2, ""),
		reject("inline plan then operand", "build --plan-json {} x.pdf", cli.CodePlanSourceConflict, 3, ""),
		reject("base dir with a plan file", "build --base-dir d --plan p.json", cli.CodeBaseDirConflict, 1, "named plan file"),
		reject("base dir with operands", "build a.pdf --base-dir=d", cli.CodeBaseDirConflict, 2, "working directory"),
	}
}

func reportRejections() []rejected {
	return []rejected{
		reject("part then page", "report r --part x --page 1", report.CodeSelectionConflict, 4,
			"--page cannot be combined with --part (argv:2)"),
		reject("page then view", "report r --page=1 --view=parts", report.CodeSelectionConflict, 3, ""),
		reject("view then part", "report r --view parts --part x", report.CodeSelectionConflict, 4, ""),
		reject("offset without view", "report r --offset 1", report.CodePagingNeedsView, 2, argView),
		reject("limit with part", "report r --part x --limit 5", report.CodePagingNeedsView, 4, ""),
		reject("details without a selection", "report r --details", report.CodeDetailsNeedSelect, 2, "--part ID, --page N, or --view"),
		reject("unknown view", "report r --view pages", report.CodeUnknownView, 2, "parts or diagnostics"),
		reject("page not a number", "report r --page x", report.CodeInvalidNumber, 2, "--page needs a whole number"),
		reject("page zero", "report r --page 0", report.CodeInvalidNumber, 2, "1-based"),
		reject("page negative", "report r --page=-1", report.CodeInvalidNumber, 2, ""),
		reject("page overflow", "report r --page=9223372036854775808", report.CodeInvalidNumber, 2, ""),
		reject("offset not a number", "report r --view=parts --offset=1.5", report.CodeInvalidNumber, 3, ""),
		reject("limit overflow", "report r --view=parts --limit=99999999999999999999", report.CodeInvalidNumber, 3, ""),
		reject("negative limit", "report r --view=parts --limit=-1", report.CodeInvalidPaging, 3, "--limit must not be negative"),
		reject("negative offset", "report r --view=parts --offset=-1", report.CodeInvalidPaging, 3, ""),
	}
}

// faultPrecedenceRejections pin the precedence of help and errors: the whole line is validated, so an invalid
// argument is reported whether it comes before or after --help.
func faultPrecedenceRejections() []rejected {
	return []rejected{
		reject("invalid UTF8 operand", "build "+string([]byte{0xff}), cli.CodeInvalidUTF8, 1, "UTF-8"),
		reject("fault before help", "build --bogus --help", cli.CodeUnknownOption, 1, ""),
		reject("fault after help", "build --help --bogus", cli.CodeUnknownOption, 2, ""),
		reject("source conflict with help", "build --help a.pdf --plan p", cli.CodePlanSourceConflict, 3, ""),
		reject("bad value swallows help", "build -o --help", cli.CodeDashValue, 1, ""),
		rejectText("format before fault selects text", "build --format=text --bogus", cli.CodeUnknownOption, 2),
		reject("format after fault does not", "build --bogus --format=text", cli.CodeUnknownOption, 1, ""),
		reject("invalid format does not select text", "build --format=txt --bogus", cli.CodeInvalidValue, 1, ""),
		rejectText("format selects text for a later missing source", "check --format text", cli.CodeMissingPlanSource, -1),
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	for _, test := range rejectedLines() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := cli.Parse(test.args)
			if err == nil {
				t.Fatalf("Parse(%q) = %+v, want a usage error", test.args, got)
			}

			usage, isUsage := errors.AsType[*cli.UsageError](err)
			if !isUsage {
				t.Fatalf("Parse(%q) error is %T, want *cli.UsageError", test.args, err)
			}

			if !reflect.DeepEqual(got, cli.Command{}) {
				t.Errorf("a failed parse returned %+v, want the zero Command", got)
			}

			checkUsageError(t, test, usage)
		})
	}
}

// contextOf is the command a failing line was being parsed as: its first word, when that names a command.
func contextOf(args []string) cli.Name {
	if len(args) > 0 {
		return cli.Help(cli.Name(args[0])).Command
	}

	return ""
}

func checkUsageError(t *testing.T, test rejected, usage *cli.UsageError) {
	t.Helper()

	if len(usage.Diagnostics) != 1 {
		t.Fatalf("Parse(%q) has %d diagnostics, want 1", test.args, len(usage.Diagnostics))
	}

	diagnostic := usage.Diagnostics[0]
	if diagnostic.Stage != report.StageUsage || diagnostic.Code != test.code {
		t.Errorf(
			"Parse(%q) = stage %q code %q, want usage %q: %s",
			test.args,
			diagnostic.Stage,
			diagnostic.Code,
			test.code,
			diagnostic.Message,
		)
	}

	checkLocation(t, test, diagnostic.Location)

	if !strings.Contains(diagnostic.Message, test.mention) {
		t.Errorf("Parse(%q) message %q does not mention %q", test.args, diagnostic.Message, test.mention)
	}

	if (usage.Format == cli.FormatText) != test.text || usage.Command != contextOf(test.args) {
		t.Errorf("Parse(%q) renders as format %d for command %q, want text=%t for %q",
			test.args, usage.Format, usage.Command, test.text, contextOf(test.args))
	}

	if !strings.Contains(usage.Error(), string(test.code)) {
		t.Errorf("Error() = %q does not carry the code", usage.Error())
	}
}

func checkLocation(t *testing.T, test rejected, location *report.Location) {
	t.Helper()

	if test.index < 0 {
		if location != nil {
			t.Errorf("Parse(%q) has location %+v, want none", test.args, location)
		}

		return
	}

	if location == nil || location.ArgvIndex == nil || *location.ArgvIndex != test.index || location.File != "argv" {
		t.Errorf("Parse(%q) location %+v, want argv:%d", test.args, location, test.index)
	}
}

// TestEveryUsageCodeIsExercised keeps UsageCodes complete and every code reachable: the rejection tables
// produce exactly the advertised codes.
func TestEveryUsageCodeIsExercised(t *testing.T) {
	t.Parallel()

	produced := map[report.Code]bool{}
	for _, test := range rejectedLines() {
		produced[test.code] = true
	}

	codes := cli.UsageCodes()

	for _, code := range codes {
		if !produced[code] {
			t.Errorf("no rejection test produces %s", code)
		}

		if !strings.HasPrefix(string(code), "usage_") && !strings.HasPrefix(string(code), "report_") {
			t.Errorf("code %q is not snake_case with a usage_ or report_ prefix", code)
		}
	}

	for code := range produced {
		if !slices.Contains(codes, code) {
			t.Errorf("%s is produced but missing from UsageCodes", code)
		}
	}

	if len(slices.Compact(slices.Sorted(slices.Values(codes)))) != len(codes) {
		t.Error("UsageCodes lists a code twice")
	}
}

func TestUsageErrorWithoutDiagnostics(t *testing.T) {
	t.Parallel()

	if got := (&cli.UsageError{}).Error(); got != "invalid command line" {
		t.Errorf("Error() = %q", got)
	}
}

func TestUsageErrorFormatsLocationAndCode(t *testing.T) {
	t.Parallel()

	_, err := cli.Parse(argv(unknownOptionLine))
	if err == nil {
		t.Fatal("a rejected line must fail")
	}

	if !strings.HasPrefix(err.Error(), "argv:1: unknown option") || !strings.HasSuffix(err.Error(), "[usage_unknown_option]") {
		t.Errorf("Error() = %v", err)
	}

	_, err = cli.Parse(nil)
	if err == nil || strings.HasPrefix(err.Error(), "argv:") {
		t.Errorf("a fault without a location must not claim one: %v", err)
	}
}
