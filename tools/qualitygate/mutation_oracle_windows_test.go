//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsMutationOraclePathSurvivesBashAndGoChildren(t *testing.T) {
	if os.Getenv("PDFCONCAT_TEST_QA_PATH_CHILD") == "1" {
		runOracleLookupChild(t)
		return
	}

	goBinary, err := exec.LookPath(goTool)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PDFCONCAT_TEST_QA_GO", goBinary)

	root := t.TempDir()
	for _, name := range []string{"qpdf", "poppler", "bundled"} {
		if err := os.Mkdir(filepath.Join(root, name), dirMode); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"qpdf", "pdftotext", "pdfinfo", "pdftoppm"} {
		verified := "poppler"
		if name == "qpdf" {
			verified = "qpdf"
		}
		for _, directory := range []string{verified, "bundled"} {
			if err := os.WriteFile(filepath.Join(root, directory, name+exeSuffix), nil, fileMode); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PDFCONCAT_QPDF_BIN", filepath.Join(root, "qpdf"))
	t.Setenv("PDFCONCAT_POPPLER_BIN", filepath.Join(root, "poppler"))
	t.Setenv("PDFCONCAT_TEST_QA_ROOT", root)
	t.Setenv("PDFCONCAT_TEST_QA_PATH_CHILD", "1")

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PDFCONCAT_TEST_QA_BINARY", binary)

	child := exec.CommandContext(t.Context(), "bash", "-c", `export PATH="$(cygpath -u "$PDFCONCAT_TEST_QA_ROOT")/bundled:$PATH"; "$PDFCONCAT_TEST_QA_BINARY" -test.run '^TestWindowsMutationOraclePathSurvivesBashAndGoChildren$'`)
	if output, runErr := child.CombinedOutput(); runErr != nil {
		t.Fatalf("Bash to mutation Go child oracle lookup: %v\n%s", runErr, output)
	}
}

func runOracleLookupChild(t *testing.T) {
	t.Helper()

	for _, name := range []string{"qpdf", "pdftotext", "pdfinfo", "pdftoppm"} {
		found, err := exec.LookPath(name)
		if err != nil || filepath.Dir(found) != filepath.Join(os.Getenv("PDFCONCAT_TEST_QA_ROOT"), "bundled") {
			t.Fatalf("Bash shadow prerequisite: %s %q %v", name, found, err)
		}
	}
	env, err := mutationCheckerEnvironment(filepath.Join(os.Getenv("PDFCONCAT_TEST_QA_ROOT"), "checker.exe"))
	if err != nil {
		t.Fatal(err)
	}
	source := `package main
import("os";"os/exec";"path/filepath";"fmt")
func main(){for _,n:=range []string{"qpdf","pdftotext","pdfinfo","pdftoppm"}{p,e:=exec.LookPath(n); d:=os.Getenv("PDFCONCAT_POPPLER_BIN");if n=="qpdf"{d=os.Getenv("PDFCONCAT_QPDF_BIN")};if e!=nil||filepath.Clean(filepath.Dir(p))!=filepath.Clean(d){panic(fmt.Sprintf("wrong oracle %s: %s %v",n,p,e))}};fmt.Print("all four verified oracle paths")}`
	root, err := os.OpenRoot(os.Getenv("PDFCONCAT_TEST_QA_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if err := root.WriteFile("lookup.go", []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(os.Getenv("PDFCONCAT_TEST_QA_ROOT"), "lookup.go")
	child := &command{dir: filepath.Dir(file), name: os.Getenv("PDFCONCAT_TEST_QA_GO"), args: []string{runVerb, file}, env: env}
	if output, runErr := child.output(t.Context()); runErr != nil || !strings.Contains(output, "all four verified oracle paths") {
		t.Fatalf("SDK-style direct Go child did not inherit verified oracle paths: %q %v", output, runErr)
	}
}

func TestWindowsMutationOracleDirectoriesRejectPartialAndMissingPair(t *testing.T) {
	t.Setenv("PDFCONCAT_QPDF_BIN", "")
	t.Setenv("PDFCONCAT_POPPLER_BIN", "")
	original := os.Getenv("PATH")
	binary := filepath.Join(t.TempDir(), "checker.exe")
	if env, err := mutationCheckerEnvironment(binary); err != nil || env[1] != "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+original {
		t.Fatalf("manual PATH contract changed: %v %v", env, err)
	}
	t.Setenv("PDFCONCAT_QPDF_BIN", t.TempDir())
	if _, err := mutationCheckerEnvironment(binary); err == nil {
		t.Fatal("partial oracle directory pair accepted")
	}
	t.Setenv("PDFCONCAT_POPPLER_BIN", filepath.Join(t.TempDir(), "missing"))
	if _, err := mutationCheckerEnvironment(binary); err == nil {
		t.Fatal("missing oracle directory accepted")
	}
}
