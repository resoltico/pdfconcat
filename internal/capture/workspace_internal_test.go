// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

const namedPath = "p.pdf"

func TestWorkspaceBesideIsPrivateAndRemovedOnClose(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()

	workspace, err := NewWorkspaceBeside(filepath.Join(parent, outputPath))
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Dir(workspace.Dir()) != parent {
		t.Fatalf("workspace %q is not beside the destination in %q", workspace.Dir(), parent)
	}

	permissiontest.RequirePrivateDirectory(t, workspace.Dir())

	writeTestFile(t, workspace.NewPath(pdfExtension), []byte("x"))

	err = workspace.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = workspace.Close()
	if err != nil {
		t.Fatalf("second Close() = %v", err)
	}

	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("parent still has %v (err %v)", entries, err)
	}
}

func TestTemporaryWorkspaceLivesInTemporaryDirectory(t *testing.T) {
	temporary := t.TempDir()

	t.Setenv("TMPDIR", temporary)
	t.Setenv("TMP", temporary)
	t.Setenv("TEMP", temporary)

	workspace, err := NewTemporaryWorkspace()
	if err != nil {
		t.Fatal(err)
	}

	defer closeWorkspace(t, workspace)

	if filepath.Dir(workspace.Dir()) != filepath.Clean(temporary) {
		t.Fatalf("workspace %q is outside %q", workspace.Dir(), temporary)
	}
}

func TestNewPathIsUniqueAndKeepsExtension(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	first, second := workspace.NewPath(pdfExtension), workspace.NewPath("")

	if first == second || !strings.HasSuffix(first, pdfExtension) || filepath.Dir(first) != workspace.Dir() {
		t.Fatalf("paths %q, %q", first, second)
	}
}

func TestWorkspaceCreationFailuresAreActionable(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), missingPath, outputPath)

	_, err := NewWorkspaceBeside(missing)

	scratch, isScratch := errors.AsType[*ScratchError](err)
	if !isScratch || scratch.Dir == "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing parent error = %v", err)
	}

	permissiontest.RequireEnforcement(t)

	readOnly := filepath.Join(t.TempDir(), "ro")

	// Without the search bit nothing can be created inside, which is how a read-only directory
	// behaves for this purpose.
	err = os.Mkdir(readOnly, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	permissiontest.DenyDirectoryChanges(t, readOnly)

	_, err = NewWorkspaceBeside(filepath.Join(readOnly, outputPath))
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("read-only parent error = %v", err)
	}
}

func TestTemporaryWorkspaceFailureNamesTemporaryDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), missingPath)

	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)

	_, err := NewTemporaryWorkspace()

	scratch, isScratch := errors.AsType[*ScratchError](err)
	if !isScratch || scratch.Dir != missing {
		t.Fatalf("error = %v", err)
	}
}

func TestCloseReportsUnremovableWorkspace(t *testing.T) {
	t.Parallel()

	// A directory name with a NUL byte can never be removed on any platform.
	workspace := &Workspace{dir: "unremovable\x00workspace"}

	err := workspace.Close()

	scratch, isScratch := errors.AsType[*ScratchError](err)
	if !isScratch || scratch.Operation != "remove job workspace" {
		t.Fatalf("Close() = %v", err)
	}
}

func TestScratchAdviceByFailureKind(t *testing.T) {
	t.Parallel()

	full := &ScratchError{Dir: "d", Operation: "copy", Err: fmt.Errorf("write: %w", errDiskFull)}
	if !strings.Contains(full.Error(), "volume is full") || !IsDiskFull(full) {
		t.Fatalf("disk-full error = %v", full)
	}

	other := &ScratchError{Dir: "d", Operation: "copy", Err: errEmptyPath}
	if !strings.Contains(other.Error(), "healthy") || IsDiskFull(other) {
		t.Fatalf("generic error = %v", other)
	}
}

func TestErrorMessagesNameThePath(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		&NotRegularFileError{Path: namedPath, Kind: kindDirectory},
		&SourceChangedError{Path: namedPath},
		&SourceError{Path: namedPath, Operation: "open", Err: errEmptyPath},
	} {
		if !strings.Contains(err.Error(), namedPath) {
			t.Errorf("%T message %q lacks the path", err, err)
		}
	}

	if !errors.Is(&SourceError{Err: errEmptyPath}, errEmptyPath) {
		t.Error("SourceError does not unwrap")
	}
}
