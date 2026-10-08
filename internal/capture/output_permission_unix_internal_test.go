//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

func TestUnobservableProtectedOutputCannotAuthorizeReportReplacement(t *testing.T) {
	t.Parallel()
	permissiontest.RequireEnforcement(t)
	dir := t.TempDir()
	output := filepath.Join(dir, outputPath)
	writeTestFile(t, output, []byte("preserve"))

	registry := NewRegistry()
	if err := registry.ProtectOutput(output); err != nil {
		t.Fatal(err)
	}

	permissiontest.DenyDirectoryChanges(t, dir)

	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}

	_, err := inspectArtifactIdentity(RoleOutput, output)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unknown identity guessed as absent: %v", err)
	}

	_, err = registry.Add(RoleReport, filepath.Join(t.TempDir(), "safe.json"))
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unknown protected identity accepted: %v", err)
	}

	if artifactParentUnavailable(string(filepath.Separator)) {
		t.Fatal("filesystem root was considered absent")
	}
}
