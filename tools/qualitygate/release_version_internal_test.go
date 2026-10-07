// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gitConfigVerb = "config"

func writeProjectVersion(t *testing.T, root, value string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(projectVersionFile))
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(value), fileMode); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredProjectVersionRejectsMissingAndNoncanonicalFiles(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"v1.2.3\n",
		"01.2.3\n",
		"1.2\n",
		"1.2.3+build\n",
		"1.2.3-01\n",
		"1.2.3.4\n",
		"1.2.3\n\n",
		" 1.2.3\n",
		"1.2.3\r\n",
		"1.2.3\n2.0.0\n",
	} {
		root := t.TempDir()
		writeProjectVersion(t, root, value)

		if version, err := configuredProjectVersion(root); err == nil {
			t.Fatalf("noncanonical %q accepted as %q", value, version)
		}
	}

	if _, err := configuredProjectVersion(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing authority failure lost: %v", err)
	}

	for _, value := range []string{linkerVersionFixture, "0.0.0\n", "1.2.3-rc.1\n", "1.2.3-alpha-01\n"} {
		root := t.TempDir()
		writeProjectVersion(t, root, value)

		got, err := configuredProjectVersion(root)
		if err != nil || got != strings.TrimSuffix(value, "\n") {
			t.Fatalf("canonical %q rejected: %q %v", value, got, err)
		}
	}
}

func TestCheckedOutProjectVersionIsCanonical(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	if _, versionErr := configuredProjectVersion(root); versionErr != nil {
		t.Fatal(versionErr)
	}
}

func TestArchivesRequireConfiguredVersionAndExactSnapshotSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectVersion(t, root, "1.2.3\n")

	for _, args := range [][]string{
		{"init", "-q"},
		{gitConfigVerb, "user.name", "Version Fixture"},
		{gitConfigVerb, "user.email", "fixture@example.invalid"},
		{gitConfigVerb, "core.abbrev", "12"},
		{"add", "."},
		{"commit", "-qm", "version fixture"},
	} {
		if _, err := (&command{dir: root, name: gitTool, args: args}).output(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	expected, err := snapshotProjectVersion(t.Context(), root, linkerVersionFixture)
	if err != nil {
		t.Fatal(err)
	}

	if len(strings.TrimPrefix(expected, "1.2.3-SNAPSHOT-")) != 12 {
		t.Fatalf("Git abbreviation policy ignored: %s", expected)
	}

	for _, version := range []string{linkerVersionFixture, expected, "9.9.9", "1.2.3-SNAPSHOT-deadbee", "1.2.3-SNAPSHOT", "1.2.3-rc.1"} {
		problems, archiveErr := archiveVersionProblems(t.Context(), root, map[string]bool{version: true})

		valid := version == linkerVersionFixture || version == expected
		if archiveErr != nil || (len(problems) == 0) != valid {
			t.Fatalf("archive %s valid=%t: %v/%v", version, valid, problems, archiveErr)
		}
	}
}

func TestConfiguredVersionFitsSnapshotArchiveAndReportIdentity(t *testing.T) {
	t.Parallel()

	maxVersion := maxConfiguredVersionBytes()
	if maxVersion != 175 {
		t.Fatalf("current archive overhead changed; maximum configured version length: %d", maxVersion)
	}

	prefix := "1.2.3-"
	longest := prefix + strings.Repeat("a", maxVersion-len(prefix))
	root := t.TempDir()
	writeProjectVersion(t, root, longest+"\n")

	if _, err := configuredProjectVersion(root); err != nil {
		t.Fatal(err)
	}

	writeProjectVersion(t, root, longest+"a\n")

	if _, err := configuredProjectVersion(root); err == nil {
		t.Fatal("snapshot would exceed portable archive filename component")
	}

	snapshot := longest + snapshotVersionMarker + strings.Repeat("a", maxSnapshotCommitBytes)

	filename := archiveNamePrefix + snapshot + "_darwin_amd64.tar.gz"
	if len(filename) != portableArchiveComponentBytes {
		t.Fatalf("archive filename length: %d", len(filename))
	}

	limit := reportProducerVersionLimit(t)
	if len(snapshot) > limit {
		t.Fatalf("snapshot identity exceeds report contract: %d > %d", len(snapshot), limit)
	}
}

func reportProducerVersionLimit(t *testing.T) int {
	t.Helper()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	data, err := readInRoot(root, "internal/report/report.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, member := range []string{"$defs", "producer", "properties", "version", "maxLength"} {
		var object map[string]json.RawMessage
		if decodeErr := json.Unmarshal(data, &object); decodeErr != nil {
			t.Fatal(decodeErr)
		}

		data = object[member]
	}

	var limit int
	if decodeErr := json.Unmarshal(data, &limit); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	return limit
}

func TestConfiguredVersionCoreMatchesReleaseParserUint64(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"18446744073709551616.0.0", "0.18446744073709551616.0", "0.0.18446744073709551616"} {
		root := t.TempDir()
		writeProjectVersion(t, root, value+"\n")

		if _, err := configuredProjectVersion(root); err == nil {
			t.Fatalf("overflowing release parser core accepted: %s", value)
		}
	}

	root := t.TempDir()
	writeProjectVersion(t, root, "18446744073709551615.18446744073709551615.18446744073709551615\n")

	if _, err := configuredProjectVersion(root); err != nil {
		t.Fatal(err)
	}
}
