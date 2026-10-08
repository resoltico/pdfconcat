// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"debug/buildinfo"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const linkerVersionFixture = "1.2.3"

func TestReleaseLinkerVersionRequiresAConsumedAssignment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flags, want string
		valid       bool
	}{
		{"-s -w -X main.version=1.2.3", linkerVersionFixture, true},
		{"-X 'main.version=quoted version'", "quoted version", true},
		{`-X "main.version=double quoted"`, "double quoted", true},
		{"-X=main.version=1.2.3", linkerVersionFixture, true},
		{"-X main.version=wrong -X main.version=1.2.3", linkerVersionFixture, true},
		{"-X main.version=1.2.3 -X main.version=wrong", "wrong", true},
		{"main.version=1.2.3", "", false},
		{"-X wrong.version=1.2.3", "", false},
		{"'-X main.version=1.2.3'", "", false},
		{"-X main.version=", "", false},
		{"-X main.version=1.2.3 'unfinished", "", false},
		{"-X", "", false},
		{"-X malformed", "", false},
		{"-- -X main.version=1.2.3", "", false},
		{"-extldflags -X main.version=1.2.3", "", false},
	}
	for _, tc := range cases {
		got, valid := releaseLinkerVersion(tc.flags)
		if got != tc.want || valid != tc.valid {
			t.Fatalf("flags=%q version=%q valid=%t want=%q/%t", tc.flags, got, valid, tc.want, tc.valid)
		}
	}
}

func TestRealReleaseBinaryVersionAgreesWithInspectableLinkerMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	files := map[string]string{
		"go.mod":  "module example.org/archiveversion\n",
		"main.go": "package main\nimport \"fmt\"\nvar version string\nfunc main(){fmt.Print(version)}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), fileMode); err != nil {
			t.Fatal(err)
		}
	}

	executable := filepath.Join(dir, executableName(runtime.GOOS))

	build := command{
		dir:  dir,
		name: goTool,
		args: []string{goBuildVerb, "-ldflags", "-s -w -X main.version=wrong -X main.version=1.2.3", "-o", executable, "."},
	}
	if err := build.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	recorded := binaryLinkerFlags(t, executable)

	got, valid := releaseLinkerVersion(recorded)

	printed, err := (&command{dir: dir, name: executable}).output(t.Context())
	if err != nil || !valid || got != linkerVersionFixture || printed != got {
		t.Fatalf("actual version=%q metadata=%q parsed=%q/%t error=%v", printed, recorded, got, valid, err)
	}

	if problems := releaseVersionProblems(executable, printed, recorded); len(problems) != 0 {
		t.Fatalf("actual correct archive version rejected: %v", problems)
	}

	if problems := releaseVersionProblems(executable, "wrong", recorded); len(problems) == 0 {
		t.Fatal("actual wrong archive version accepted")
	}
}

func binaryLinkerFlags(t *testing.T, filename string) string {
	t.Helper()

	info, err := buildinfo.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	var recorded string

	for _, setting := range info.Settings {
		if setting.Key == "-ldflags" {
			recorded = setting.Value
		}
	}

	return recorded
}
