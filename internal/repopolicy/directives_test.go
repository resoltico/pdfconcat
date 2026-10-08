// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	kindNolint = "nolint"
	kindNosec  = "nosec"
	kindTool   = "tool disable/ignore/skip"
	kindLint   = "lint:ignore or lint:file-ignore"
)

// TestNoProhibitedDirectivesInRepository scans every Go file the repository owns.
func TestNoProhibitedDirectivesInRepository(t *testing.T) {
	t.Parallel()

	violations, err := repopolicy.ScanDirectives(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, violation := range violations {
		t.Error(violation.String())
	}
}

// TestDirectivesRejectEveryMechanism is the negative control: each comment form that switches a tool
// off is found, in production and test files, on its own line and trailing a statement. The
// directive text is assembled from parts so that this file does not contain what it forbids.
func TestDirectivesRejectEveryMechanism(t *testing.T) {
	t.Parallel()

	nolint := "no" + "lint"
	nosec := "no" + "sec"

	cases := []struct {
		name    string
		comment string
		kind    string
	}{
		{"nolint without space", "//" + nolint, kindNolint},
		{"nolint with linter", "//" + nolint + ":gosec // reason", kindNolint},
		{"nolint with space", "// " + nolint + ":all", kindNolint},
		{"nolint upper case", "//" + strings.ToUpper(nolint) + ":errcheck", kindNolint},
		{"nolint block comment", "/* " + nolint + " */", kindNolint},
		{"gosec multiline block", "/* rationale\n#" + nosec + " G304\n*/", kindNosec},
		{"gosec multiline block indented", "/* rationale\n  #" + nosec + " G304\n*/", kindNosec},
		{"gosec marker", "// #" + nosec + " G304", kindNosec},
		{"gosec marker without hash", "// " + nosec, kindNosec},
		{"gosec marker without space", "//#" + nosec, kindNosec},
		{"staticcheck line ignore", "//lint:" + "ignore SA1000 reason", kindLint},
		{"staticcheck file ignore", "//lint:" + "file-ignore SA1000 reason", kindLint},
		{"revive disable", "// revive:" + "disable:exported", kindTool},
		{"revive disable next line", "//revive:" + "disable-next-line", kindTool},
		{"exhaustive ignore", "//exhaustive:" + "ignore", kindTool},
		{"gocritic ignore", "// gocritic:" + "ignore", kindTool},
	}

	for _, test := range cases {
		for _, placement := range []struct{ name, source string }{
			{"own line", "package p\n\n" + test.comment + "\nfunc f() {}\n"},
			{"trailing", "package p\n\nfunc f() {} " + test.comment + "\n"},
		} {
			t.Run(test.name+"/"+placement.name, func(t *testing.T) {
				t.Parallel()

				violations, err := repopolicy.DirectivesIn(directiveSourcePath, []byte(placement.source))
				if err != nil {
					t.Fatal(err)
				}

				if len(violations) != 1 || violations[0].Kind != test.kind || violations[0].Line != 3 {
					t.Fatalf("got %+v, want one %q violation at line 3", violations, test.kind)
				}
			})
		}
	}
}

// TestDirectivesIgnoreLiteralsAndProse proves that text that merely resembles a directive is not one,
// and that the directives Go itself needs stay valid.
func TestDirectivesIgnoreLiteralsAndProse(t *testing.T) {
	t.Parallel()

	nolint := "no" + "lint"
	source := "// SPDX-License-Identifier: MPL-2.0\n" +
		"// Copyright (c) 2026 Ervins\n\n" +
		"//go:build linux || darwin\n\n" +
		"// Package p demonstrates that mentioning //" + nolint + " in prose is not a directive.\n" +
		"package p\n\n" +
		"import _ \"embed\"\n\n" +
		"//go:embed data.txt\n" +
		"var data string\n\n" +
		"//go:generate stringer -type=Kind\n\n" +
		"//line other.go:10\n" +
		"var text = \"//" + nolint + ":gosec\"\n\n" +
		"var raw = `// #" + "nosec G101`\n\n" +
		"// Example usage, indented as documentation code:\n" +
		"//\n" +
		"//\t//" + nolint + ":errcheck\n" +
		"//\t// #" + "nosec\n" +
		"func f() {\n" +
		"\t// Explains that " + nolint + " is rejected and so is #" + "nosec.\n" +
		"}\n"

	violations, err := repopolicy.DirectivesIn(directiveSourcePath, []byte(source))
	if err != nil {
		t.Fatal(err)
	}

	if len(violations) != 0 {
		t.Fatalf("found false positives: %+v", violations)
	}
}

func TestDirectivesReportUnparsableSource(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.DirectivesIn("p/broken.go", []byte("package p\nfunc {"))
	if err == nil {
		t.Fatal("unparsable source was accepted as clean")
	}
}

// TestScanDirectivesWalksHiddenDirectoriesAndTests writes a small tree and requires the walk to find
// directives in hidden directories and _test.go files, to report paths relative to the root, and to
// skip only the version-control directory and ignored build output.
func TestScanDirectivesWalksHiddenDirectoriesAndTests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directive := "//" + "no" + "lint:gosec\n"

	files := map[string]string{
		"pkg/dist/directive.go":       "package pkg\n\n" + directive + "func Nested() {}\n",
		"main.go":                     "package main\n",
		"pkg/code_test.go":            "package pkg\n\n" + directive + "func F() {}\n",
		".hidden/inner/tool.go":       "package inner\n\n" + directive + "func G() {}\n",
		".git/hooks/ignored.go":       "package hooks\n\n" + directive + "func H() {}\n",
		"dist/generated.go":           "package dist\n\n" + directive + "func I() {}\n",
		".tools/cache/module.go":      "package cache\n\n" + directive + "func J() {}\n",
		"docs/example.md":             directive,
		"third_party/vendored/lib.go": "package lib\n\n" + directive + "func K() {}\n",
	}

	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))

		err := os.MkdirAll(filepath.Dir(full), 0o750)
		if err != nil {
			t.Fatal(err)
		}

		err = os.WriteFile(full, []byte(content), 0o600)
		if err != nil {
			t.Fatal(err)
		}
	}

	violations, err := repopolicy.ScanDirectives(root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, violation := range violations {
		got[violation.File] = true
	}

	want := map[string]bool{
		"pkg/code_test.go":            true,
		".hidden/inner/tool.go":       true,
		"third_party/vendored/lib.go": true,
		"pkg/dist/directive.go":       true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for name := range want {
		if !got[name] {
			t.Fatalf("missing %s in %v", name, got)
		}
	}
}

func TestDirectiveViolationReportsPhysicalLineDespiteDisplayMapping(t *testing.T) {
	t.Parallel()

	source := "package p\n//line display.go:900\n//" + "no" + "lint:gosec\nfunc Value() {}\n"

	violations, err := repopolicy.DirectivesIn(directiveSourcePath, []byte(source))
	if err != nil {
		t.Fatal(err)
	}

	if len(violations) != 1 || violations[0].File != directiveSourcePath || violations[0].Line != 3 {
		t.Fatalf("display position replaced the physical directive attribution: %v", violations)
	}
}
