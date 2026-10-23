// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package cli adapts argument grammar, process input and presentation to core.
package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/core"
)

// Operand is a source path or a plain generated page at an argv position.
type Operand struct {
	Path  string
	Blank bool
	Index int
}

// Command is a parsed request; parsing performs no filesystem I/O.
type Command struct {
	Name, Plan, Output, BaseDir, FitTo, Report, Schema string
	Overwrite, Help, JSON                              bool
	Operands                                           []Operand
}
type option struct{ name, alias, value, description string }

var options = []option{
	{"--blank", "", "", "Insert one generated page with default style."},
	{"--plan", "", "FILE|-", "Read a plan v4 JSON file or standard input."},
	{"--output", "-o", "FILE", "Output PDF; overrides plan output."},
	{"--base-dir", "", "DIR", "Base directory for --plan -."},
	{"--fit-to", "", "SIZE", "Fit every page onto this sheet; refuses widgets and numeric local destinations."},
	{"--report", "", "FILE", "Write a full JSON report."},
	{"--overwrite", "", "", "Replace existing output/report files."},
	{"--json", "", "", "Print one JSON result to standard output."},
	{"--help", "-h", "", "Show help."},
}

// Parse returns command intent and an optional complete normalized failure.
// Token validation and error-format discovery perform no I/O.
func Parse(args []string) (Command, *core.Result) {
	c := Command{}
	failure := func(kind core.InvocationFaultKind, message string, index int) (Command, *core.Result) {
		c.JSON = wantsJSON(args)
		r := core.InvocationFailure(operationHint(c.Name), kind, message, fmt.Sprintf("argv:%d", index))
		return c, &r
	}
	for i, arg := range args {
		if !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return failure("usage_invalid_value", "Arguments must be valid UTF-8 without NUL.", i)
		}
	}
	if len(args) == 0 {
		c.Help = true
		return c, nil
	}
	switch args[0] {
	case "--help", "-h":
		c.Help = true
		if len(args) > 1 {
			return failure("usage_conflict", "Root help accepts no other arguments.", 1)
		}
		return c, nil
	case "--version":
		c.Name = "version"
	case "build", "check", "version", "schema":
		c.Name = args[0]
	case "help":
		c.Help = true
		if len(args) > 2 {
			return failure("usage_conflict", "help accepts at most one command.", 2)
		}
		if len(args) == 2 {
			c.Name = args[1]
			if !knownCommand(c.Name) {
				unknown := c.Name
				c.Name = ""
				return failure("usage_unknown_command", "Unknown help command: "+unknown, 1)
			}
		}
		return c, nil
	default:
		if strings.HasPrefix(args[0], "-") {
			return failure("usage_unknown_option", "Unknown root option: "+args[0], 0)
		}
		return failure("usage_unknown_command", "Unknown command: "+args[0], 0)
	}
	if c.Name == "schema" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			c.Help = true
			return c, nil
		}
		if len(args) != 2 || args[1] != "plan" && args[1] != "result" {
			return failure("usage_invalid_value", "schema requires plan or result.", 1)
		}
		c.Schema = args[1]
		return c, nil
	}
	seen := map[string]bool{}
	positional := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !positional && arg == "--" {
			positional = true
			continue
		}
		if !positional && arg == "--blank" && c.Name != "version" {
			c.Operands = append(c.Operands, Operand{Blank: true, Index: i})
			continue
		}
		if !positional && strings.HasPrefix(arg, "-") && arg != "-" {
			name, _, _ := strings.Cut(arg, "=")
			spec, value, equals := optionToken(arg)
			if !optionAllowed(c.Name, spec) {
				return failure("usage_unknown_option", "Unknown option: "+name, i)
			}
			if seen[spec.name] {
				return failure("usage_conflict", "Option may be used only once: "+spec.name, i)
			}
			seen[spec.name] = true
			if spec.value != "" {
				if !equals {
					next, ok := separateValueIndex(args, i)
					if !ok {
						return failure("usage_missing_value", spec.name+" needs "+spec.value, i)
					}
					i = next
					value = args[i]
				}
				if value == "" {
					return failure("usage_invalid_value", spec.name+" must not be empty.", i)
				}
			} else if equals {
				return failure("usage_invalid_value", spec.name+" accepts no value.", i)
			}
			switch spec.name {
			case "--plan":
				c.Plan = value
			case "--output":
				c.Output = value
			case "--base-dir":
				c.BaseDir = value
			case "--fit-to":
				if _, err := core.ParseSheet(value); err != nil {
					return failure("usage_invalid_value", err.Error(), i)
				}
				c.FitTo = value
			case "--report":
				c.Report = value
			case "--overwrite":
				c.Overwrite = true
			case "--json":
				c.JSON = true
			case "--help":
				c.Help = true
			}
		} else {
			c.Operands = append(c.Operands, Operand{Path: arg, Index: i})
		}
	}
	if c.Help {
		return c, nil
	}
	if c.Name == "version" {
		if len(c.Operands) > 0 {
			return failure("usage_conflict", "version accepts no operands.", c.Operands[0].Index)
		}
		return c, nil
	}
	if c.Plan != "" && len(c.Operands) > 0 {
		return failure("usage_conflict", "--plan cannot be combined with PDF operands or --blank.", 0)
	}
	if c.BaseDir != "" && c.Plan != "-" {
		return failure("usage_conflict", "--base-dir requires --plan -.", 0)
	}
	if c.Plan == "" && len(c.Operands) == 0 {
		return failure("usage_missing_input", "Give --plan FILE|- or PDF operands.", 0)
	}
	return c, nil
}

