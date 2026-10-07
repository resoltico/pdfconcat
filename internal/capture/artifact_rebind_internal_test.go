// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const formerArtifactName = "former.json"

func TestArtifactRebindRetiresOnlyReplacedObject(t *testing.T) {
	t.Parallel()

	registry, output, former := artifactReplacementFixture(t)
	rebindFixture(t, registry, output)

	if _, err := registry.Add(RoleReport, former); err != nil {
		t.Fatalf("former distinct object rejected: %v", err)
	}

	requireRegistryAlias(t, registry, RoleReport, output, RoleOutput, output)
}

func TestArtifactRebindPreservesIndependentHardlinkAndAccuratePath(t *testing.T) {
	t.Parallel()

	registry, output, former := artifactReplacementFixture(t)
	if _, err := registry.Add(RoleOutput, former); err != nil {
		t.Fatal(err)
	}

	rebindFixture(t, registry, output)
	requireRegistryAlias(t, registry, RoleReport, former, RoleOutput, former)
	requireRegistryAlias(t, registry, RoleReport, output, RoleOutput, output)
}

func artifactReplacementFixture(t *testing.T) (*Registry, string, string) {
	t.Helper()
	dir := t.TempDir()
	output, former := filepath.Join(dir, outputPath), filepath.Join(dir, formerArtifactName)
	writeTestFile(t, output, []byte("old object"))

	if err := os.Link(output, former); err != nil {
		t.Fatalf("required hardlink capability unavailable: %v", err)
	}

	registry := NewRegistry()
	if _, err := registry.Add(RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	return registry, output, former
}

func rebindFixture(t *testing.T, registry *Registry, output string) {
	t.Helper()
	replaceArtifactFixture(t, output)

	if _, err := registry.Add(RoleOutput, output); err != nil {
		t.Fatal(err)
	}
}

func replaceArtifactFixture(t *testing.T, destination string) {
	t.Helper()

	staged := filepath.Join(filepath.Dir(destination), "replacement")
	writeTestFile(t, staged, []byte("replacement object"))

	if err := os.Rename(staged, destination); err != nil {
		t.Fatal(err)
	}
}

func TestFailedArtifactRebindPreservesPriorAndInputClaims(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source, output, former := filepath.Join(dir, sourceAPath), filepath.Join(dir, outputPath), filepath.Join(dir, formerArtifactName)
	writeTestFile(t, source, []byte("immutable input"))
	writeTestFile(t, output, []byte("old artifact"))

	if err := os.Link(output, former); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry()
	if _, err := registry.Add(RoleSource, source); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Add(RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(source, output); err != nil {
		t.Fatal(err)
	}

	requireRegistryAlias(t, registry, RoleOutput, output, RoleSource, source)
	requireRegistryAlias(t, registry, RoleFont, former, RoleOutput, output)

	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}

	replaceArtifactFixture(t, output)

	if _, err := registry.Add(RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	requireRegistryAlias(t, registry, RoleReport, source, RoleSource, source)

	if _, err := registry.Add(RoleReport, former); err != nil {
		t.Fatal(err)
	}
}

func requireRegistryAlias(t *testing.T, registry *Registry, role Role, path string, otherRole Role, otherPath string) {
	t.Helper()

	_, err := registry.Add(role, path)

	var alias *AliasError
	if !errors.As(err, &alias) || alias.OtherRole != otherRole || alias.OtherPath != otherPath {
		t.Fatalf("alias identity/path lost: %v", err)
	}
}

func TestRetiredReportLeavesInputsAndOtherArtifactsProtected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	output, original := filepath.Join(dir, outputPath), filepath.Join(dir, "original.json")

	recovery, source := filepath.Join(dir, "recovery.json"), filepath.Join(dir, sourceAPath)
	for _, path := range []string{output, original, recovery, source} {
		writeTestFile(t, path, []byte(path))
	}

	registry := NewRegistry()
	for path, role := range map[string]Role{output: RoleOutput, original: RoleReport, source: RoleSource} {
		if _, err := registry.Add(role, path); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(output, original); err != nil {
		t.Fatal(err)
	}

	if err := registry.RetireReport(original); err != nil {
		t.Fatal(err)
	}

	if err := registry.RetireReport(original); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Add(RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Add(RoleReport, recovery); err != nil {
		t.Fatal(err)
	}

	requireRegistryAlias(t, registry, RoleFont, source, RoleSource, source)
	requireRegistryAlias(t, registry, RoleFont, output, RoleOutput, output)
}

func TestRetiredReportKeepsNameReservation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "reserved.json")

	registry := NewRegistry()
	if _, err := registry.Add(RoleReport, path); err != nil {
		t.Fatal(err)
	}

	if err := registry.RetireReport(path); err != nil {
		t.Fatal(err)
	}

	requireRegistryAlias(t, registry, RoleOutput, path, RoleReport, path)
}

func TestArtifactRebindRefreshesAlternateParentSpelling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	parent, alternate := filepath.Join(dir, "parent"), filepath.Join(dir, "alternate")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(parent, alternate); err != nil {
		t.Fatalf("required directory symlink capability unavailable: %v", err)
	}

	output, other := filepath.Join(parent, outputPath), filepath.Join(alternate, outputPath)
	former := filepath.Join(parent, formerArtifactName)

	writeTestFile(t, output, []byte("old object"))

	if err := os.Link(output, former); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry()
	for _, path := range []string{output, other} {
		if _, err := registry.Add(RoleOutput, path); err != nil {
			t.Fatal(err)
		}
	}

	rebindFixture(t, registry, output)

	if _, err := registry.Add(RoleReport, former); err != nil {
		t.Fatalf("alternate spelling retained obsolete identity: %v", err)
	}
}

func TestArtifactRefreshFailureDoesNotPartiallyRebind(t *testing.T) {
	t.Parallel()

	registry, output, former := artifactReplacementFixture(t)
	if _, err := registry.Add(RoleOutput, former); err != nil {
		t.Fatal(err)
	}

	witness := filepath.Join(filepath.Dir(output), "witness.json")
	if err := os.Link(former, witness); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(former); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(former, 0o700); err != nil {
		t.Fatal(err)
	}

	replaceArtifactFixture(t, output)

	if _, err := registry.Add(RoleOutput, output); err == nil {
		t.Fatal("invalid remaining artifact was accepted")
	}

	_, err := registry.Add(RoleFont, witness)

	var alias *AliasError
	if !errors.As(err, &alias) || alias.OtherRole != RoleOutput {
		t.Fatalf("failed refresh retired previous claim: %v", err)
	}
}

func TestRefreshedArtifactConflictStopsThirdRegistration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	output, reportPath, third := filepath.Join(dir, outputPath), filepath.Join(dir, "report.json"), filepath.Join(dir, "third.json")
	writeTestFile(t, output, []byte("output object"))
	writeTestFile(t, reportPath, []byte("report object"))

	registry := NewRegistry()
	if _, err := registry.Add(RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Add(RoleReport, reportPath); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(reportPath); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(output, reportPath); err != nil {
		t.Fatal(err)
	}

	_, err := registry.Add(RoleOutput, third)

	var alias *AliasError
	if !errors.As(err, &alias) || alias.Role == alias.OtherRole {
		t.Fatalf("refreshed other-role conflict ignored: %v", err)
	}
}
