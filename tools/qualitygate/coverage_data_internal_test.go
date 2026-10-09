// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExecutableCoverageIncludesEveryRealChildAndRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binary := buildCoverageBranchFixture(t, root)

	data := filepath.Join(root, "data")
	first, second := filepath.Join(data, "first"), filepath.Join(data, "second")

	for _, directory := range []string{first, second} {
		if err := os.MkdirAll(directory, dirMode); err != nil {
			t.Fatal(err)
		}
	}

	runCoverageBranchFixture(t, root, binary, first, []string{"choose-first"})
	runCoverageBranchFixture(t, root, binary, second, nil)

	inputs, inputErr := executableCoverageInputs(data)
	if inputErr != nil || !slices.Equal(inputs, []string{first, second}) {
		t.Fatalf("real child coverage omitted: %v/%v", inputs, inputErr)
	}

	profile := filepath.Join(root, "children.out")

	args := []string{goToolVerb, covdataVerb, "textfmt", "-i=" + strings.Join(inputs, ","), "-o=" + profile}
	if err := goCommand(root, args...).run(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertCoverageBranches(t, profile)

	runCoverageBranchFixture(t, root, binary, data, nil)

	inputs, inputErr = executableCoverageInputs(data)
	if inputErr != nil || !slices.Equal(inputs, []string{data, first, second}) {
		t.Fatalf("legacy root coverage omitted: %v/%v", inputs, inputErr)
	}

	removeCoverageCounters(t, second)

	if incomplete, err := executableCoverageInputs(data); err == nil {
		t.Fatalf("incomplete child hidden by complete root and sibling: %v", incomplete)
	}
}

func removeCoverageCounters(t *testing.T, directory string) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "covcounters.") {
			if removeErr := os.Remove(filepath.Join(directory, entry.Name())); removeErr != nil {
				t.Fatal(removeErr)
			}
		}
	}
}

func assertCoverageBranches(t *testing.T, path string) {
	t.Helper()

	profile, err := readProfile(path)
	if err != nil {
		t.Fatal(err)
	}

	covered := map[int]bool{}

	for _, block := range profile.Blocks {
		if block.Count > 0 {
			covered[block.StartLine] = true
		}
	}

	if !covered[5] || !covered[7] {
		t.Fatalf("independent first and second branches not both covered: %+v", profile.Blocks)
	}
}

func TestExecutableCoverageRejectsEmptyAndUnexpectedInputs(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"empty-root", "empty-child", "nested-child", "unknown-file"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			switch name {
			case "empty-child":
				if err := os.Mkdir(filepath.Join(root, "child"), dirMode); err != nil {
					t.Fatal(err)
				}
			case "nested-child":
				if err := os.MkdirAll(filepath.Join(root, "child", "nested"), dirMode); err != nil {
					t.Fatal(err)
				}
			case "unknown-file":
				writeForeignGraphFixture(t, filepath.Join(root, "unknown"), "not coverage")
			default:
			}

			if inputs, err := executableCoverageInputs(root); err == nil {
				t.Fatalf("invalid executable coverage accepted: %s/%v", name, inputs)
			}
		})
	}
}

func buildCoverageBranchFixture(t *testing.T, root string) string {
	t.Helper()

	source := `package main
import "os"
func main() {
if len(os.Args)>1 {
println("first branch")
} else {
println("second branch")
}
}
`

	writeForeignGraphFixture(t, filepath.Join(root, moduleFileName), "module coverage.fixture\ngo 1.27.2\n")
	writeForeignGraphFixture(t, filepath.Join(root, "branches.go"), source)

	binary := filepath.Join(root, "coverage-fixture.exe")
	if err := goCommand(root, "build", "-cover", "-o", binary, ".").run(t.Context()); err != nil {
		t.Fatal(err)
	}

	return binary
}

func runCoverageBranchFixture(t *testing.T, root, binary, directory string, args []string) {
	t.Helper()

	run := &command{dir: root, name: binary, args: args, env: []string{"GOCOVERDIR=" + directory}}
	if err := run.run(t.Context()); err != nil {
		t.Fatal(err)
	}
}
