// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// parser holds the state of one left-to-right pass over the arguments. The first fault ends the pass.
	parser struct {
		// seen maps an option's long name to the argument index where it first appeared.
		seen map[string]int
		// selected names the first report selection option; selectedAt is its index.
		selected string
		// context is the command being parsed, or rootContext before one is named.
		context Name
		args    []string
		specs   []optionSpec
		cmd     Command
		// selectedAt is the argument index of the first selection option.
		selectedAt int
		// sourceAt is the argument index that first fixed cmd.PlanSource.
		sourceAt   int
		endOptions bool
		help       bool
		version    bool
	}

	// argument is an option token split at "=": name is the option, value the attached text if attached.
	argument struct {
		name, value string
		attached    bool
	}
)

// Parse reads the arguments after the executable name. It returns a *UsageError for a rejected command line.
//
// The pass runs left to right. Every option is validated as it is read, so an invalid argument anywhere on the
// line is reported even when --help is also present; help is shown only for an otherwise valid line, and
// requirements that need the whole line (a plan source, a report file) are not enforced for a help request.
// After a lone "--" every argument is a literal operand.
func Parse(args []string) (Command, error) {
	state := &parser{args: args, specs: optionSpecs(), seen: map[string]int{}}

	err := state.run()
	if err != nil {
		return Command{}, err
	}

	return state.cmd, nil
}

func (p *parser) run() error {
	next, err := p.selectCommand()
	if err != nil {
		return err
	}

	for next < len(p.args) {
		last, stepErr := p.step(next)
		if stepErr != nil {
			return stepErr
		}

		next = last + 1
	}

	return p.finish()
}

// fail builds the error for a fault at argument index, or at no argument when index is negative.
func (p *parser) fail(code report.Code, index int, format string, args ...any) error {
	return usageError(p.context, p.cmd.Format, code, index, fmt.Sprintf(format, args...))
}

// selectCommand reads the command word and returns the index of the first argument after it. A line that
// begins with an option has no command yet; only --help, --version, and --format are valid there.
func (p *parser) selectCommand() (int, error) {
	if len(p.args) == 0 {
		return 0, p.missingCommand()
	}

	if err := p.argumentEncoding(0); err != nil {
		return 0, err
	}

	if strings.HasPrefix(p.args[0], "-") {
		return 0, nil
	}

	name, found := commandNamed(p.args[0])
	if !found {
		return 0, p.fail(CodeUnknownCommand, 0, "unknown command %q; commands: %s", p.args[0], joinNames(commandNames()))
	}

	p.context, p.cmd.Name = name, name

	return 1, nil
}

func (p *parser) missingCommand() error {
	return p.fail(CodeMissingCommand, -1, "no command given; commands: %s; try pdfconcat --help", joinNames(commandNames()))
}

func commandNamed(word string) (Name, bool) {
	for _, name := range commandNames() {
		if string(name) == word {
			return name, true
		}
	}

	return rootContext, false
}

func joinNames(names []Name) string {
	words := make([]string, len(names))
	for i, name := range names {
		words[i] = string(name)
	}

	return strings.Join(words, ", ")
}

// step consumes the argument at index (and the value after it, if it takes one) and returns the index of the
// last argument consumed.
func (p *parser) step(index int) (int, error) {
	if err := p.argumentEncoding(index); err != nil {
		return index, err
	}

	arg := p.args[index]

	switch {
	case p.endOptions:
		return index, p.operand(index, arg)
	case arg == endOfOptions:
		p.endOptions = true

		return index, nil
	case arg == stdinPath || !strings.HasPrefix(arg, "-"):
		return index, p.operand(index, arg)
	default:
		return p.option(index, splitOption(arg))
	}
}

// splitOption separates "--name=value"; only long options take an attached value.
func splitOption(arg string) argument {
	if !strings.HasPrefix(arg, "--") {
		return argument{name: arg}
	}

	name, value, attached := strings.Cut(arg, "=")

	return argument{name: name, value: value, attached: attached}
}

// lookup finds the option for name in the current context. The second result says whether any command has it.
func (p *parser) lookup(name string) (*optionSpec, bool) {
	known := false

	for i := range p.specs {
		candidate := &p.specs[i]
		if candidate.name != name && candidate.short != name {
			continue
		}

		known = true

		if candidate.appliesTo(p.context) {
			return candidate, true
		}
	}

	return nil, known
}

