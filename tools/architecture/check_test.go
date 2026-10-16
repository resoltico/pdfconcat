// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package architecture_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/tools/architecture"
)

func sourceCopy(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") && d.Name() != "go.mod" && d.Name() != ".golangci.yml" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dest
}
func write(t *testing.T, root, path, text string) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestActualPolicyEdges(t *testing.T) {
	cases := []struct {
		path, dependency string
		reject           bool
	}{
		{"cmd/pdfconcat/probe.go", "github.com/resoltico/pdfconcat/internal/cli", false},
		{"cmd/pdfconcat/probe.go", "github.com/resoltico/pdfconcat/internal/core", true},
		{"internal/cli/probe.go", "github.com/resoltico/pdfconcat/internal/core", false},
		{"internal/cli/probe.go", "os/exec", true}, {"internal/cli/probe.go", "net/http", true},
		{"internal/core/probe.go", "github.com/resoltico/pdfconcat/internal/cli", true},
		{"internal/pdf/probe.go", "github.com/resoltico/pdfconcat/internal/publication", true},
		{"internal/job/probe.go", "github.com/resoltico/pdfconcat/internal/pdf", true},
		{"internal/oracle/probe.go", "github.com/resoltico/pdfconcat/internal/pdf", true},
		{"internal/oracle/probe.go", "github.com/resoltico/pdfconcat/internal/core", true},
		{"internal/oracle/probe.go", "github.com/resoltico/pdfconcat/internal/job", true},
		{"internal/oracle/probe.go", "github.com/resoltico/pdfconcat/internal/typeset", true},
		{"internal/core/probe.go", "github.com/resoltico/pdfconcat/internal/oracle", true},
		{"internal/core/probe_test.go", "github.com/resoltico/pdfconcat/internal/pdffixture", false},
	}
	for _, dep := range []string{"job", "plan", "pdf", "typeset", "result", "diag", "publication"} {
		cases = append(cases, struct {
			path, dependency string
			reject           bool
		}{"internal/cli/probe.go", "github.com/resoltico/pdfconcat/internal/" + dep, true})
	}
	for _, name := range []string{"probe_windows.go", "probe_arm64.go", ".hidden.go", "_ignored.go", "probe_generated.go"} {
		cases = append(cases, struct {
			path, dependency string
			reject           bool
		}{"internal/cli/" + name, "os/exec", true}, struct {
			path, dependency string
			reject           bool
		}{"internal/cli/" + name, "context", false})
	}
	for _, tc := range cases {
		t.Run(tc.path+"/"+tc.dependency, func(t *testing.T) {
			root := sourceCopy(t)
			write(t, root, tc.path, "//go:build custom_inactive\n\npackage probe\nimport _ "+`"`+tc.dependency+`"`+"\n")
			_, err := architecture.Check(root)
			if (err != nil) != tc.reject {
				t.Fatalf("reject=%v want=%v: %v", err != nil, tc.reject, err)
			}
			if tc.reject && !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("did not test actual import rule: %v", err)
			}
		})
	}
}
func TestScopeFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"child owner", "internal/cli/child/probe.go", "package child"},
		{"second command", "cmd/gui/probe.go", "package main"},
		{"hidden package", ".hidden/probe.go", "package hidden"},
		{"testdata package", "internal/cli/testdata/probe.go", "package sample"},
		{"nested module", "nested/go.mod", "module unexpected"},
		{"workspace", "go.work", "go 1.27.2"},
		{"inactive syntax", "internal/cli/probe.go", "//go:build never\n\npackage cli\nfunc broken(){ invalid + }"},
		{"config syntax", ".golangci.yml", "linters: ["},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := sourceCopy(t)
			write(t, root, tc.path, tc.body)
			if _, err := architecture.Check(root); err == nil {
				t.Fatal("accepted invalid scope")
			}
		})
	}
	for _, change := range []struct{ from, to string }{
		{`list-mode: strict`, `list-mode: lax`},
		{`files: ["**/internal/cli/*.go", "!$test"]`, `files: ["**/internal/cli/*.go", "!$test", "!**/probe.go"]`},
		{`**/internal/cli/*.go`, `**/internal/cli/**`},
		{`layer-internal-cli:`, `layer-internal-cli-copy:\n          list-mode: strict\n          files: ["**/internal/cli/*.go", "!$test"]\n          allow: ["$gostd"]\n        layer-internal-cli:`},
		{`**/internal/cli/*.go`, `**/internal/absent/*.go`},
	} {
		t.Run(change.to, func(t *testing.T) {
			root := sourceCopy(t)
			p := filepath.Join(root, ".golangci.yml")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			write(t, root, ".golangci.yml", strings.ReplaceAll(string(data), change.from, strings.ReplaceAll(change.to, `\n`, "\n")))
			if _, err = architecture.Check(root); err == nil {
				t.Fatal("accepted weakened owner")
			}
		})
	}
	for _, directory := range []bool{false, true} {
		t.Run("source link", func(t *testing.T) {
			root := sourceCopy(t)
			target := filepath.Join(root, "internal/cli/cli.go")
			link := filepath.Join(root, "internal/cli/linked.go")
			if directory {
				target = filepath.Dir(target)
				link = filepath.Join(root, "linked")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skip(err)
			}
			if _, err := architecture.Check(root); err == nil {
				t.Fatal("accepted link")
			}
		})
	}
}
func TestProtectiveRuleControlDetectsWeakening(t *testing.T) {
	root := sourceCopy(t)
	write(t, root, "internal/cli/probe.go", "package cli\nimport _ \"github.com/resoltico/pdfconcat/internal/job\"")
	_, err := architecture.Check(root)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("real rule failed rejection control: %v", err)
	}
	path := filepath.Join(root, ".golangci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Keep ownership intact but weaken the real CLI rule's allow list.
	text := strings.Replace(string(data), "- github.com/resoltico/pdfconcat/internal/core$", "- github.com/resoltico/pdfconcat/internal/core$\n            - github.com/resoltico/pdfconcat/internal/job$", 1)
	write(t, root, ".golangci.yml", text)
	if _, err = architecture.Check(root); err != nil {
		t.Fatalf("negative control did not expose weakened rule: %v", err)
	}
}

func TestOwnerDirectoriesMustBeCanonicalModuleRelative(t *testing.T) {
	for _, dir := range []string{".", "..", "./internal/cli", "internal/./cli", "internal/../cli", "internal//cli", "internal/cli/", "/internal/cli", "C:/internal/cli", "C:internal/cli", `\\server\share\cli`} {
		t.Run(dir, func(t *testing.T) {
			root := sourceCopy(t)
			// A dot owner previously counted this root file without applying its layer:
			// depguard sees the canonical filename, which contains no /./ component.
			write(t, root, "owner_probe.go", "//go:build owner_inactive\n\npackage probe\nimport _ \"github.com/resoltico/pdfconcat/internal/cli\"\n")
			path := filepath.Join(root, ".golangci.yml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			owner := "        layer-owner-probe:\n          list-mode: strict\n          files: [\"**/" + strings.ReplaceAll(dir, `\`, `\\`) + "/*.go\", \"!$test\"]\n          allow: [\"$gostd\"]\n"
			write(t, root, ".golangci.yml", strings.Replace(string(data), "\n  exclusions:", "\n"+owner+"\n  exclusions:", 1))
			if _, err = architecture.Check(root); err == nil || !strings.Contains(err.Error(), "owner directory") {
				t.Fatalf("noncanonical owner must be rejected before discovery/analyzer: %v", err)
			}
		})
	}
}