func optionToken(arg string) (*option, string, bool) {
	name, value, equals := strings.Cut(arg, "=")
	for i := range options {
		if name == options[i].name || options[i].alias != "" && name == options[i].alias {
			return &options[i], value, equals
		}
	}
	return nil, value, equals
}

func optionAllowed(command string, spec *option) bool {
	return spec != nil && (command != "version" && command != "--version" || spec.name == "--json" || spec.name == "--help")
}

func separateValueIndex(args []string, index int) (int, bool) {
	next := index + 1
	if next >= len(args) || args[next] == "--blank" {
		return index, false
	}
	if spec, _, _ := optionToken(args[next]); spec != nil {
		return index, false
	}
	// A literal -- remains a value here; it ends options only at an option boundary.
	return next, true
}

// wantsJSON locates the exact machine-output flag before the first delimiter.
func wantsJSON(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "build", "check", "version", "--version":
	default:
		return false
	}
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		if arg == "--json" {
			return true
		}
	}
	return false
}

func knownCommand(name string) bool {
	return name == "build" || name == "check" || name == "schema" || name == "version"
}

// Help uses the same option declarations as Parse.
func Help(command string) string {
	var b strings.Builder
	switch command {
	case "":
		b.WriteString("PDFConcat assembles selected PDF pages, sections and generated pages from one reviewed plan.\n\nCommands: build, check, schema plan|result, version, help [COMMAND]\nUse pdfconcat help build for inputs and options.\n")
		return b.String()
	case "schema":
		return "Usage: pdfconcat schema plan|result\nPrint the JSON Schema.\n"
	case "version":
		b.WriteString("Usage: pdfconcat version [--json]\n")
	default:
		fmt.Fprintf(&b, "Usage: pdfconcat %s [options] (--plan FILE|- | PDF... [--blank]...)\n", command)
		b.WriteString("Options may interleave with operands; -- ends options.\n")
	}
	for _, o := range options {
		if !optionAllowed(command, &o) {
			continue
		}
		names := o.name
		if o.alias != "" {
			names += " / " + o.alias
		}
		if o.value != "" {
			names += " " + o.value
		}
		fmt.Fprintf(&b, "  %-28s %s\n", names, o.description)
	}
	return b.String()
}
