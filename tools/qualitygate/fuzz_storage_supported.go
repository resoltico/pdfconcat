// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// fuzzStorage owns disposable compiler, corpus and child temporary files.
// Failure inputs and event evidence remain outside this tree. Cleanup requires
// settled producers and the original directory identity; it is not a disk quota.
type fuzzStorage struct {
	path     string
	root     *os.Root
	identity os.FileInfo
	closed   bool
}

func newFuzzStorage(sourceRoot string) (*fuzzStorage, error) {
	parent, err := externalFuzzTemporaryParent(sourceRoot)
	if err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(parent, "qualitygate-fuzz-")
	if err != nil {
		return nil, fmt.Errorf("create owned fuzz storage: %w", err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(path))
	}
	identity, err := root.Stat(".")
	if err != nil {
		return nil, errors.Join(err, root.Close(), os.RemoveAll(path))
	}
	storage := &fuzzStorage{path: path, root: root, identity: identity}
	for _, name := range []string{"cache", "tmp"} {
		if err = root.Mkdir(name, dirMode); err != nil {
			return nil, errors.Join(fmt.Errorf("prepare owned fuzz storage: %w", err), storage.close())
		}
	}
	return storage, nil
}

func externalFuzzTemporaryParent(sourceRoot string) (string, error) {
	parent, err := filepath.Abs(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve fuzz temporary parent: %w", err)
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("resolve native fuzz temporary parent: %w", err)
	}
	sourceRoot, err = filepath.Abs(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve fuzz source root: %w", err)
	}
	sourceRoot, err = filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve native fuzz source root: %w", err)
	}
	relative, err := filepath.Rel(sourceRoot, parent)
	if err != nil {
		return "", err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: fuzz temporary parent %q is inside source root %q; set TMPDIR outside the repository", errGate, parent, sourceRoot)
	}
	return parent, nil
}

func (s *fuzzStorage) environment() []string {
	tmp := filepath.Join(s.path, "tmp")
	return []string{"GOCACHE=" + filepath.Join(s.path, "cache"), "GOCACHEPROG=", "GOTMPDIR=" + tmp, "TMPDIR=" + tmp, "TMP=" + tmp, "TEMP=" + tmp}
}

func (s *fuzzStorage) finish(result error) error {
	if errors.Is(result, errFuzzCleanupUncertain) {
		s.closed = true
		return errors.Join(result, fmt.Errorf("retain unsettled fuzz storage %q: %w", s.path, errFuzzCleanupUncertain), s.root.Close())
	}
	return errors.Join(result, s.close())
}

func (s *fuzzStorage) close() (result error) {
	if s.closed {
		return nil
	}
	s.closed = true
	defer func() { result = errors.Join(result, s.root.Close()) }()
	parent, err := os.OpenRoot(filepath.Dir(s.path))
	if err != nil {
		return fmt.Errorf("open fuzz storage parent: %w", err)
	}
	defer func() { result = errors.Join(result, parent.Close()) }()
	if err := s.verifyNamespace(parent); err != nil {
		return err
	}
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return fmt.Errorf("read owned fuzz storage %q: %w", s.path, err)
	}
	for _, entry := range entries {
		result = errors.Join(result, s.root.RemoveAll(entry.Name()))
	}
	if result != nil {
		return fmt.Errorf("clean owned fuzz storage %q: %w", s.path, result)
	}
	if err := s.verifyNamespace(parent); err != nil {
		return err
	}
	return parent.Remove(filepath.Base(s.path))
}

func (s *fuzzStorage) verifyNamespace(parent *os.Root) error {
	current, err := parent.Lstat(filepath.Base(s.path))
	if err != nil {
		return fmt.Errorf("inspect owned fuzz storage %q: %w", s.path, err)
	}
	if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(s.identity, current) {
		return fmt.Errorf("%w: owned fuzz storage %q was substituted; preserve unknown entry", errGate, s.path)
	}
	return nil
}
