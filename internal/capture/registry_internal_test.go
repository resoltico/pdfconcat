// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestRoleNames(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RolePlan, RoleSource, RoleFont, RoleOutput, RoleReport, Role(0)} {
		if role.String() == "" {
			t.Errorf("role %d has no name", role)
		}
	}
}

func TestIdentityOfFollowsLinksAndReportsMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target, link, hard := filepath.Join(dir, "t"), filepath.Join(dir, "s"), filepath.Join(dir, "h")
	writeTestFile(t, target, []byte("x"))

	err := os.Symlink(target, link)
	if err != nil {
		t.Fatalf(symlinkUnavailableFormat, err)
	}

	err = os.Link(target, hard)
	if err != nil {
		t.Fatalf("required hard-link capability unavailable: %v", err)
	}

	want, err := IdentityOf(target)
	if err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{link, hard} {
		got, aliasErr := IdentityOf(alias)
		if aliasErr != nil || got != want {
			t.Fatalf("IdentityOf(%q) = %v, %v; want %v", alias, got, aliasErr, want)
		}
	}

	if want.String() == "" {
		t.Fatal("empty identity string")
	}

	_, err = IdentityOf(filepath.Join(dir, missingPath))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("IdentityOf(missing) = %v", err)
	}
}

func TestRegistryRejectsOneFileInTwoRoles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file, hard, link := filepath.Join(dir, sourceAPath), filepath.Join(dir, "hard.pdf"), filepath.Join(dir, "link.pdf")
	writeTestFile(t, file, []byte("x"))

	linkErr := os.Symlink(file, link)
	hardErr := os.Link(file, hard)

	if linkErr != nil || hardErr != nil {
		t.Fatalf("required link capabilities unavailable: symlink=%v hardlink=%v", linkErr, hardErr)
	}

	tests := map[string]struct {
		firstPath     string
		secondPath    string
		first, second Role
	}{
		"output is a hard link of a source": {file, hard, RoleSource, RoleOutput},
		"source symlink resolves to a font": {file, link, RoleFont, RoleSource},
		"report is the plan":                {file, file, RolePlan, RoleReport},
		"plan is a source":                  {hard, file, RoleSource, RolePlan},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := NewRegistry()

			_, err := registry.Add(test.first, test.firstPath)
			if err != nil {
				t.Fatal(err)
			}

			_, err = registry.Add(test.second, test.secondPath)

			var alias *AliasError
			if !errors.As(err, &alias) || alias.OtherRole != test.first || !strings.Contains(err.Error(), test.secondPath) {
				t.Fatalf("Add() = %v", err)
			}
		})
	}
}

func TestRegistryAllowsRepeatedRoleAndDistinctFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, second := filepath.Join(dir, sourceAPath), filepath.Join(dir, "b.pdf")
	writeTestFile(t, first, []byte("a"))
	writeTestFile(t, second, []byte("b"))

	registry := NewRegistry()

	for _, path := range []string{first, first, second} {
		identity, err := registry.Add(RoleSource, path)
		if err != nil || identity == (Identity{}) {
			t.Fatalf("Add(%q) = %v, %v", path, identity, err)
		}
	}

	_, err := registry.Add(RoleOutput, filepath.Join(dir, outputPath))
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Add(RoleOutput, filepath.Join(dir, outputPath))
	if err != nil {
		t.Fatalf("repeating the output role = %v", err)
	}
}

func TestRegistryRejectsUnusableTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file, link, fifo := filepath.Join(dir, "f"), filepath.Join(dir, "l"), filepath.Join(dir, "p")
	writeTestFile(t, file, []byte("x"))

	if err := os.Symlink(file, link); err != nil {
		t.Fatalf("required symbolic-link capability unavailable: %v", err)
	}

	tests := map[string]string{
		"symbolic link": link,
		"directory":     dir,
		"under a file":  filepath.Join(file, "child"),
	}

	if runtime.GOOS != "windows" {
		if err := makeFIFO(fifo); err != nil {
			t.Fatalf("required FIFO capability unavailable: %v", err)
		}

		tests["named pipe"] = fifo
	}

	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewRegistry().Add(RoleOutput, path)
			if err == nil {
				t.Fatalf("Add(%q) succeeded", path)
			}
		})
	}

	var target *ArtifactTargetError

	_, err := NewRegistry().Add(RoleReport, link)
	if !errors.As(err, &target) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symbolic link report = %v", err)
	}

	_, err = NewRegistry().Add(RoleOutput, filepath.Join(file, "child"))
	if err == nil {
		t.Fatal("output under a regular file accepted")
	}
}

func TestRegistryRejectsMissingInputsAndEmptyPaths(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	dir := t.TempDir()

	for _, test := range []struct {
		path string
		role Role
	}{
		{filepath.Join(dir, missingPDFPath), RoleSource},
		{"", RolePlan},
		{filepath.Join(dir, "missing-dir", outputPath), RoleOutput},
	} {
		_, err := registry.Add(test.role, test.path)

		sourceErr, isSource := errors.AsType[*SourceError](err)
		if !isSource || sourceErr.Operation == "" {
			t.Errorf("Add(%v, %q) = %v", test.role, test.path, err)
		}
	}
}

func TestRegistryRejectsNewOutputAndReportWithOnePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	registry := NewRegistry()
	path := filepath.Join(dir, "result.json")

	_, err := registry.Add(RoleOutput, path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Add(RoleReport, path)

	alias, isAlias := errors.AsType[*AliasError](err)
	if !isAlias || alias.Path != path {
		t.Fatalf("Add() = %v", err)
	}

	_, err = registry.Add(RoleReport, filepath.Join(dir, "other.json"))
	if err != nil {
		t.Fatalf("distinct report rejected: %v", err)
	}
}

func TestRegistryRejectsPotentialAliasesOfNewArtifacts(t *testing.T) {
	t.Parallel()

	for _, names := range [][2]string{{outputPath, "OUT.pdf"}, {"café.pdf", "cafe\u0301.pdf"}} {
		for _, populated := range []bool{false, true} {
			dir := t.TempDir()
			if populated {
				writeTestFile(t, filepath.Join(dir, "marker"), []byte("x"))
			}

			registry := NewRegistry()
			if _, err := registry.Add(RoleOutput, filepath.Join(dir, names[0])); err != nil {
				t.Fatal(err)
			}

			if _, err := registry.Add(RoleReport, filepath.Join(dir, names[1])); err == nil {
				t.Fatal("potential alias accepted")
			}
		}
	}
}

func TestRegistryIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, sourceAPath)
	writeTestFile(t, path, []byte("x"))

	registry := NewRegistry()

	var group sync.WaitGroup

	for range 16 {
		group.Go(func() {
			_, err := registry.Add(RoleSource, path)
			if err != nil {
				t.Error(err)
			}
		})
	}

	group.Wait()
}