// option handles one option token and returns the index of the last argument it consumed.
func (p *parser) option(index int, arg argument) (int, error) {
	spec, known := p.lookup(arg.name)

	switch {
	case spec == nil && known:
		return index, p.inapplicableOption(index, arg.name)
	case spec == nil:
		return index, p.unknownOption(index, arg.name)
	case spec.kind == kindDirective:
		return index, p.blank(index, arg)
	}

	first, repeated := p.seen[spec.name]
	if repeated {
		return index, p.fail(CodeDuplicateOption, index, "%s given twice (argv:%d and argv:%d); each option may appear once",
			spec.name, first, index)
	}

	p.seen[spec.name] = index

	if spec.kind != kindValue {
		return index, p.bare(spec, index, arg)
	}

	value, last, err := p.valueOf(spec, index, arg)
	if err != nil {
		return index, err
	}

	if value == "" && !spec.allowEmpty {
		return index, p.fail(CodeEmptyValue, index, "%s needs a non-empty %s", spec.name, spec.value)
	}

	return last, spec.apply(p, index, value)
}

// bare records a flag or an action, which take no value.
func (p *parser) bare(spec *optionSpec, index int, arg argument) error {
	if arg.attached {
		return p.fail(CodeUnexpectedValue, index, "%s takes no value; write %s alone", spec.name, spec.name)
	}

	return spec.apply(p, index, "")
}

// valueOf returns the value of a value option and the index of the last argument it used. A separate value
// must not look like an option, so a misspelled option is never swallowed as a file name; a value that begins
// with "-" is attached with =.
func (p *parser) valueOf(spec *optionSpec, index int, arg argument) (string, int, error) {
	if arg.attached {
		return arg.value, index, nil
	}

	if index+1 >= len(p.args) {
		return "", index, p.fail(CodeMissingValue, index, "%s needs a value: %s %s", spec.name, spec.name, spec.value)
	}

	if err := p.argumentEncoding(index + 1); err != nil {
		return "", index, err
	}

	next := p.args[index+1]
	if strings.HasPrefix(next, "-") && (next != stdinPath || !spec.allowStdin) {
		return "", index, p.fail(CodeDashValue, index,
			"%s needs a value but the next argument is %q, which starts with '-'; if %q is the value write %s=%s",
			spec.name, next, next, spec.name, next)
	}

	return next, index + 1, nil
}

// unknownOption explains an option that no command has, naming what is valid here.
func (p *parser) unknownOption(index int, name string) error {
	return p.fail(CodeUnknownOption, index, "unknown option %q %s; options that apply: %s", name, p.where(), p.applicable())
}

// inapplicableOption explains an option of another command, naming where it is valid and what is valid here.
func (p *parser) inapplicableOption(index int, name string) error {
	return p.fail(CodeInapplicableOption, index, "option %q is not valid %s%s; options that apply: %s",
		name, p.where(), p.validFor(name), p.applicable())
}

// where names the current context for messages.
func (p *parser) where() string {
	if p.context == rootContext {
		return "before a command"
	}

	return "for " + string(p.context)
}

// validFor lists the commands an option applies to, as " (it applies to build, check)"; empty when it applies
// to none, which is the root-only --version.
func (p *parser) validFor(name string) string {
	var commands []Name

	for i := range p.specs {
		if p.specs[i].name == name {
			commands = append(commands, p.specs[i].commands...)
		}
	}

	commands = slices.DeleteFunc(slices.Compact(commands), func(command Name) bool { return command == rootContext })
	if len(commands) == 0 {
		return ""
	}

	return " (it applies to " + joinNames(commands) + ")"
}

// applicable lists the options valid in the current context.
func (p *parser) applicable() string {
	var names []string

	for i := range p.specs {
		if p.specs[i].appliesTo(p.context) {
			names = append(names, p.specs[i].label())
		}
	}

	return strings.Join(names, "; ")
}

// argumentEncoding rejects byte strings that JSON cannot represent losslessly before any parser echo.
func (p *parser) argumentEncoding(index int) error {
	if !utf8.ValidString(p.args[index]) {
		return p.fail(CodeInvalidUTF8, index, "argument is not valid UTF-8; use Unicode text and filenames")
	}

	return nil
}
