// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedUnusableOutputDoesNotBlockDistinctReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "parent-file")
	writeTestFile(t, parentFile, []byte("not a directory"))

	targets := []string{dir, filepath.Join(dir, "absent-parent", outputPath), filepath.Join(parentFile, "nested-file", outputPath)}
	for _, output := range targets {
		registry := NewRegistry()
		if err := registry.ProtectOutput(output); err != nil {
			t.Fatalf("protect %q: %v", output, err)
		}

		if _, err := registry.Add(RoleReport, filepath.Join(dir, "safe-evidence.json")); err != nil {
			t.Fatalf("report blocked by %q: %v", output, err)
		}

		if _, err := registry.Add(RoleOutput, output); err == nil {
			t.Fatalf("unusable PDF target %q approved", output)
		}
	}
}

func TestProtectedNewOutputStillRejectsReportNameAliases(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	path := filepath.Join(t.TempDir(), "Packet.pdf")

	if err := registry.ProtectOutput(path); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{path, filepath.Join(filepath.Dir(path), "packet.PDF")} {
		_, err := registry.Add(RoleReport, target)

		var alias *AliasError
		if !errors.As(err, &alias) || alias.OtherRole != RoleOutput {
			t.Fatalf("protected name %q: %v", target, err)
		}
	}

	if err := registry.ProtectOutput(""); err == nil {
		t.Fatal("empty output became the working directory")
	}
}

func TestProtectedOutputRefreshRejectsInputAndReportIdentityAliases(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, outputPath)
	writeTestFile(t, source, []byte("input"))

	registry := NewRegistry()
	if _, err := registry.Add(RoleSource, source); err != nil {
		t.Fatal(err)
	}

	if err := registry.ProtectOutput(output); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(source, output); err != nil {
		t.Fatal(err)
	}

	if err := registry.ProtectOutput(output); err == nil {
		t.Fatal("late output/input alias accepted")
	}

	other := NewRegistry()
	if err := other.ProtectOutput(output); err != nil {
		t.Fatal(err)
	}

	_, err := other.Add(RoleReport, source)

	var alias *AliasError
	if !errors.As(err, &alias) || alias.OtherRole != RoleOutput {
		t.Fatalf("report aliased protected output: %v", err)
	}
}
