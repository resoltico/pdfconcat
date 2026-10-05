// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// binary builds the executable once and returns its path.
var binary = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "pdfconcat-e2e-")
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "pdfconcat")
	if exe := ".exe"; os.PathSeparator == '\\' {
		path += exe
	}

	output, err := exec.Command("go", "build", "-o", path, ".").CombinedOutput()
	if err != nil {
		return "", errors.Join(err, errors.New(string(output)))
	}

	return path, nil
})

type result struct {
	stdout, stderr string
	code           int
}

func runCLI(t *testing.T, stdin string, args ...string) result {
	t.Helper()

	path, err := binary()
	if err != nil {
		t.Fatalf("build pdfconcat: %v", err)
	}

	command := exec.Command(path, args...)
	command.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	err = command.Run()

	code := 0

	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run pdfconcat: %v", err)
	}

	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func TestMain(m *testing.M) {
	m.Run() // The runner exits with its status once TestMain returns.

	path, err := binary()
	if err == nil {
		_ = os.RemoveAll(filepath.Dir(path))
	}
}

func TestInformationalCommandsSucceed(t *testing.T) {
	t.Parallel()

	for flag, want := range map[string]string{"--help": "Usage:", "--version": "pdfconcat dev", "--print-schema": "\"$schema\""} {
		got := runCLI(t, "", flag)
		if got.code != 0 || !strings.Contains(got.stdout, want) || got.stderr != "" {
			t.Errorf("%s = %+v, want status 0 and %q on stdout", flag, got, want)
		}
	}

	var schema map[string]any

	err := json.Unmarshal([]byte(runCLI(t, "", "--print-schema").stdout), &schema)
	if err != nil {
		t.Errorf("--print-schema is not JSON: %v", err)
	}
}

func TestUsageErrorsExitWithStatusTwoAndAHint(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{}, {"a.pdf"}, {"-o", "x.pdf", "--bogus", "a.pdf"}, {"-o", "--blank", "a.pdf"}} {
		got := runCLI(t, "", args...)
		if got.code != 2 || !strings.Contains(got.stderr, "pdfconcat --help") || got.stdout != "" {
			t.Errorf("%q = %+v, want status 2 with a help hint on stderr", args, got)
		}
	}
}

func TestOperationalFailuresExitWithStatusOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got := runCLI(t, "", "-o", filepath.Join(dir, "out.pdf"), filepath.Join(dir, "missing.pdf"))

	if got.code != 1 || !strings.Contains(got.stderr, "missing.pdf") {
		t.Errorf("result = %+v, want status 1 naming the missing file", got)
	}
}

func TestAssemblesFromCommandLineAndPlan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.pdf"), filepath.Join(dir, "second.pdf")
	pdffixture.Write(t, first, 2, pdffixture.A4())
	pdffixture.Write(t, second, 3, pdffixture.A4())

	direct := filepath.Join(dir, "direct.pdf")

	got := runCLI(t, "", "-o", direct, "--blank-text", "Left blank", first, "--blank", second)
	if got.code != 0 || !strings.Contains(got.stdout, "6 pages") {
		t.Fatalf("direct run = %+v", got)
	}

	planned := filepath.Join(dir, "planned.pdf")

	plan, err := json.Marshal(map[string]any{
		"version": 1,
		"items":   []any{first, map[string]any{"blank": map[string]any{"text": map[string]any{"value": "Left blank"}}}, second},
	})
	if err != nil {
		t.Fatal(err)
	}

	got = runCLI(t, string(plan), "--plan", "-", "-o", planned, "--json")
	if got.code != 0 {
		t.Fatalf("plan run = %+v", got)
	}

	var summary struct {
		Pages int `json:"pages"`
	}

	err = json.Unmarshal([]byte(got.stdout), &summary)
	if err != nil || summary.Pages != 6 {
		t.Fatalf("plan run report = %q (%v), want 6 pages", got.stdout, err)
	}
}
