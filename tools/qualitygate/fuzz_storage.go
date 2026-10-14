// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// fuzzStorage owns disposable compiler, interesting-corpus and child temporary
// files for one campaign. Go writes failing inputs to the source package's
// testdata/fuzz directory; those and event evidence are outside this tree.
// Lifetime cleanup prevents accumulation, not unlimited growth during a command.
type fuzzStorage struct{ path string }

func newFuzzStorage() (*fuzzStorage, error) {
	parent, err := filepath.Abs(os.TempDir())
	if err != nil {
		return nil, fmt.Errorf("resolve fuzz temporary parent: %w", err)
	}
	path, err := os.MkdirTemp(parent, "qualitygate-fuzz-")
	if err != nil {
		return nil, fmt.Errorf("create owned fuzz storage: %w", err)
	}
	for _, name := range []string{"cache", "tmp"} {
		if err = os.Mkdir(filepath.Join(path, name), dirMode); err != nil {
			return nil, errors.Join(fmt.Errorf("prepare owned fuzz storage: %w", err), os.RemoveAll(path))
		}
	}
	return &fuzzStorage{path: path}, nil
}

func (s *fuzzStorage) environment() []string {
	tmp := filepath.Join(s.path, "tmp")
	return []string{"GOCACHE=" + filepath.Join(s.path, "cache"), "GOCACHEPROG=", "GOTMPDIR=" + tmp, "TMPDIR=" + tmp, "TMP=" + tmp, "TEMP=" + tmp}
}

func (s *fuzzStorage) close() error {
	if err := os.RemoveAll(s.path); err != nil {
		return fmt.Errorf("remove owned fuzz storage: %w", err)
	}
	return nil
}
