// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func preparedToolsFixture(t *testing.T) (string, preparedMutationTools) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".tools", "bin")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gremlins", lintTool} {
		if err := os.WriteFile(filepath.Join(dir, preparedToolFilename(name)), []byte(name), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	authority := preparedMutationTools{SourceSHA: strings.Repeat("a", 40), RunID: "123", PatchSHA: strings.Repeat("b", 64), GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	receipt, err := hashPreparedTools(root, authority)
	if err != nil {
		t.Fatal(err)
	}
	return root, receipt
}

func TestPreparedMutationToolsRejectUntrustedIdentityAndBytes(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func(*preparedMutationTools){
		"source":        func(r *preparedMutationTools) { r.SourceSHA = strings.Repeat("c", 40) },
		"run":           func(r *preparedMutationTools) { r.RunID = "124" },
		"pin":           func(r *preparedMutationTools) { r.PatchSHA = strings.Repeat("c", 64) },
		"Go":            func(r *preparedMutationTools) { r.GoVersion = "go0.0.0" },
		"OS":            func(r *preparedMutationTools) { r.GOOS = "foreign" },
		"architecture":  func(r *preparedMutationTools) { r.GOARCH = "foreign" },
		"partial":       func(r *preparedMutationTools) { r.LinterSHA = "" },
		"SDK bytes":     func(r *preparedMutationTools) { r.GremlinsSHA = strings.Repeat("0", 64) },
		"checker bytes": func(r *preparedMutationTools) { r.LinterSHA = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			root, receipt := preparedToolsFixture(t)
			authority := receipt
			authority.GremlinsSHA, authority.LinterSHA = "", ""
			edit(&receipt)
			if err := verifyPreparedTools(root, receipt, authority); err == nil {
				t.Fatal("untrusted prepared tools accepted")
			}
			if runtime.GOOS != windowsOS {
				info, err := os.Stat(filepath.Join(root, ".tools", "bin", preparedToolFilename("gremlins")))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm()&0o111 != 0 {
					t.Fatal("failed verification enabled executable before rejecting")
				}
			}
		})
	}
	root, receipt := preparedToolsFixture(t)
	authority := receipt
	authority.GremlinsSHA, authority.LinterSHA = "", ""
	if err := verifyPreparedTools(root, receipt, authority); err != nil {
		t.Fatalf("matching prepared bytes rejected: %v", err)
	}
}

func TestPreparedMutationToolsRejectMissingExtraAndIrregularFiles(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"missing", "directory", "symlink", "extra", "parent symlink", "bin symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, receipt := preparedToolsFixture(t)
			authority := receipt
			authority.GremlinsSHA, authority.LinterSHA = "", ""
			file := filepath.Join(root, ".tools", "bin", preparedToolFilename("gremlins"))
			switch kind {
			case "extra":
				if err := os.WriteFile(filepath.Join(root, ".tools", "bin", "unexpected"), nil, fileMode); err != nil {
					t.Fatal(err)
				}
			case "parent symlink", "bin symlink":
				tools, moved := filepath.Join(root, ".tools"), filepath.Join(root, "moved")
				if kind == "bin symlink" {
					tools = filepath.Join(tools, "bin")
				}
				if err := os.Rename(tools, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, tools); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "directory":
					if err := os.Mkdir(file, dirMode); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(filepath.Join(root, ".tools", "bin", preparedToolFilename(lintTool)), file); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := verifyPreparedTools(root, receipt, authority); err == nil {
				t.Fatalf("%s prepared tool accepted", kind)
			}
		})
	}
}

func TestPreparedToolsBadHashStopsRealExecutableLaunch(t *testing.T) {
	t.Parallel()
	root, receipt := preparedToolsFixture(t)
	dir := snapshotFixture(t, map[string]string{"go.mod": "module marker\n", "main.go": "package main;import(\"os\");func main(){if e:=os.WriteFile(os.Args[1],[]byte(\"executed\"),0600);e!=nil{panic(e)}}"})
	binary := filepath.Join(root, ".tools", "bin", preparedToolFilename("gremlins"))
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if _, err := (&command{dir: dir, name: goTool, args: []string{"build", "-buildvcs=false", "-o", binary, "."}}).output(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt, err := hashPreparedTools(root, receipt)
	if err != nil {
		t.Fatal(err)
	}
	authority := receipt
	authority.GremlinsSHA, authority.LinterSHA = "", ""
	marker := filepath.Join(root, "executed")
	launch := func(r preparedMutationTools) error {
		if err := verifyPreparedTools(root, r, authority); err != nil {
			return err
		}
		return (&command{name: binary, args: []string{marker}}).run(t.Context())
	}
	bad := receipt
	bad.GremlinsSHA = strings.Repeat("0", 64)
	if err := launch(bad); err == nil {
		t.Fatal("changed payload digest reached launch")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unverified executable ran: %v", err)
	}
	if err := launch(receipt); err != nil {
		t.Fatal(err)
	}
	data, err := readInRoot(root, "executed")
	if err != nil || string(data) != "executed" {
		t.Fatalf("real positive launch failed: %q %v", data, err)
	}
}

func TestPreparedToolsWorkflowAuthorityAndRawReceipt(t *testing.T) {
	root, receipt := preparedToolsFixture(t)
	repository, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	versions, err := readInRoot(repository, toolVersionsFileName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools"), dirMode); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{registryName: []byte("version: 1\n"), toolVersionsFileName: versions, ".gitignore": []byte(".tools/\n")} {
		if err := os.WriteFile(filepath.Join(root, name), data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", registryName, toolVersionsFileName, ".gitignore"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		if err := (&command{dir: root, name: gitTool, args: args}).run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	head, err := (&command{dir: root, name: gitTool, args: []string{"rev-parse", "HEAD"}}).output(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_SHA", strings.TrimSpace(head))
	t.Setenv("GITHUB_RUN_ID", "123")
	authority, err := preparedToolsAuthority(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	receipt.SourceSHA, receipt.PatchSHA = authority.SourceSHA, authority.PatchSHA
	t.Chdir(root)
	for _, raw := range []string{"", "{}", `{"unexpected":true}`} {
		t.Setenv("PDFCONCAT_MUTATION_TOOLS", raw)
		if err := runPreparedMutationTools(t.Context(), nil, false); err == nil {
			t.Fatalf("partial raw receipt %q accepted", raw)
		}
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PDFCONCAT_MUTATION_TOOLS", string(raw))
	if err := runPreparedMutationTools(t.Context(), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := runPreparedMutationTools(t.Context(), nil, true); err == nil {
		t.Fatal("recording blessed non-Go fixture bytes")
	}
	t.Setenv("GITHUB_SHA", strings.Repeat("0", 40))
	if _, err := preparedToolsAuthority(context.Background(), root); err == nil {
		t.Fatal("wrong source accepted")
	}
	t.Setenv("GITHUB_SHA", strings.TrimSpace(head))
	t.Setenv("GITHUB_RUN_ID", "")
	if _, err := preparedToolsAuthority(t.Context(), root); err == nil {
		t.Fatal("missing run accepted")
	}
	t.Setenv("GITHUB_RUN_ID", "123")
	if err := os.WriteFile(filepath.Join(root, toolVersionsFileName), append(versions, '\n'), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := preparedToolsAuthority(t.Context(), root); err == nil {
		t.Fatal("dirty source accepted")
	}
}
