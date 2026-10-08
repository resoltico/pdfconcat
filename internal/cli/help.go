// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// HelpDoc is the structured help of the root or of one command. Rendered as JSON it is the default help
	// output; RenderText gives the human form of the same content.
	HelpDoc struct {
		Next           *[]string      `json:"next"`
		ExecutableFrom string         `json:"executable_from,omitempty"`
		NextArgs       []string       `json:"next_args,omitempty"`
		Templates      []HelpTemplate `json:"templates,omitempty"`
		// Kind is always "help".
		Kind string `json:"kind"`
		// Command is the command described; absent for the root help.
		Command Name   `json:"command,omitempty"`
		Summary string `json:"summary"`
		// Example illustrates command grammar; Next carries runtime executable authority.
		Example string   `json:"example"`
		Usage   []string `json:"usage"`
		// Commands lists the commands; present only in the root help.
		Commands []HelpCommand `json:"commands,omitempty"`
		// Options lists exactly the options that apply.
		Options       []HelpOption `json:"options"`
		Notes         []string     `json:"notes,omitempty"`
		FormatVersion int          `json:"format_version"`
		NextOmitted   bool         `json:"next_omitted,omitzero"`
	}

	// HelpCommand is one line of the root help's command list.
	HelpCommand struct {
		Name    Name   `json:"name"`
		Summary string `json:"summary"`
	}

	// HelpOption describes one option.
	HelpOption struct {
		Name    string `json:"name"`
		Short   string `json:"short,omitempty"`
		Value   string `json:"value,omitempty"`
		Summary string `json:"summary"`
	}

	// HelpTemplate is a labeled argv recipe requiring caller-supplied paths.
	HelpTemplate struct {
		Description string   `json:"description"`
		Argv        []string `json:"argv"`
	}
	// helpEntry is the prose of one command's help.
	helpEntry struct {
		templates []HelpTemplate
		summary   string
		example   string
		usage     []string
		notes     []string
	}
)

// helpKind is the Kind of every help document.
const (
	helpKind = "help"
	// RootHelpBytes bounds root discovery output.
	RootHelpBytes = 2 << 10
	// CommandHelpBytes bounds task-specific command help.
	CommandHelpBytes = 4 << 10
)

// Help returns the structured help of a command, or of the root for an empty or unknown name.
func Help(name Name) HelpDoc {
	if _, found := commandNamed(string(name)); !found {
		name = rootContext
	}

	entry := helpContents()[name]
	doc := HelpDoc{
		FormatVersion: report.Version, Templates: entry.templates,
		Kind: helpKind, Command: name, Summary: entry.summary, Usage: entry.usage, Notes: entry.notes, Example: entry.example,
	}

	specs := optionSpecs()
	for i := range specs {
		if specs[i].appliesTo(name) {
			doc.Options = append(doc.Options,
				HelpOption{Name: specs[i].name, Short: specs[i].short, Value: specs[i].value, Summary: specs[i].summary})
		}
	}

	if name == rootContext {
		for _, command := range commandNames() {
			doc.Commands = append(doc.Commands, HelpCommand{Name: command, Summary: helpContents()[command].summary})
		}
	}

	return doc
}

// helpContents holds the prose of each command's help.
func helpContents() map[Name]helpEntry {
	return map[Name]helpEntry{
		NameBuild:  buildHelp(),
		NameCheck:  checkHelp(),
		NameReport: reportHelp(),
		NameSchema: {
			summary: "Print the JSON Schema of a plan, complete report, or structured response.",
			usage:   []string{"pdfconcat schema plan|report|response"},
			notes:   []string{"The schema is the whole output: raw JSON, never wrapped."},
			example: "pdfconcat schema plan",
		},
		NameVersion: {
			summary: "Print the version, commit, and commit date.",
			usage:   []string{"pdfconcat version"},
			example: "pdfconcat version",
		},
		NameHelp: {
			summary: "Show help for a command.",
			usage:   []string{"pdfconcat help [COMMAND]"},
			example: "pdfconcat help build",
		},
		rootContext: {
			summary: "Assemble PDFs in an explicit order and insert generated pages at explicit positions.",
			usage:   []string{"pdfconcat COMMAND [options]"},
			notes: []string{
				"Standard output is compact JSON; --format text is a human rendering, not an interface to parse.",
				"Exit status: 0 success, 2 invalid instructions, 1 I/O, backend, or publication failure, 130 interrupted.",
				"Run pdfconcat help COMMAND for one command's options.",
			},
			example: "pdfconcat build --help",
			templates: []HelpTemplate{
				{
					Description: "Template: replace input/output paths",
					Argv:        []string{"build", "-o", "merged.pdf", "first.pdf", "--blank", "second.pdf"},
				},
				{
					Description: "Template: check then build a reusable plan",
					Argv:        []string{"check", "--plan", "job.json", "--report", "job.report.json"},
				},
			},
		},
	}
}

func buildHelp() helpEntry {
	return helpEntry{
		summary: "Assemble a PDF from a plan or from direct operands.",
		usage: []string{
			"pdfconcat build --plan FILE|- [-o FILE] [options]",
			"pdfconcat build --plan-json JSON [-o FILE] [options]",
			"pdfconcat build -o FILE [options] [--] PDF|--blank ...",
		},
		templates: jobTemplates(NameBuild),
		notes: append(jobNotes(), []string{
			"Give exactly one plan source: --plan, --plan-json, or direct operands (PDF paths and --blank, in output order).",
			"After -- every argument is a path; write ./--blank for a file named --blank.",
			"A value that starts with - needs the attached form, --name=value.",
			"Default output is a bounded JSON summary; --report FILE keeps the complete result for pdfconcat report.",
		}...),
		example: "pdfconcat build --plan job.json",
	}
}

