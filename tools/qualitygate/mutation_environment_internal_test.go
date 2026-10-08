// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMutationSnapshotFindsIgnoredParentCheckerThroughPath(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		t.Fatal(err)
	}

	goBinary, err := exec.LookPath(goTool)
	if err != nil {
		t.Fatal(err)
	}
	// Keep Go available while removing a checker inherited from the invoking shell.
	t.Setenv("PATH", filepath.Dir(goBinary))

	env, err := mutationCheckerEnvironment(binary)
	if err != nil {
		t.Fatal(err)
	}

	scratch := t.TempDir()
	source := "package main\nimport(\"fmt\";\"os/exec\")\n" +
		"func main(){p,e:=exec.LookPath(\"golangci-lint\");if e!=nil{panic(e)};fmt.Print(p)}\n"

	file := filepath.Join(scratch, "checker.go")
	if err = os.WriteFile(file, []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}

	output, err := (&command{dir: scratch, name: goTool, args: []string{runVerb, file}, env: env}).output(t.Context())

	absolute, pathErr := filepath.Abs(binary)
	if err != nil || pathErr != nil || strings.TrimSpace(output) != absolute {
		t.Fatalf("child checker path: %q, %v, %v", output, err, pathErr)
	}

	if _, statErr := os.Stat(filepath.Join(scratch, ".tools")); !os.IsNotExist(statErr) {
		t.Fatalf("ignored tools copied: %v", statErr)
	}

	if _, err = mutationEnvironment(t.Context(), &mutationOptions{root: scratch, integration: true}, nil); err == nil {
		t.Fatal("missing checker prerequisite accepted")
	}
}

func TestPackageMutationDoesNotRequireWholeSuiteChecker(t *testing.T) {
	t.Parallel()

	env, err := mutationEnvironment(t.Context(), &mutationOptions{root: t.TempDir()}, nil)
	if err != nil || !slices.Equal(env, []string{readonlyGoFlags}) {
		t.Fatalf("package mutation environment: %v, %v", env, err)
	}
}
