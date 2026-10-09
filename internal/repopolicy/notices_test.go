// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const ownModule = "github.com/resoltico/pdfconcat"

// goroot asks the Go toolchain where its standard library, and its LICENSE, live.
func goroot(t *testing.T) string {
	t.Helper()

	output, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}

	return strings.TrimSpace(string(output))
}

// TestNoticesMatchLinkedModules checks the real notices against the real release dependency graph:
// every linked module has a row at the version go.mod selects, and the license texts shipped under
// third_party reproduce the license files of the module versions that are linked.
func TestNoticesMatchLinkedModules(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	modules, err := repopolicy.LinkedModules(context.Background(), root, ownModule)
	if err != nil {
		t.Fatal(err)
	}

	if len(modules) == 0 {
		t.Fatal("no third-party modules found; the go list invocation has drifted")
	}

	problems, err := repopolicy.NoticeIssues(root, goroot(t), modules)
	if err != nil {
		t.Fatal(err)
	}

	if len(problems) > 0 {
		t.Fatalf("THIRD_PARTY_NOTICES.md does not match the linked modules:\n%s", strings.Join(problems, "\n"))
	}
}

// noticesFixture builds a repository root with one linked module and a Go toolchain directory.
func noticesFixture(t *testing.T, mutate func(files map[string]string)) (string, string, []repopolicy.LinkedModule) {
	t.Helper()

	base := t.TempDir()
	moduleDir := filepath.Join(base, "mod")
	gorootDir := filepath.Join(base, "goroot")

	files := map[string]string{
		"root/go.mod":                       "module example.test/app\n\ngo 1.27.1\n",
		"root/third_party/licenses/BSD.txt": "Copyright The Go Authors.\nRedistribution and use.\n",
		"root/third_party/licenses/MIT.txt": "MIT License\nCopyright Example\n",
		"mod/LICENSE":                       "MIT License\r\nCopyright Example  \r\n\r\n",
		"goroot/LICENSE":                    "Copyright The Go Authors.\nRedistribution and use.\n",
		"root/THIRD_PARTY_NOTICES.md": noticeTable(
			"| `example.test/dep` | v1.2.3 | MIT | a dependency | `third_party/licenses/MIT.txt` |",
		),
	}

	mutate(files)

	for name, content := range files {
		full := filepath.Join(base, filepath.FromSlash(name))

		err := os.MkdirAll(filepath.Dir(full), 0o750)
		if err != nil {
			t.Fatal(err)
		}

		err = os.WriteFile(full, []byte(content), 0o600)
		if err != nil {
			t.Fatal(err)
		}
	}

	return filepath.Join(base, "root"), gorootDir, []repopolicy.LinkedModule{{Path: "example.test/dep", Version: "v1.2.3", Dir: moduleDir}}
}

func noticeTable(rows ...string) string {
	return "| Component | Version | License | Purpose | License copy |\n| --- | --- | --- | --- | --- |\n" +
		"| Go standard library | Go 1.27.1 toolchain | BSD | runtime | `third_party/licenses/BSD.txt` |\n" +
		strings.Join(rows, "\n") + "\n"
}

func TestNoticeIssuesAcceptsMatchingFixture(t *testing.T) {
	t.Parallel()

	root, gorootDir, modules := noticesFixture(t, func(map[string]string) {})

	problems, err := repopolicy.NoticeIssues(root, gorootDir, modules)
	if err != nil {
		t.Fatal(err)
	}

	if len(problems) != 0 {
		t.Fatalf(unexpectedProblemsFormat, problems)
	}
}

