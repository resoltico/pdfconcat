// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type testEvidence struct {
	stdout   *os.File
	stderr   *os.File
	path     string
	explicit bool
}

// openTestEvidence streams complete JSON events and separate process stderr outside source files.
func openTestEvidence(requested string) (*testEvidence, error) {
	var (
		file *os.File
		err  error
	)
	if requested == "" {
		file, err = os.CreateTemp("", "qualitygate-test-events-*.jsonl")
	} else {
		requested, err = filepath.Abs(requested)
		if err != nil {
			return nil, fmt.Errorf("resolve event evidence path: %w", err)
		}

		file, err = createEvidenceFile(requested)
	}

	if err != nil {
		return nil, fmt.Errorf("create test event evidence: %w", err)
	}

	stderr, err := os.OpenFile(file.Name()+".stderr", os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create test stderr evidence: %w", err), file.Close())
	}

	return &testEvidence{stdout: file, stderr: stderr, path: file.Name(), explicit: requested != ""}, nil
}

func (e *testEvidence) closeFiles() error {
	return errors.Join(e.stdout.Close(), e.stderr.Close())
}

func (e *testEvidence) finish(result error) {
	if e.explicit || result != nil {
		log.Printf("test: complete JSON event evidence: %s; process stderr: %s.stderr", e.path, e.path)
		return
	}

	removeAll(e.path)
	removeAll(e.path + ".stderr")
}

// createEvidenceFile confines creation to the selected evidence directory without overwriting files.
func createEvidenceFile(path string) (*os.File, error) {
	directory, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open event evidence directory: %w", err)
	}
	defer closeLogged(directory)

	file, err := directory.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return nil, fmt.Errorf("create event evidence file: %w", err)
	}

	return file, nil
}
