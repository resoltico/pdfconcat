// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package doccontract_test

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

// documentedOption is one row of the options table.
type documentedOption struct {
	short    string
	contexts []string
}

const cliDocument = "docs/CLI.md"

// helpContexts are the places an option can apply, in the words docs/CLI.md uses.
func helpContexts() map[string]cli.Name {
	return map[string]cli.Name{
		"root": "", "build": cli.NameBuild, "check": cli.NameCheck, "report": cli.NameReport,
		"schema": cli.NameSchema, "version": cli.NameVersion, "help": cli.NameHelp,
	}
}

// documentedOptions parses the options table of docs/CLI.md.
func documentedOptions(t *testing.T) map[string]documentedOption {
	t.Helper()

	options := map[string]documentedOption{}

	for _, row := range tableRows(t, readDocument(t, cliDocument), "## Options") {
		if len(row) != 3 {
			t.Fatalf("options row %q has %d cells, want 3", row, len(row))
		}

		var long, short string

		for spelling := range strings.SplitSeq(row[0], ", ") {
			name := strings.Fields(strings.Trim(spelling, "`"))[0]
			if strings.HasPrefix(name, "--") {
				long = name
			} else {
				short = name
			}
		}

		options[long] = documentedOption{short: short, contexts: contextsOf(t, row[1])}
	}

	return options
}

// contextsOf expands the "Applies to" cell: command names, root, or all.
func contextsOf(t *testing.T, cell string) []string {
	t.Helper()

	if cell == "all" {
		return slices.Sorted(func(yield func(string) bool) {
			for word := range helpContexts() {
				if !yield(word) {
					return
				}
			}
		})
	}

	words := strings.Split(cell, ", ")
	for _, word := range words {
		if _, known := helpContexts()[word]; !known {
			t.Fatalf("unknown context %q in %q", word, cell)
		}
	}

	return words
}

// TestDocumentedOptionsMatchHelp compares the options table of docs/CLI.md with the structured help of every
// command, in both directions, including short spellings.
func TestDocumentedOptionsMatchHelp(t *testing.T) {
	t.Parallel()

	documented := documentedOptions(t)

	for word, command := range helpContexts() {
		help := cli.Help(command).Options
		inHelp := make([]string, len(help))

		for i, option := range help {
			inHelp[i] = option.Name

			if documented[option.Name].short != option.Short {
				t.Errorf("%s: help gives short spelling %q, docs/CLI.md %q", option.Name, option.Short, documented[option.Name].short)
			}
		}

		inDocs := optionsAppliedTo(documented, word)

		slices.Sort(inHelp)

		if !slices.Equal(inHelp, inDocs) {
			t.Errorf("options for %q: help lists %v, docs/CLI.md lists %v", word, inHelp, inDocs)
		}
	}
}

// optionsAppliedTo lists, sorted, the documented options that apply in the context.
func optionsAppliedTo(documented map[string]documentedOption, word string) []string {
	var options []string

	for option, row := range documented {
		if slices.Contains(row.contexts, word) {
			options = append(options, option)
		}
	}

	slices.Sort(options)

	return options
}

// TestDocumentedCommandsMatchHelp keeps the commands table equal to the command list of the root help.
func TestDocumentedCommandsMatchHelp(t *testing.T) {
	t.Parallel()

	rows := tableRows(t, readDocument(t, cliDocument), "## Commands")
	documented := make([]string, len(rows))

	for i, row := range rows {
		documented[i] = strings.Trim(row[0], "`")
	}

	commands := cli.Help("").Commands
	inHelp := make([]string, len(commands))

	for i, command := range commands {
		inHelp[i] = string(command.Name)
	}

	if !slices.Equal(documented, inHelp) {
		t.Errorf("commands: docs/CLI.md lists %v, the root help lists %v", documented, inHelp)
	}
}

// TestUsageCodesAreDocumented keeps the error-code table equal to the codes the parser can produce.
func TestUsageCodesAreDocumented(t *testing.T) {
	t.Parallel()

	text := readDocument(t, cliDocument)

	for _, code := range cli.UsageCodes() {
		if !strings.Contains(text, "| `"+string(code)+"` |") {
			t.Errorf("code %s has no row in docs/CLI.md", code)
		}
	}

	for _, match := range regexp.MustCompile("`(usage_[a-z_]+)`").FindAllStringSubmatch(text, -1) {
		if !slices.Contains(cli.UsageCodes(), report.Code(match[1])) {
			t.Errorf("docs/CLI.md mentions %s, which the parser never produces", match[1])
		}
	}
}

// TestDocumentedExitStatusesMatchTheCode compares the exit-status table with report.Status.ExitCode.
func TestDocumentedExitStatusesMatchTheCode(t *testing.T) {
	t.Parallel()

	rows := tableRows(t, readDocument(t, cliDocument), "## Exit status")
	documented := make([]string, len(rows))

	for i, row := range rows {
		documented[i] = strings.Trim(row[0], "`")
	}

	statuses := []report.Status{report.StatusOK, report.StatusInvalid, report.StatusFailed, report.StatusInterrupted}
	inCode := make([]string, len(statuses))

	for i, status := range statuses {
		inCode[i] = strconv.Itoa(status.ExitCode())
	}

	slices.Sort(documented)
	slices.Sort(inCode)

	if !slices.Equal(documented, inCode) {
		t.Errorf("exit statuses: docs/CLI.md lists %v, report.Status defines %v", documented, inCode)
	}
}
