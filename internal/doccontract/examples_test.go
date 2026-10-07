// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package doccontract_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
)

// quoteState is where shellWords is inside a command line.
type quoteState struct {
	words   []string
	current strings.Builder
	quote   rune
	started bool
}

const pythonLanguage = "python"

// TestDocumentedPlanExamplesDecode requires every json block of the user documents to be a plan the decoder
// accepts. Output samples are written in text blocks, so a json block is always a plan.
func TestDocumentedPlanExamplesDecode(t *testing.T) {
	t.Parallel()

	for _, file := range documentedFiles() {
		for index, example := range fencedBlocks(readDocument(t, file), "json") {
			_, err := plan.Decode(context.Background(), plan.Input{Name: file, BaseDir: "/plan"}, strings.NewReader(example))
			if err != nil {
				t.Errorf("%s json example %d is not a valid plan: %v\n%s", file, index+1, err, example)
			}
		}
	}
}

// pythonCommand finds an interpreter; documentation checks must not silently skip.
func pythonCommand(t *testing.T) string {
	t.Helper()

	for _, name := range []string{"python3", pythonLanguage} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path
		}
	}

	t.Fatal("python3 is required to check the documented recipes; install it or put it on PATH")

	return ""
}

// compilePython byte-compiles one recipe and returns the interpreter's complaint, if any.
func compilePython(t *testing.T, interpreter, source string) string {
	t.Helper()

	script := filepath.Join(t.TempDir(), "recipe.py")

	err := os.WriteFile(script, []byte(source), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	output, err := exec.CommandContext(t.Context(), interpreter, "-m", "py_compile", script).CombinedOutput()
	if err != nil {
		return err.Error() + "\n" + string(output)
	}

	return ""
}

// TestDocumentedPythonRecipesCompile byte-compiles every python block, and requires docs/CLI.md to carry exactly
// one plan-generation recipe that states its ordering and its encoding.
func TestDocumentedPythonRecipesCompile(t *testing.T) {
	t.Parallel()

	interpreter := pythonCommand(t)
	checked := 0

	for _, file := range documentedFiles() {
		for index, source := range fencedBlocks(readDocument(t, file), pythonLanguage) {
			if problem := compilePython(t, interpreter, source); problem != "" {
				t.Errorf("%s python block %d does not compile: %s", file, index+1, problem)
			}

			checked++
		}
	}

	if checked == 0 {
		t.Fatal("no python blocks found: the documented recipe has gone missing")
	}

	recipes := fencedBlocks(readDocument(t, cliDocument), pythonLanguage)
	if len(recipes) != 1 {
		t.Fatalf("docs/CLI.md has %d python recipes, want exactly one", len(recipes))
	}

	for _, required := range []string{"sorted(", `encoding="utf-8"`, `"version": 1`, "raise SystemExit"} {
		if !strings.Contains(recipes[0], required) {
			t.Errorf("the python recipe lacks %q", required)
		}
	}
}

// shellWords splits one documented command line the way a POSIX shell would for the cases the documents use:
// single and double quotes, a trailing # comment, and a "< file" redirection.
func shellWords(line string) []string {
	state := &quoteState{}

	for _, char := range line {
		if state.quote != 0 {
			state.inQuotes(char)

			continue
		}

		if char == '#' && !state.started {
			break
		}

		state.outsideQuotes(char)
	}

	state.flush()

	if index := slices.Index(state.words, "<"); index >= 0 {
		return state.words[:index]
	}

	return state.words
}

func (s *quoteState) flush() {
	if s.started {
		s.words = append(s.words, s.current.String())
	}

	s.current.Reset()

	s.started = false
}

func (s *quoteState) inQuotes(char rune) {
	if char == s.quote {
		s.quote = 0

		return
	}

	s.current.WriteRune(char)
}

func (s *quoteState) outsideQuotes(char rune) {
	switch char {
	case '\'', '"':
		s.quote, s.started = char, true
	case ' ', '\t':
		s.flush()
	default:
		s.current.WriteRune(char)

		s.started = true
	}
}

// TestDocumentedCommandsParse runs every pdfconcat command of a console block through the parser: examples in
// the documents are valid command lines for the grammar they describe.
func TestDocumentedCommandsParse(t *testing.T) {
	t.Parallel()

	checked := 0

	for _, file := range documentedFiles() {
		for _, block := range fencedBlocks(readDocument(t, file), "console") {
			for line := range strings.SplitSeq(block, "\n") {
				if words := shellWords(line); len(words) > 0 {
					checkDocumentedCommand(t, file, line, words)

					checked++
				}
			}
		}
	}

	if checked < 10 {
		t.Fatalf("only %d documented commands found; the examples have gone missing", checked)
	}
}

func checkDocumentedCommand(t *testing.T, file, line string, words []string) {
	t.Helper()

	if words[0] != "pdfconcat" {
		t.Errorf("%s: console line %q is not a pdfconcat command", file, line)

		return
	}

	_, err := cli.Parse(words[1:])
	if err != nil {
		t.Errorf("%s: documented command %q is rejected: %v", file, line, err)
	}
}
