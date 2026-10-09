// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package capture

import (
	"bytes"
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

func TestWindowsUnresolvableProtectedDeviceLinkRefusesReportAndRetainsClaims(t *testing.T) {
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
	requireWindowsInvalidDeviceLink(t, output)

	if identity, err := inspectArtifactIdentity(RoleOutput, output); identity != (Identity{}) ||
		!errors.Is(err, windows.ERROR_INVALID_REPARSE_DATA) {
		t.Fatalf("failed native link observation guessed as a filesystem identity: %v/%v", identity, err)
	}

	_, err := registry.Add(RoleReport, filepath.Join(t.TempDir(), "safe-report.json"))
	if !errors.Is(err, windows.ERROR_INVALID_REPARSE_DATA) || !maps.Equal(bindings, registry.artifactBindings) ||
		!maps.Equal(claims, registry.byIdentity) {
		t.Fatalf("unobservable protected object authorized report or changed claims: %v", err)
	}

	var source *SourceError
	if !errors.As(err, &source) || source.Path != output || source.Operation != "identify output" {
		t.Fatalf("failed protected observation lost its path or operation: %v", err)
	}
}

func replaceOutputWithWindowsDeviceLink(t *testing.T, output string) {
	t.Helper()

	original := readTestFile(t, output)

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

		if !bytes.Equal(readTestFile(t, output), original) {
			t.Error("restored output bytes differ from the original object")
		}
	})

	if err := os.Symlink(`\\.\NUL`, output); err != nil {
		t.Fatalf(symlinkUnavailableFormat, err)
	}
}

func requireWindowsInvalidDeviceLink(t *testing.T, path string) {
	t.Helper()

	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}

	handle, openErr := windows.CreateFile(
		name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0,
	)
	if openErr == nil {
		if closeErr := windows.CloseHandle(handle); closeErr != nil {
			t.Error(closeErr)
		}

		t.Fatal("required native unresolvable device link unexpectedly opened")
	}

	if !errors.Is(openErr, windows.ERROR_INVALID_REPARSE_DATA) {
		t.Fatalf("native unresolvable device-link prerequisite not established: %v", openErr)
	}
}
