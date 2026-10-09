// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package capture

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestProtectedOutputRetainsNativeWindowsPathEncodingFailure(t *testing.T) {
	t.Parallel()

	path := "invalid\x00.pdf"
	registry := NewRegistry()
	err := registry.ProtectOutput(path)

	var source *SourceError
	if !errors.As(err, &source) || !errors.Is(err, syscall.EINVAL) || source.Path != path ||
		source.Operation != "resolve protected output" || len(registry.artifactBindings) != 0 {
		t.Fatalf("invalid native path changed protected claims: %v/%+v", err, registry.artifactBindings)
	}
}

func TestWindowsVolumeRootIsNotAnUnavailableArtifactParent(t *testing.T) {
	t.Parallel()

	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("native volume root prerequisite: %s/%v", root, err)
	}

	if artifactParentUnavailable(root) {
		t.Fatalf("existing volume root considered absent: %s", root)
	}
}

func TestWindowsProtectedOutputDeviceReplacementRefusesReportAndRetainsClaims(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	output := filepath.Join(dir, outputPath)
	writeTestFile(t, output, []byte("preserve real output object"))

	registry := NewRegistry()
	if err := registry.ProtectOutput(output); err != nil {
		t.Fatal(err)
	}

	bindings := maps.Clone(registry.artifactBindings)
	claims := maps.Clone(registry.byIdentity)

	replaceOutputWithWindowsDeviceLink(t, output)
	requireWindowsCharacterDevice(t, output)

	if identity, err := inspectArtifactIdentity(RoleOutput, output); identity != (Identity{}) || !errors.Is(err, errNotDiskFile) {
		t.Fatalf("native device identity guessed as a filesystem object: %v/%v", identity, err)
	}

	_, err := registry.Add(RoleReport, filepath.Join(t.TempDir(), "safe-report.json"))
	if !errors.Is(err, errNotDiskFile) || !maps.Equal(bindings, registry.artifactBindings) || !maps.Equal(claims, registry.byIdentity) {
		t.Fatalf("unobservable protected object authorized report or changed claims: %v", err)
	}
}

func replaceOutputWithWindowsDeviceLink(t *testing.T, output string) {
	t.Helper()

	retained := filepath.Join(filepath.Dir(output), "retained-output.pdf")
	if err := os.Rename(output, retained); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}

		if err := os.Rename(retained, output); err != nil {
			t.Error(err)
		}
	})

	if err := os.Symlink(`\\.\NUL`, output); err != nil {
		t.Fatalf(symlinkUnavailableFormat, err)
	}
}

func requireWindowsCharacterDevice(t *testing.T, path string) {
	t.Helper()

	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()

	kind, queryErr := windows.GetFileType(windows.Handle(file.Fd()))
	if queryErr != nil || kind != windows.FILE_TYPE_CHAR {
		t.Fatalf("device-link prerequisite not independently established: type=%#x/%v", kind, queryErr)
	}
}
