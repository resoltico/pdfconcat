//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

func TestWindowsPrivateWorkspaceAndFileExcludeInheritedOtherAccess(t *testing.T) {
	t.Parallel()
	parent := broadPrivacyParent(t)

	workspace, err := NewWorkspaceBeside(filepath.Join(parent, "output.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeWorkspace(t, workspace)

	permissiontest.RequirePrivateDirectory(t, workspace.Dir())

	path := workspace.NewPath(".pdf")
	if writeErr := os.WriteFile(path, []byte("private bytes"), testPermissions); writeErr != nil {
		t.Fatal(writeErr)
	}

	inherited, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}

	inheritedACL, _, err := inherited.DACL()
	if err != nil || inheritedACL == nil || inheritedACL.AceCount != 1 {
		t.Fatalf("private child inherited other access: %v", err)
	}

	file, err := CreatePrivateTemp(parent, privateFixturePattern)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()

	if _, writeErr := file.WriteString("private report"); writeErr != nil {
		t.Fatal(writeErr)
	}

	permissiontest.RequirePrivateFile(t, file.Name())

	if got := string(readTestFile(t, file.Name())); got != "private report" {
		t.Fatalf("owner read/write failed: %q", got)
	}
}

func TestWindowsPrivateCreationRejectsCollisionAndPreservesForeignPath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "foreign")
	writeTestFile(t, path, []byte("foreign bytes"))

	if file, err := createPrivateWindowsFile(path); err == nil {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}

		t.Fatal("exclusive private creation replaced foreign path")
	}

	if got := string(readTestFile(t, path)); got != "foreign bytes" {
		t.Fatal("foreign bytes changed")
	}
}

func broadPrivacyParent(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()

	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}

	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}

	setErr := windows.SetNamedSecurityInfo(
		parent, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	)
	if setErr != nil {
		t.Fatal(setErr)
	}

	return parent
}

func TestWindowsPrivateCreatorsRejectNULWithoutChangingNamespace(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	sentinel := filepath.Join(parent, "sentinel")
	writeTestFile(t, sentinel, []byte("unchanged"))

	invalid := parent + string(filepath.Separator) + "invalid\x00directory"

	workspace, workspaceErr := NewWorkspaceBeside(filepath.Join(invalid, outputPath))
	if workspace != nil {
		closeWorkspace(t, workspace)
	}

	failure, isScratch := errors.AsType[*ScratchError](workspaceErr)
	if !isScratch || failure.Dir != invalid || workspace != nil {
		t.Fatalf("unrepresentable workspace accepted or lost its path: %v", workspaceErr)
	}

	file, fileErr := CreatePrivateTemp(invalid, privateFixturePattern)
	if file != nil {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}

	if fileErr == nil || file != nil {
		t.Fatal("unrepresentable private file accepted")
	}

	if string(readTestFile(t, sentinel)) != "unchanged" {
		t.Fatal("rejected private creation changed existing bytes")
	}

	entries, readErr := os.ReadDir(parent)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("rejected private creation changed namespace: %v %v", entries, readErr)
	}
}
