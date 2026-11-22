// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Package files publishes a sibling temporary file without exposing partial bytes.
package files

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Staged owns its temporary name. Published state is independent of later errors.
type Staged struct {
	*os.File
	target, temp   string
	parent         *os.Root
	parentIdentity fs.FileInfo
	fileIdentity   fs.FileInfo
	name           string
	creationMode   fs.FileMode
	mode           fs.FileMode
	published      bool
	closed         bool
}

// ErrDestinationExists identifies rejection by an atomic no-replace operation.
var ErrDestinationExists = errors.New("publication destination exists")

// ErrStageParentChanged identifies an observed destination directory replacement.
var ErrStageParentChanged = errors.New("destination parent directory changed during staging")

// Create uses conventional permissions (0666 minus umask). If an incumbent
// exists, the empty stage adopts its permissions before any bytes are written.
// Abort releases the owned resources after a failed or abandoned stage.
func Create(target string) (*Staged, error) {
	parentPath := filepath.Dir(target)
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	parentIdentity, err := parent.Stat(".")
	if err != nil {
		return nil, errors.Join(err, closeStageParent(parent))
	}
	var mode fs.FileMode
	existing, err := parent.Lstat(filepath.Base(target))
	if err == nil {
		if !existing.Mode().IsRegular() {
			return nil, errors.Join(fmt.Errorf("target is not a regular file: %s", target), closeStageParent(parent))
		}
		mode = existing.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.Join(err, closeStageParent(parent))
	}
	// Keep the sibling name independent of the target's filesystem name budget.
	name := ".pdfconcat." + rand.Text() + ".tmp"
	f, err := parent.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return nil, errors.Join(err, closeStageParent(parent))
	}
	result := &Staged{File: f, target: target, temp: filepath.Join(parentPath, name),
		parent: parent, parentIdentity: parentIdentity, name: name}
	info, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, result.Abort())
	}
	result.fileIdentity = info
	result.creationMode = info.Mode().Perm()
	result.mode = result.creationMode
	if existing != nil {
		if err = result.setMode(mode); err != nil {
			return nil, errors.Join(err, result.Abort())
		}
	}
	return result, nil
}

func (f *Staged) setMode(mode fs.FileMode) error {
	if mode == f.mode {
		return nil
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	f.mode = mode
	return nil
}

// Path is the stage's original path. Cleanup uses the owned directory handle.
func (f *Staged) Path() string { return f.temp }

// Reader reads the completed stage through the owned handle. It starts at zero,
// leaves the writer's position alone, and is bounded to the current file length.
// The stage retains ownership of the handle until commit or abort.
func (f *Staged) Reader(c context.Context) (io.ReadSeeker, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return io.NewSectionReader(f.File, 0, info.Size()), nil
}

// Published stays true if rename/link succeeded even when Commit also failed.
func (f *Staged) Published() bool { return f.published }

// Commit performs the native namespace transition. Roles.Publish supplies
// the final role and permission checks for application destinations.
func (f *Staged) Commit(overwrite bool) (bool, error) {
	if f.closed {
		return f.published, errors.New("temporary file is already closed")
	}
	if err := f.Sync(); err != nil {
		return false, err
	}
	if err := f.closeFile(); err != nil {
		return false, err
	}
	info, err := os.Stat(filepath.Dir(f.target))
	if err != nil {
		return false, fmt.Errorf("inspect destination parent before publication: %w", err)
	}
	if !os.SameFile(info, f.parentIdentity) {
		return false, ErrStageParentChanged
	}
	if err := f.checkOwnedName(); err != nil {
		return false, err
	}
	published, err := publish(f.temp, f.target, overwrite, renameNoReplace, os.Link, func(string) error {
		return f.removeOwnedName()
	})
	if published && err == nil {
		// A native rename consumed the name. A successful fallback already
		// removed it through the owned directory.
		f.name = ""
	}
	published, err = f.finishCommit(published, err, func() error { return syncDirectory(f.parent) })
	if f.name == "" {
		err = errors.Join(err, f.closeParent())
	}
	return published, err
}

func (f *Staged) finishCommit(published bool, publishErr error, syncDir func() error) (bool, error) {
	f.recordPublication(published)
	if publishErr != nil {
		return f.published, publishErr
	}
	return f.published, syncDir()
}

// recordPublication is monotonic, including when cleanup/durability fails next.
func (f *Staged) recordPublication(published bool) { f.published = f.published || published }

// publish exposes the namespace transition separately from trailing cleanup.
// Its narrow function arguments let tests check state/error ordering without
// emulating native permissions, ACLs, consoles or filesystem faults.
func publish(temp, target string, overwrite bool, noReplace func(string, string) error, link func(string, string) error, remove func(string) error) (bool, error) {
	if overwrite {
		if err := os.Rename(temp, target); err != nil {
			return false, err
		}
		return true, nil
	}
	err := noReplace(temp, target)
	if err == nil {
		return true, nil
	}
	if !unsupportedRename(err) {
		return false, noReplaceFailure(err)
	}
	if err = link(temp, target); err != nil {
		return false, noReplaceFailure(fmt.Errorf("publishing without replacing the destination failed: %w", err))
	}
	if err = remove(temp); err != nil {
		return true, err
	}
	return true, nil
}

func noReplaceFailure(err error) error {
	if errors.Is(err, fs.ErrExist) {
		return errors.Join(ErrDestinationExists, err)
	}
	return err
}

// Abort closes/removes only the owned temporary name, never the target.
func (f *Staged) Abort() error {
	return errors.Join(f.closeFile(), f.removeOwnedName(), f.closeParent())
}

func (f *Staged) closeFile() error {
	if f.closed {
		return nil
	}
	f.closed = true
	if err := f.Close(); err != nil {
		return fmt.Errorf("close staged file: %w", err)
	}
	return nil
}

func (f *Staged) checkOwnedName() error {
	info, err := f.parent.Lstat(f.name)
	if err != nil {
		return err
	}
	if f.fileIdentity != nil && !os.SameFile(info, f.fileIdentity) {
		return errors.New("staged filename no longer identifies the owned file")
	}
	return nil
}

func (f *Staged) removeOwnedName() error {
	if f.parent == nil || f.name == "" {
		return nil
	}
	err := f.checkOwnedName()
	if err == nil {
		err = f.parent.Remove(f.name)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove staged file: %w", err)
	}
	f.name = ""
	return nil
}

func (f *Staged) closeParent() error {
	if f.parent == nil {
		return nil
	}
	err := closeStageParent(f.parent)
	f.parent = nil
	return err
}

func closeStageParent(parent *os.Root) error {
	if err := parent.Close(); err != nil {
		return fmt.Errorf("close stage directory: %w", err)
	}
	return nil
}
