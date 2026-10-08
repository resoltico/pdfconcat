// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
)

type (
	// failingWriter fails every write.
	failingWriter struct{}

	// helpOptionJSON is an option as the help JSON shows it.
	helpOptionJSON struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}

	// helpJSON is the shape agents read from the help output.
	helpJSON struct {
		Kind     string            `json:"kind"`
		Command  string            `json:"command"`
		Summary  string            `json:"summary"`
		Example  string            `json:"example"`
		Usage    []string          `json:"usage"`
		Commands []json.RawMessage `json:"commands"`
		Options  []helpOptionJSON  `json:"options"`
	}
)

// errClosed is the failure of a closed stream.
var errClosed = errors.New("closed")

func (failingWriter) Write([]byte) (int, error) { return 0, errClosed }

// optionNames lists the option names of a help document in order.
func optionNames(doc cli.HelpDoc) []string {
	names := make([]string, len(doc.Options))
	for i, option := range doc.Options {
		names[i] = option.Name
	}

	return names
}

// allCommands lists every command, with the empty name for the root.
func allCommands() []cli.Name {
	return []cli.Name{"", cli.NameBuild, cli.NameCheck, cli.NameReport, cli.NameSchema, cli.NameVersion, cli.NameHelp}
}

// expectedOptions is the independent statement of which options apply where.
func expectedOptions() map[cli.Name][]string {
	planning := []string{
		planOption,
		argPlanJSON,
		argBlank,
		argBaseDir,
		"--output",
		"--overwrite",
		"--report",
		argJobs,
		"--details",
		argFormat,
		argHelp,
	}

	return map[cli.Name][]string{
		"":              {argFormat, argVersion, argHelp},
		cli.NameBuild:   planning,
		cli.NameCheck:   planning,
		cli.NameReport:  {"--expect-attempt", argPart, "--page", argView, "--offset", argLimit, "--details", argFormat, argHelp},
		cli.NameSchema:  {argHelp},
		cli.NameVersion: {argFormat, argHelp},
		cli.NameHelp:    {argFormat, argHelp},
	}
}

func TestHelpListsExactlyTheApplicableOptions(t *testing.T) {
	t.Parallel()

	for name, want := range expectedOptions() {
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()

			if got := optionNames(cli.Help(name)); !slices.Equal(got, want) {
				t.Errorf("Help(%q) options = %v, want %v", name, got, want)
			}
		})
	}
}

// probe is the arguments that use option once in command: a sample value is added for options that take one.
func probe(command cli.Name, option string) []string {
	samples := map[string]string{
		"--expect-attempt": "fixture-attempt",
		planOption:         "x",
		argPlanJSON:        "{}",
		argBaseDir:         "x",
		"--output":         "x",
		"--report":         "x",
		argJobs:            "1",
		argPart:            "x",
		"--page":           "1",
		argView:            "parts",
		"--offset":         "0",
		argLimit:           "1",
		argFormat:          "json",
	}

	args := []string{string(command), option}
	if sample, takesValue := samples[option]; takesValue {
		args = append(args, sample)
	}

	if command == "" {
		return args[1:]
	}

	return args
}

// TestHelpAgreesWithTheParser probes the parser with every option of every command: it is accepted exactly
// where help lists it and rejected as not applicable elsewhere.
func TestHelpAgreesWithTheParser(t *testing.T) {
	t.Parallel()

	var every []string

	for _, options := range expectedOptions() {
		every = append(every, options...)
	}

	slices.Sort(every)
	every = slices.Compact(every)

	for command, listed := range expectedOptions() {
		for _, option := range every {
			args := probe(command, option)

			_, err := cli.Parse(args)
			usage, _ := errors.AsType[*cli.UsageError](err)
			rejected := usage != nil && slices.Contains([]string{string(cli.CodeInapplicableOption), string(cli.CodeUnknownOption)},
				string(usage.Diagnostics[0].Code))

			if listed := slices.Contains(listed, option); listed == rejected {
				t.Errorf("Parse(%q): rejected as not applicable = %t, but help lists the option = %t (err %v)", args, rejected, listed, err)
			}
		}
	}
}

