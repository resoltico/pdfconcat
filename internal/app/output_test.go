// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
)

func TestARejectedCommandLineNamesTheCommandItWasAbout(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command string
		args    []string
	}{
		"no command":   {"", []string{"--no-such-option"}},
		"build option": {commandBuild, []string{commandBuild, "--no-such-option"}},
	}

	for name, test := range cases {
		res := execute(t.Context(), t, appOf(newFake(t)), workDir(t), test.args...)
		parsed := res.requireCode(t, 2, "")

		if parsed.Command != test.command {
			t.Errorf("%s: the summary is about %q, want %q", name, parsed.Command, test.command)
		}
	}
}

func TestVersionIsPrintedAsTextOrAsJSON(t *testing.T) {
	t.Parallel()

	build := app.BuildInfo{Version: "1.2.3", Commit: "abcdef", CommitDate: "2026-10-05T09:34:20Z"}
	env := app.Env{WorkingDir: workDir(t), Build: build}

	text := executeWith(t.Context(), t, appOf(newFake(t)), env, commandVersion, flagFormat, formatText)
	want := fmt.Sprintf("pdfconcat 1.2.3\ncommit: abcdef\ndate: 2026-10-05T09:34:20Z\n%s %s/%s\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH)

	if text.code != 0 || text.stdout != want {
		t.Errorf("text version: exit %d, %q", text.code, text.stdout)
	}

	compact := executeWith(t.Context(), t, appOf(newFake(t)), env, commandVersion)

	var info map[string]any

	err := json.Unmarshal([]byte(compact.stdout), &info)
	if err != nil || compact.code != 0 || info["version"] != "1.2.3" || info["commit"] != "abcdef" || info["go"] != runtime.Version() {
		t.Errorf("JSON version: exit %d, %q, %v", compact.code, compact.stdout, err)
	}
}

func TestHelpIsPrintedAsTextOrAsJSON(t *testing.T) {
	t.Parallel()

	runner := appOf(newFake(t))
	env := app.Env{WorkingDir: workDir(t)}

	text := executeWith(t.Context(), t, runner, env, commandHelp, flagFormat, formatText)
	if text.code != 0 || !strings.HasPrefix(text.stdout, "pdfconcat: Assemble PDFs") || strings.HasPrefix(text.stdout, "{") {
		t.Errorf("text help: exit %d, %.80q", text.code, text.stdout)
	}

	compact := executeWith(t.Context(), t, runner, env, commandHelp)
	if compact.code != 0 || !json.Valid([]byte(compact.stdout)) {
		t.Errorf("JSON help: exit %d, %.80q", compact.code, compact.stdout)
	}
}
