// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type (
	exportActionStep struct {
		Name  string `yaml:"name"`
		Shell string `yaml:"shell"`
		Run   string `yaml:"run"`
	}
	exportActionRuns struct {
		Steps []exportActionStep `yaml:"steps"`
	}
	exportAction struct {
		Runs exportActionRuns `yaml:"runs"`
	}
	exportCase struct {
		name, input, want string
		reject            bool
	}
)

const (
	workflowBash        = "bash"
	existingEnvironment = "EXISTING=preserved\n"
)

func TestPrepareActionExportsOnlyValidatedEnvironmentAssignments(t *testing.T) {
	t.Parallel()
	script := prepareExportScript(t)

	cases := []exportCase{
		{"comments and blank lines", "# tools\n\n  # another\n  KEY=v1.2.3  \nDIGEST=h1:abc+=\n", "KEY=v1.2.3\nDIGEST=h1:abc+=\n", false},
		{"real pinned file", string(readRepoFile(t, toolVersionsPath)), "", false},
		{"malformed after valid", "GOOD=v1.0.0\nBAD LINE\n", "", true},
		{"duplicate after valid", "KEY=v1.0.0\nKEY=v2.0.0\n", "", true},
		{"invalid key", "bad-name=v1.0.0\n", "", true},
		{"empty value", "KEY=\n", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output, body, err := runPrepareExport(t, script, tc.input)
			assertPrepareExport(t, tc, output, body, err)
		})
	}
}

func prepareExportScript(t *testing.T) string {
	t.Helper()

	var action exportAction
	if err := yaml.Unmarshal(readRepoFile(t, ".github/actions/prepare/action.yml"), &action); err != nil {
		t.Fatal(err)
	}

	if _, err := exec.LookPath(workflowBash); err != nil {
		t.Fatalf("required Bash workflow-control prerequisite unavailable: %v", err)
	}

	for _, step := range action.Runs.Steps {
		if step.Name != "Export pinned tool versions" {
			continue
		}

		if step.Shell != workflowBash || step.Run == "" {
			t.Fatal("actual environment export needs nonempty Bash step")
		}

		return step.Run
	}

	t.Fatal("actual environment export step not found")

	return ""
}

func runPrepareExport(t *testing.T, script, input string) ([]byte, []byte, error) {
	t.Helper()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tools"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, toolVersionsPath), []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "github-env")
	if err := os.WriteFile(target, []byte(existingEnvironment), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(t.Context(), workflowBash, "-e", "-o", "pipefail", "-c", script)
	command.Dir = root

	command.Env = append(os.Environ(), "GITHUB_ENV="+target)
	output, err := command.CombinedOutput()

	directory, openErr := os.OpenRoot(root)
	if openErr != nil {
		t.Fatal(openErr)
	}

	t.Cleanup(func() {
		if closeErr := directory.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	body, readErr := directory.ReadFile("github-env")
	if readErr != nil {
		t.Fatal(readErr)
	}

	if err != nil {
		err = fmt.Errorf("execute actual prepare export: %w", err)
	}

	return output, body, err
}

func assertPrepareExport(t *testing.T, tc exportCase, output, body []byte, err error) {
	t.Helper()

	if tc.reject {
		if err == nil || string(body) != existingEnvironment {
			t.Fatalf("invalid pins accepted or partial env appended: error=%v output=%q environment=%q", err, output, body)
		}

		return
	}

	if err != nil {
		t.Fatalf("actual export step failed: %v: %s", err, output)
	}

	exported := strings.TrimPrefix(string(body), existingEnvironment)
	if tc.want != "" && exported != tc.want {
		t.Fatalf("canonical export %q, want %q", exported, tc.want)
	}

	for line := range strings.SplitSeq(strings.TrimSuffix(exported, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			t.Fatalf("GitHub environment protocol invalid: %q", line)
		}
	}
}
