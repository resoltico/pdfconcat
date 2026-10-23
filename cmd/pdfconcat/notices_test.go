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

func TestNoticesMatchLinkedTargetModules(t *testing.T) {
	data, err := os.ReadFile("../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| `") {
			columns := strings.Split(line, "|")
			name := strings.Trim(strings.TrimSpace(columns[1]), "`")
			if documented[name] {
				t.Fatalf("duplicate notice %s", name)
			}
			documented[name] = true
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	linked := map[string]bool{}
	for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		command := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./cmd/pdfconcat")
		command.Dir = root
		command.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("target %s modules: %v %s", target, err, output)
		}
		for _, module := range strings.Fields(string(output)) {
			if module != "github.com/resoltico/pdfconcat" {
				linked[module] = true
			}
		}
	}
	if len(linked) != len(documented) {
		t.Fatalf("linked/noticed module count %d/%d", len(linked), len(documented))
	}
	for module := range linked {
		if !documented[module] {
			t.Fatalf("missing notice %s", module)
		}
	}
}