func TestLinkedModuleNoticesFollowActualLocalReplacement(t *testing.T) {
	t.Parallel()
	root, gorootDir, _ := noticesFixture(t, func(files map[string]string) {
		files["root/go.mod"] += "\nrequire example.test/dep v1.2.3\nreplace example.test/dep => ../mod\n"
		files["root/cmd/pdfconcat/main.go"] = "package main\nimport \"example.test/dep\"\nfunc main(){dep.Value()}\n"
		files["mod/go.mod"] = "module example.test/dep\n\ngo 1.27.1\n"
		files["mod/value.go"] = "package dep\nfunc Value(){}\n"
	})

	modules, err := repopolicy.LinkedModules(t.Context(), root, "example.test/app")
	if err != nil {
		t.Fatal(err)
	}

	if len(modules) != 1 || modules[0].Dir != filepath.Join(filepath.Dir(root), "mod") {
		t.Fatalf("license lookup did not follow effective source: %+v", modules)
	}

	problems, err := repopolicy.NoticeIssues(root, gorootDir, modules)
	if err != nil || len(problems) != 0 {
		t.Fatalf("actual replacement license refused: %v %v", problems, err)
	}

	if writeErr := os.WriteFile(filepath.Join(modules[0].Dir, "LICENSE"), []byte("changed local license"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	problems, err = repopolicy.NoticeIssues(root, gorootDir, modules)
	if err != nil || len(problems) == 0 {
		t.Fatal("changed actual local-source license was not detected")
	}
}

// TestNoticeIssuesRejectsModuleDrift is a negative control: a new dependency, a stale row and a
// version change are each reported.
func TestNoticeIssuesRejectsModuleDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		modules func(modules []repopolicy.LinkedModule) []repopolicy.LinkedModule
		name    string
		want    string
	}{
		{
			func(modules []repopolicy.LinkedModule) []repopolicy.LinkedModule {
				return append(modules, repopolicy.LinkedModule{Path: "example.test/font", Version: "v0.1.0", Dir: modules[0].Dir})
			},
			"new module without row", "example.test/font is linked into a release binary but has no row",
		},
		{
			func([]repopolicy.LinkedModule) []repopolicy.LinkedModule { return nil },
			"row without linked module", "lists example.test/dep, which is not linked",
		},
		{
			func(modules []repopolicy.LinkedModule) []repopolicy.LinkedModule {
				modules[0].Version = "v1.3.0"

				return modules
			},
			"version changed", "notices say v1.2.3, go.mod selects v1.3.0",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root, gorootDir, modules := noticesFixture(t, func(map[string]string) {})

			problems, err := repopolicy.NoticeIssues(root, gorootDir, test.modules(modules))
			if err != nil {
				t.Fatal(err)
			}

			requireContains(t, problems, test.want)
		})
	}
}

// TestNoticeIssuesRejectsFileDrift is a negative control: a license that no longer matches, a missing
// copy, an orphan copy and a moved Go version are each reported.
func TestNoticeIssuesRejectsFileDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mutate func(files map[string]string)
		name   string
		want   string
	}{
		{
			func(files map[string]string) { files["mod/LICENSE"] = "MIT License\nCopyright Someone Else\n" },
			"license text changed upstream", "no listed license copy reproduces the module's LICENSE",
		},
		{
			func(files map[string]string) { files["mod/NOTICE"] = "Attribution required.\n" },
			"module ships a second license file", "no listed license copy reproduces the module's NOTICE",
		},
		{func(files map[string]string) {
			delete(files, "mod/LICENSE")

			files["mod/README.md"] = "x"
		}, "module ships no license", "ships no license file"},
		{
			func(files map[string]string) { delete(files, "root/third_party/licenses/MIT.txt") },
			"copy file missing", "license copy third_party/licenses/MIT.txt",
		},
		{
			func(files map[string]string) { files["root/third_party/licenses/Orphan.txt"] = "x" },
			"orphan license copy", "third_party/licenses/Orphan.txt is not referenced",
		},
		{
			func(files map[string]string) { files["root/go.mod"] = "module example.test/app\n\ngo 1.28.0\n" },
			"go version moved", "does not require that Go version",
		},
		{
			func(files map[string]string) { files["goroot/LICENSE"] = "Something else\n" },
			"standard library license changed", "Go standard library: no listed license copy reproduces",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root, gorootDir, modules := noticesFixture(t, test.mutate)

			problems, err := repopolicy.NoticeIssues(root, gorootDir, modules)
			if err != nil {
				t.Fatal(err)
			}

			requireContains(t, problems, test.want)
		})
	}
}