// TestHelpExamplesAreValidCommands runs every example through the parser: the one next step is a real command.
func TestHelpExamplesAreValidCommands(t *testing.T) {
	t.Parallel()

	for _, name := range allCommands() {
		example := cli.Help(name).Example

		words := strings.Fields(example)
		if len(words) < 2 || words[0] != "pdfconcat" {
			t.Fatalf("Help(%q) example %q is not a pdfconcat command", name, example)
		}

		command, err := cli.Parse(words[1:])
		if err != nil {
			t.Errorf("Help(%q) example %q does not parse: %v", name, example, err)

			continue
		}

		want := name
		if name == "" {
			want = cli.NameHelp
		}

		if command.Name != want {
			t.Errorf("Help(%q) example %q is the %s command, want %s", name, example, command.Name, want)
		}
	}
}

func TestRootHelpFitsOneScreenAndNamesTheNextStep(t *testing.T) {
	t.Parallel()

	doc := cli.Help("")

	var text bytes.Buffer

	err := doc.RenderText(&text)
	if err != nil {
		t.Fatal(err)
	}

	if lines := strings.Count(text.String(), "\n"); lines > 30 {
		t.Errorf("root help has %d lines, want at most 30:\n%s", lines, text.String())
	}

	if !strings.Contains(text.String(), "Next step:\n  "+doc.Example+"\n") {
		t.Errorf("root help does not end with its example:\n%s", text.String())
	}

	for _, command := range []string{argBuild, argCheck, argReport, "schema", "version", argHelpCommand} {
		if !slices.ContainsFunc(doc.Commands, func(c cli.HelpCommand) bool { return string(c.Name) == command && c.Summary != "" }) {
			t.Errorf("root help does not describe %s", command)
		}
	}

	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	if len(encoded) > 2048 || bytes.ContainsRune(encoded, '\n') {
		t.Errorf("root help JSON is %d bytes (want compact, at most 2048)", len(encoded))
	}
}

func TestCommandHelpIsStructured(t *testing.T) {
	t.Parallel()

	for _, name := range allCommands()[1:] {
		encoded, err := json.Marshal(cli.Help(name))
		if err != nil {
			t.Fatal(err)
		}

		var decoded helpJSON

		err = json.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatal(err)
		}

		undescribed := slices.ContainsFunc(decoded.Options, func(o helpOptionJSON) bool { return o.Name == "" || o.Summary == "" })

		switch {
		case decoded.Kind != argHelpCommand,
			decoded.Command != string(name),
			decoded.Summary == "",
			len(decoded.Usage) == 0,
			decoded.Example == "":
			t.Errorf("Help(%q) is incomplete: %s", name, encoded)
		case len(decoded.Commands) != 0:
			t.Errorf("Help(%q) lists commands; only the root does", name)
		case len(decoded.Options) == 0 || undescribed:
			t.Errorf("Help(%q) has an undescribed option: %s", name, encoded)
		default:
		}
	}
}

func TestHelpTextShowsUsageOptionsAndExample(t *testing.T) {
	t.Parallel()

	var text bytes.Buffer

	err := cli.Help(cli.NameBuild).RenderText(&text)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"pdfconcat build: ", "Usage:\n  pdfconcat build --plan FILE|-", "-o, --output FILE  ", "Notes:\n  - ",
		"Next step:\n  pdfconcat build --plan job.json\n",
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("build help text lacks %q:\n%s", want, text.String())
		}
	}

	for _, absent := range []string{argPart, argLimit, "Commands:"} {
		if strings.Contains(text.String(), absent) {
			t.Errorf("build help mentions %q:\n%s", absent, text.String())
		}
	}

	var version bytes.Buffer

	err = cli.Help(cli.NameVersion).RenderText(&version)
	if err != nil || strings.Contains(version.String(), "Notes:") {
		t.Errorf("version help has no notes section (err %v):\n%s", err, version.String())
	}
}

func TestUnknownHelpNameIsRootHelp(t *testing.T) {
	t.Parallel()

	if got := cli.Help("bogus"); got.Command != "" || len(got.Commands) == 0 {
		t.Errorf("Help(bogus) = %+v, want the root help", got)
	}
}

func TestHelpTextReportsWriteFailure(t *testing.T) {
	t.Parallel()

	err := cli.Help("").RenderText(failingWriter{})
	if !errors.Is(err, errClosed) {
		t.Errorf("RenderText on a failing writer = %v, want the writer's error", err)
	}
}
