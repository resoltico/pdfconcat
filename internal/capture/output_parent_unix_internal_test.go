//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedOutputParentTraversalUsesNativeObjectIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logical, physical := filepath.Join(dir, "logical"), filepath.Join(dir, "physical")
	for _, path := range []string{logical, filepath.Join(physical, "sub"), filepath.Join(physical, "slot")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(physical, "sub"), filepath.Join(logical, "link")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(physical, "slot", outputPath)
	writeTestFile(t, target, []byte("native input object"))
	literal := logical + "/link/../slot/" + outputPath
	native, err := IdentityOf(literal)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := IdentityOf(target)
	if err != nil || native != expected {
		t.Fatalf("native fixture objects differ: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logical, "slot")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("logical parent is not distinctly absent: %v", err)
	}
	registry := NewRegistry()
	if _, err := registry.Add(RoleSource, target); err != nil {
		t.Fatal(err)
	}
	err = registry.ProtectOutput(literal)
	var alias *AliasError
	if !errors.As(err, &alias) || alias.Role != RoleOutput || alias.OtherRole != RoleSource || alias.Path != literal || alias.OtherPath != target {
		t.Fatalf("existing native output/input alias guessed absent: %v", err)
	}
	protected := NewRegistry()
	if err := protected.ProtectOutput(literal); err != nil {
		t.Fatal(err)
	}
	// An input must see protection before another artifact registration can refresh it.
	requireRegistryAlias(t, protected, RoleFont, target, RoleOutput, literal)
}
