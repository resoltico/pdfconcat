// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package publication publishes a sibling temporary file without exposing partial bytes.
package publication

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File owns its temporary name. Published state is independent of later errors.
type File struct {
	*os.File
	target, temp string
	published    bool
	closed       bool
}

// Create uses conventional permissions (0666 minus umask). Replacing an existing
// regular file preserves its mode; symlinks and other nonregular targets are refused.
func Create(target string) (*File, error) {
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
	temp := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+"."+rand.Text()+".tmp")
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return nil, err
	}
	result := &File{File: f, target: target, temp: temp}
	if existing != nil {
		if err = f.Chmod(mode); err != nil {
			_ = result.Abort()
			return nil, err
		}
	}
	return result, nil
}

// Path identifies the owned temporary file, for verification before publication.
func (f *File) Path() string { return f.temp }

// Published stays true if rename/link succeeded even when Commit also failed.
func (f *File) Published() bool { return f.published }

// Commit flushes, closes, publishes, then syncs the containing directory.
func (f *File) Commit(overwrite bool) (bool, error) {
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

func (f *File) finishCommit(published bool, publishErr error, syncDir func() error) (bool, error) {
	f.recordPublication(published)
	if publishErr != nil {
		return f.published, publishErr
	}
	return f.published, syncDir()
}

// recordPublication is monotonic, including when cleanup/durability fails next.
func (f *File) recordPublication(published bool) { f.published = f.published || published }

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
		return false, err
	}
	if err = link(temp, target); err != nil {
		return false, fmt.Errorf("no-replace rename and link publication are unavailable; consider --overwrite: %w", err)
	}
	if err = remove(temp); err != nil {
		return true, err
	}
	return true, nil
}

// Abort closes/removes only the owned temporary name, never the target.
func (f *File) Abort() error {
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
