// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
)

func parse(t *testing.T, args ...string) cli.Request {
	t.Helper()

	command, err := cli.Parse(args)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", args, err)
	}

	if command.Action != cli.ActionAssemble {
		t.Fatalf("Parse(%q) action = %v", args, command.Action)
	}

	return command.Request
}

func TestParseKeepsSequenceOrder(t *testing.T) {
	t.Parallel()

	request := parse(t, "-o", "out.pdf", "--blank", "a.pdf", "--blank", "--blank", "b.pdf", "--blank")

	kinds := ""

	var kindsSb33 strings.Builder

	for _, item := range request.Sequence.Items {
		if item.Kind == assembly.Blank {
			kindsSb33.WriteString("B")
		} else {
			kindsSb33.WriteString(string(item.Path[0]))
		}
	}

	kinds += kindsSb33.String()

	if kinds != "BaBBbB" {
		t.Fatalf("sequence = %q, want BaBBbB", kinds)
	}
}

func TestParseBlankWithInlineText(t *testing.T) {
	t.Parallel()

	request := parse(t, "-o", "out.pdf", "a.pdf", "--blank=Part two = final", "--blank=", "--blank")

	text := func(index int) assembly.Option[string] { return request.Sequence.Items[index].Blank.Text.Value }
	if got := text(1).OrElse("unset"); got != "Part two = final" {
		t.Errorf("first blank text = %q", got)
	}

	if got := text(2); !got.IsSet() || got.OrElse("x") != "" {
		t.Errorf("--blank= must explicitly clear text, got %+v", got)
	}

	if text(3).IsSet() {
		t.Error("plain --blank must leave text unset")
	}
}

func TestParseEndOfOptionsMakesDirectivesLiteralPaths(t *testing.T) {
	t.Parallel()

	request := parse(t, "-o", "out.pdf", "--", "--blank", "--plan", "-o")

	if len(request.Sequence.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(request.Sequence.Items))
	}

	for index, want := range []string{"--blank", "--plan", "-o"} {
		item := request.Sequence.Items[index]
		if item.Kind != assembly.PDF || item.Path != want {
			t.Errorf("item %d = %+v, want literal path %q", index, item, want)
		}
	}
}

func TestParseOptionForms(t *testing.T) {
	t.Parallel()

	request := parse(t, "--output=out.pdf", "--overwrite", "--dry-run", "--json", "a.pdf")
	if request.Output != "out.pdf" || !request.Overwrite || !request.DryRun || !request.JSON {
		t.Fatalf("request = %+v", request)
	}

	request = parse(t, "-o=out.pdf", "-", "a.pdf")
	if request.Output != "out.pdf" || request.Sequence.Items[0].Path != "-" {
		t.Fatalf("request = %+v", request)
	}
}