func checkHelp() helpEntry {
	return helpEntry{
		summary: "Validate inputs and layout without creating a PDF.",
		usage: []string{
			"pdfconcat check --plan FILE|- [-o FILE] [options]",
			"pdfconcat check --plan-json JSON [-o FILE] [options]",
			"pdfconcat check [options] [--] PDF|--blank ...",
		},
		templates: jobTemplates(NameCheck),
		notes: append(jobNotes(), []string{
			"Plan sources and options are those of build; -o additionally checks the destination.",
			"Checks source-known backend policy using scratch snapshots; later I/O or destination changes may still fail.",
		}...),
		example: "pdfconcat check --plan job.json --report job.report.json",
	}
}

func reportHelp() helpEntry {
	return helpEntry{
		summary: "Query a saved report without reopening any PDF.",
		usage:   []string{"pdfconcat report FILE [--part ID | --page N | --view parts|diagnostics] [--offset N] [--limit N] [--details]"},
		notes: []string{
			"Query status/counts describe this read; saved_run and selection_counts describe historical records. --expect-attempt guards " +
				"correlation using a 26-character base32 ID, not integrity or current PDF contents.",
			"--details materializes source/style/font values; complete reports use shared array indexes. " +
				"Publication targets in saved evidence are historical.",
			"Choose at most one of --part, --page, --view; --offset and --limit page a --view.",
			"A negative number needs the attached form, --limit=-1, and is rejected.",
		},
		example: "pdfconcat report job.report.json --view diagnostics --limit 20",
	}
}

// RenderText writes the help as human-readable text.
func (d HelpDoc) RenderText(out io.Writer) error {
	var text strings.Builder

	title := "pdfconcat"
	if d.Command != rootContext {
		title += " " + string(d.Command)
	}

	fmt.Fprintf(&text, "%s: %s\n\nUsage:\n", title, d.Summary)

	for _, line := range d.Usage {
		fmt.Fprintf(&text, "  %s\n", line)
	}

	if len(d.Commands) > 0 {
		rows := make([][2]string, len(d.Commands))
		for i, command := range d.Commands {
			rows[i] = [2]string{string(command.Name), command.Summary}
		}

		writeTable(&text, "Commands", rows)
	}

	rows := make([][2]string, len(d.Options))
	for i, option := range d.Options {
		rows[i] = [2]string{optionLabel(option.Name, option.Short, option.Value), option.Summary}
	}

	writeTable(&text, "Options", rows)

	if len(d.Notes) > 0 {
		text.WriteString("\nNotes:\n")

		for _, note := range d.Notes {
			fmt.Fprintf(&text, "  - %s\n", note)
		}
	}

	for _, template := range d.Templates {
		fmt.Fprintf(&text, "\n%s (argv after executable): %q\n", template.Description, template.Argv)
	}

	d.renderNext(&text)

	_, err := io.WriteString(out, text.String())
	if err != nil {
		return fmt.Errorf("render help: %w", err)
	}

	return nil
}

// writeTable writes a titled two-column list with the second column aligned.
func writeTable(text *strings.Builder, title string, rows [][2]string) {
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}

	fmt.Fprintf(text, "\n%s:\n", title)

	for _, row := range rows {
		fmt.Fprintf(text, "  %-*s  %s\n", width, row[0], row[1])
	}
}

func jobNotes() []string {
	return []string{
		"Blank size: leading blanks inherit the first following source; others inherit the nearest preceding " +
			"source. Explicit generated sizes do not change inheritance.",
		"Coordinates: x right, y up. Anchor positions the block; alignment positions lines. Clear defaults " +
			"with text.value=\"\" and background:\"none\". Whitespace: docs/BLANK_PAGES.md.",
		"No-clobber is default. --overwrite replaces PDF/report only after complete input/alias inventory; " +
			"earlier failures can write reports only to unused targets. Success can carry warnings: review warning_count and diagnostics.",
		"Workflow: check --plan job.json --report job.report.json; report job.report.json --view parts " +
			"--details; build --plan job.json. Templates require your own paths.",
	}
}

func jobTemplates(command Name) []HelpTemplate {
	const styled = `{"version":1,"items":[{"blank":{"size":"A4","background":"#eef4ff",` +
		`"text":{"value":"Rīga","font":"default","color":"#102030"}}}]}`

	return []HelpTemplate{
		{
			Description: "Template: ordered sources with one blank",
			Argv:        []string{string(command), "-o", "merged.pdf", "first.pdf", "--blank", "second.pdf"},
		},
		{
			Description: "Styled generated-only job; built-in font (use {\"file\":\"font.ttf\"} for supplied TTF)",
			Argv:        []string{string(command), "--plan-json", styled, "-o", "separator.pdf"},
		},
	}
}

// renderNext distinguishes executable argv from caller-reconstructed help or a grammar example.
func (d HelpDoc) renderNext(text *strings.Builder) {
	switch {
	case d.Next != nil:
		fmt.Fprintf(text, "\nNext step (argv):\n  %q\n", *d.Next)
	case d.NextOmitted:
		fmt.Fprintf(text, "\nNext argv omitted: use %s with %q\n", d.ExecutableFrom, d.NextArgs)
	default:
		fmt.Fprintf(text, "\nNext step:\n  %s\n", d.Example)
	}
}
