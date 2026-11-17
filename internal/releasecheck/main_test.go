// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingTargetWithRecomputedManifestRefused(t *testing.T) {
	expected, err := inventory("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var lines strings.Builder
	for _, name := range names(expected) {
		if name == "pdfconcat_windows_arm64.zip" {
			continue
		}
		lines.WriteString(strings.Repeat("0", 64) + "  " + name + "\n")
	}
	if err = os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(lines.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err = verify("../..", dir); err == nil || !strings.Contains(err.Error(), "complete required asset set") {
		t.Fatalf("missing target was not independently detected: %v", err)
	}
}
func TestConfigurationCannotSilentlyDropSupportedTarget(t *testing.T) {
	data, err := os.ReadFile("../../.goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	data = []byte(strings.Replace(string(data), "goos: [darwin, linux, windows]", "goos: [darwin, linux]", 1))
	if err = os.WriteFile(filepath.Join(root, ".goreleaser.yml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = inventory(root); err == nil || !strings.Contains(err.Error(), "six-target support") {
		t.Fatalf("configuration deletion redefined support: %v", err)
	}
}
