// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// linkedModules returns the third-party modules linked into the executable for each release OS.
func linkedModules(t *testing.T, root string) map[string]bool {
	t.Helper()

	linked := map[string]bool{}

	for _, goos := range []string{"darwin", "linux", "windows"} {
		command := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{.Path}}{{end}}", "./cmd/pdfconcat")
		command.Dir = root
		command.Env = append(command.Environ(), "GOOS="+goos)

		output, err := command.Output()
		if err != nil {
			t.Fatalf("go list for %s: %v", goos, err)
		}

		for module := range strings.FieldsSeq(string(output)) {
			if module != "github.com/resoltico/pdfconcat" {
				linked[module] = true
			}
		}
	}

	if len(linked) == 0 {
		t.Fatal("no third-party modules found; the go list invocation has drifted")
	}

	return linked
}

// TestNoticesListExactlyTheLinkedModules fails for a linked module without a notice, and for a notice
// without a linked module.
func TestNoticesListExactlyTheLinkedModules(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	notices := readText(t, root, "THIRD_PARTY_NOTICES.md")
	linked := linkedModules(t, root)

	for module := range linked {
		if !strings.Contains(notices, "| `"+module+"` |") {
			t.Errorf("module %s is linked but has no row in THIRD_PARTY_NOTICES.md", module)
		}
	}

	for _, match := range regexp.MustCompile("(?m)^\\| `([^`]+)` \\|").FindAllStringSubmatch(notices, -1) {
		if !linked[match[1]] {
			t.Errorf("THIRD_PARTY_NOTICES.md lists %s, which is not linked into any release binary", match[1])
		}
	}
}
