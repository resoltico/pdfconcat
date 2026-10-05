// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import (
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// separateValueArgs is the number of arguments an option spans when its value is a separate argument.
const separateValueArgs = 2

// Parse converts command-line arguments into a typed command without performing file I/O.
func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, Usagef("missing arguments")
	}

	parser := newParser()
	for index := 0; index < len(args); {
		next, err := parser.consume(args[index:])
		if err != nil {
			return Command{}, err
		}

		if next.action != 0 {
			return Command{Action: next.action}, nil
		}

		index += next.consumed
	}

	return parser.finish()
}

// step is the result of consuming one or more arguments: how many were used and,
// for help/version/schema, the terminal action.
type step struct {
	action   Action
	consumed int
}

// parser accumulates the request while arguments are consumed in order.
type parser struct {
	request      Request
	items        []assembly.Item
	values       map[string]valueOption
	seen         map[string]bool
	literalPaths bool
}

func newParser() *parser {
	return &parser{values: valueOptions(), seen: map[string]bool{}}
}

// consume interprets the first argument of args.
func (p *parser) consume(args []string) (step, error) {
	arg := args[0]
	if p.literalPaths || !strings.HasPrefix(arg, "-") || arg == "-" {
		p.items = append(p.items, assembly.PDFItem(arg))

		return step{consumed: 1}, nil
	}

	if p.consumeDirective(arg) {
		return step{consumed: 1}, nil
	}

	name, inline, hasInline := strings.Cut(arg, "=")

	name = canonicalName(name)
	if isFlag(name) {
		return p.consumeFlag(name, hasInline)
	}

	return p.consumeValueOption(args, name, inline, hasInline)
}

// consumeDirective handles the tokens that are part of the ordered sequence itself.
func (p *parser) consumeDirective(arg string) bool {
	switch arg {
	case optEndOfOptions:
		p.literalPaths = true
	case optBlank:
		p.items = append(p.items, assembly.BlankItem(assembly.BlankStyle{}, 1))
	default:
		text, found := strings.CutPrefix(arg, optBlank+"=")
		if !found {
			return false
		}

		style := assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Some(text)}}
		p.items = append(p.items, assembly.BlankItem(style, 1))
	}

	return true
}

func (p *parser) consumeFlag(name string, hasInline bool) (step, error) {
	if hasInline {
		return step{}, Usagef("option %s does not take a value", name)
	}

	switch name {
	case optHelp, optHelpShort:
		return step{action: ActionHelp, consumed: 1}, nil
	case optVersion:
		return step{action: ActionVersion, consumed: 1}, nil
	case optPrintSchema:
		return step{action: ActionPrintSchema, consumed: 1}, nil
	case optOverwrite:
		p.request.Overwrite = true
	case optDryRun:
		p.request.DryRun = true
	case optJSON:
		p.request.JSON = true
	default:
		return step{}, Usagef("unknown option %q", name)
	}

	return step{consumed: 1}, nil
}

func (p *parser) consumeValueOption(args []string, name, inline string, hasInline bool) (step, error) {
	option, known := p.values[name]
	if !known {
		return step{}, Usagef("unknown option %q", args[0])
	}

	if p.seen[name] {
		return step{}, Usagef("%s may be specified only once", name)
	}

	p.seen[name] = true

	value, consumed, err := p.optionValue(args, name, inline, hasInline)
	if err != nil {
		return step{}, err
	}

	if option.path && value == "" {
		return step{}, Usagef("%s requires a non-empty path", name)
	}

	err = option.apply(&p.request, value)
	if err != nil {
		return step{}, err
	}

	return step{consumed: consumed}, nil
}

// optionValue returns the value of an option and how many arguments it spans.
// A separate value may not itself be an option, so that "-o --blank" fails
// loudly instead of silently naming the output "--blank".
func (p *parser) optionValue(args []string, name, inline string, hasInline bool) (string, int, error) {
	if hasInline {
		return inline, 1, nil
	}

	if len(args) < separateValueArgs {
		return "", 0, Usagef("%s requires a value", name)
	}

	value := args[1]
	if p.looksLikeOption(value) {
		return "", 0, Usagef(
			"%s requires a value, but %q is an option; use %s=VALUE to pass a value that begins with '-'",
			name,
			value,
			name,
		)
	}

	return value, separateValueArgs, nil
}

// looksLikeOption reports whether value is exactly a recognized option token.
func (p *parser) looksLikeOption(value string) bool {
	name, _, _ := strings.Cut(value, "=")

	name = canonicalName(name)
	if _, known := p.values[name]; known {
		return true
	}

	return isFlag(name)
}

// finish validates cross-option rules and returns the assemble command.
func (p *parser) finish() (Command, error) {
	request := p.request

	switch {
	case request.PlanPath != "" && len(p.items) != 0:
		return Command{}, Usagef("--plan cannot be combined with direct sequence items")
	case request.PlanPath == "" && len(p.items) == 0:
		return Command{}, Usagef("missing PDF sequence or --plan FILE")
	case request.PlanPath == "" && request.Output == "":
		return Command{}, Usagef("missing required --output FILE")
	}

	if request.PlanPath == "" {
		request.Sequence = assembly.Sequence{Items: p.items}

		err := request.Sequence.Validate()
		if err != nil {
			return Command{}, &UsageError{Message: err.Error()}
		}
	}

	return Command{Action: ActionAssemble, Request: request}, nil
}
