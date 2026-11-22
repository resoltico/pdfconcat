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
	target, temp string
	creationMode fs.FileMode
	mode         fs.FileMode
	published    bool
	closed       bool
}

// ErrDestinationExists identifies rejection by an atomic no-replace operation.
var ErrDestinationExists = errors.New("publication destination exists")

// Create uses conventional permissions (0666 minus umask). If an incumbent
// exists, the empty stage adopts its permissions before any bytes are written.
func Create(target string) (*Staged, error) {
	var mode fs.FileMode
	existing, err := os.Lstat(target)
	if err == nil {
		if !existing.Mode().IsRegular() {
			return nil, fmt.Errorf("target is not a regular file: %s", target)
		}
		mode = existing.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// Keep the sibling name independent of the target's filesystem name budget.
	temp := filepath.Join(filepath.Dir(target), ".pdfconcat."+rand.Text()+".tmp")
	f, err := os.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return nil, err
	}
	result := &Staged{File: f, target: target, temp: temp}
	info, err := f.Stat()
	if err != nil {
		_ = result.Abort()
		return nil, err
	}
	result.creationMode = info.Mode().Perm()
	result.mode = result.creationMode
	if existing != nil {
		if err = result.setMode(mode); err != nil {
			_ = result.Abort()
			return nil, err
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

// Path identifies the owned temporary file.
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
	err := f.Close()
	f.closed = true
	if err != nil {
		return false, err
	}
	published, err := publish(f.temp, f.target, overwrite, renameNoReplace, os.Link, os.Remove)
	return f.finishCommit(published, err, func() error { return syncDirectory(filepath.Dir(f.target)) })
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
	var closeErr error
	if !f.closed {
		closeErr = f.Close()
		f.closed = true
	}
	err := os.Remove(f.temp)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return errors.Join(closeErr, err)
}
