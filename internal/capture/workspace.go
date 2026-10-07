// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
)

// Workspace is the owner-only private directory (0700 on Unix; protected owner DACL on Windows) that owns every scratch file of one job:
// snapshots of sources and fonts, and staged output. Close removes everything it owns.
// A Workspace is safe for concurrent use.
type Workspace struct {
	// openScratch creates one exclusive scratch file; tests replace it to inject write failures.
	openScratch func(path string) (io.WriteCloser, error)
	// afterChunk runs after each copied chunk; tests use it to change or cancel mid-copy.
	afterChunk func(copied int64)
	dir        string
	counter    atomic.Uint64
}

const (
	besideOutputPattern = ".pdfconcat-job-*"
	temporaryPattern    = "pdfconcat-job-*"
	scratchPermissions  = 0o600
)

// NewWorkspaceBeside creates the workspace in the directory that will receive the published
// output, so that staged files can be published by a same-filesystem rename. The directory must
// already exist.
func NewWorkspaceBeside(destination string) (*Workspace, error) {
	return newWorkspace(filepath.Dir(destination), besideOutputPattern)
}

// NewTemporaryWorkspace creates the workspace in the operating system's temporary directory, for
// a check that has no output destination.
func NewTemporaryWorkspace() (*Workspace, error) {
	return newWorkspace("", temporaryPattern)
}

func newWorkspace(parent, pattern string) (*Workspace, error) {
	dir, err := createWorkspaceDirectory(parent, pattern)
	if err != nil {
		location := parent
		if location == "" {
			location = os.TempDir()
		}

		return nil, &ScratchError{Dir: location, Operation: "create private job workspace", Err: err}
	}

	return &Workspace{dir: dir, openScratch: openExclusiveScratch}, nil
}

// openExclusiveScratch creates path, which must not exist, as a private write-only file.
func openExclusiveScratch(path string) (io.WriteCloser, error) {
	// The path comes from NewPath inside the private workspace; Clean only normalizes it.
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, scratchPermissions)
	if err != nil {
		return nil, fmt.Errorf("create scratch file: %w", err)
	}

	return file, nil
}

// Dir returns the absolute-or-as-created path of the private directory.
func (w *Workspace) Dir() string { return w.dir }

// NewPath returns a not-yet-existing path inside the workspace, for staged output. The name is
// unique within the workspace and carries the given extension (including the dot, or empty).
func (w *Workspace) NewPath(extension string) string {
	return filepath.Join(w.dir, "scratch-"+strconv.FormatUint(w.counter.Add(1), 10)+extension)
}

// Close removes the workspace and everything in it. It is safe to call more than once.
func (w *Workspace) Close() error {
	err := os.RemoveAll(w.dir)
	if err != nil {
		return &ScratchError{Dir: w.dir, Operation: "remove job workspace", Err: err}
	}

	return nil
}