func TestParseBlankStyleOptions(t *testing.T) {
	t.Parallel()

	request := parse(t, "-o", "out.pdf",
		"--blank-size", "A5", "--blank-background", "#FAFAFA", "--blank-text", "-- draft --",
		"--blank-font", "Courier", "--blank-font-size", "9pt", "--blank-color", "#333",
		"--blank-anchor", "bottom-right", "--blank-x", "-10", "--blank-y=-1cm", "--blank-width", "5cm",
		"--blank-align", "right", "--blank-leading", "1.4", "a.pdf", "--blank")

	spec, err := request.Blank.Resolve(assembly.PageDim{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	text := spec.Text
	if text.Value != "-- draft --" || text.Font != "Courier" || text.Size != 9 || text.Anchor != assembly.AnchorBottomRight ||
		text.X != -10 || text.Width < 141 || text.Width > 142 || text.Align != assembly.AlignRight || text.Leading != 1.4 {
		t.Fatalf("text = %+v", text)
	}

	if spec.Dim.Width < 419 || spec.Dim.Width > 421 ||
		spec.Background.OrElse(assembly.Color{}) != (assembly.Color{R: 0xFA, G: 0xFA, B: 0xFA}) {
		t.Fatalf("page = %+v", spec)
	}
}

func TestParsePlanInvocation(t *testing.T) {
	t.Parallel()

	request := parse(t, "--plan", "book.json")
	if request.PlanPath != "book.json" || request.Output != "" || len(request.Sequence.Items) != 0 {
		t.Fatalf("request = %+v", request)
	}

	if request = parse(t, "--plan", "-", "-o", "x.pdf"); request.PlanPath != cli.StdinPlan {
		t.Fatalf("PlanPath = %q", request.PlanPath)
	}
}

func TestParseActions(t *testing.T) {
	t.Parallel()

	for args, want := range map[string]cli.Action{
		"-h":             cli.ActionHelp,
		"--help":         cli.ActionHelp,
		"--version":      cli.ActionVersion,
		"--print-schema": cli.ActionPrintSchema,
	} {
		command, err := cli.Parse([]string{args})
		if err != nil || command.Action != want {
			t.Errorf("Parse(%q) = %+v, %v; want action %v", args, command, err, want)
		}
	}
}

func TestParseRejectsInvalidCommandLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "missing arguments"},
		{"no output", []string{"a.pdf"}, "--output"},
		{"no sequence", []string{"-o", "out.pdf"}, "missing PDF sequence"},
		{"blank only", []string{"-o", "out.pdf", "--blank"}, "no PDF"},
		{"plan with items", []string{"--plan", "p.json", "a.pdf"}, "cannot be combined"},
		{"duplicate output", []string{"-o", "1.pdf", "--output", "2.pdf", "a.pdf"}, "only once"},
		{"duplicate style option", []string{"-o", "o.pdf", "--blank-text", "a", "--blank-text", "b", "x.pdf"}, "only once"},
		{"output missing value", []string{"a.pdf", "-o"}, "requires a value"},
		{"output value is directive", []string{"-o", "--blank", "a.pdf"}, "is an option"},
		{"empty output", []string{"-o", "", "a.pdf"}, "non-empty"},
		{"unknown option", []string{"-o", "o.pdf", "--frobnicate", "a.pdf"}, "unknown option"},
		{"flag with value", []string{"-o", "o.pdf", "--dry-run=yes", "a.pdf"}, "does not take a value"},
		{"bad color", []string{"-o", "o.pdf", "--blank-color", "red", "a.pdf"}, "invalid color"},
		{"bad length", []string{"-o", "o.pdf", "--blank-x", "far", "a.pdf"}, "invalid length"},
		{"bad leading", []string{"-o", "o.pdf", "--blank-leading", "tall", "a.pdf"}, "invalid leading"},
		{"bad size", []string{"-o", "o.pdf", "--blank-size", "huge", "a.pdf"}, "invalid page size"},
		{"bad font", []string{"-o", "o.pdf", "--blank-font", "Papyrus", "a.pdf"}, "unknown font"},
		{"style value is option", []string{"-o", "o.pdf", "--blank-text", "--blank", "a.pdf"}, "is an option"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := cli.Parse(test.args)

			var usage *cli.UsageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse(%q) error = %v, want usage error containing %q", test.args, err, test.want)
			}
		})
	}
}

func TestHelpAndVersionText(t *testing.T) {
	t.Parallel()

	help := cli.HelpText()
	for _, want := range []string{"--blank", "--plan", "--blank-text", "--dry-run", "--json", "Exit status"} {
		if !strings.Contains(help, want) {
			t.Errorf("help is missing %q", want)
		}
	}

	version := cli.VersionText(cli.BuildInfo{Version: "1.2.3", Commit: "abc", CommitDate: "2026-01-02"})
	if !strings.Contains(version, "1.2.3") || !strings.Contains(version, "abc") {
		t.Errorf("version text = %q", version)
	}
}
