// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// identityQuery is the one native metadata query used by private role helpers.
// Production entry points always supply identity; tests can exercise query errors
// after successful filesystem inspection without replacing global process state.
type identityQuery func(string) (objectID, error)

func identityError(operation, path string, err error) error {
	if err == nil {
		return nil
	}
	return &os.PathError{Op: operation, Path: path, Err: err}
}

type reservation struct {
	object  objectID
	missing []string
}

// reserve walks only ordinary absence. Dangling links and obstructed prefixes
// never become proof that a destination is separate.
func reserve(path string, query identityQuery) (reservation, error) {
	var tail []string
	for {
		info, err := os.Stat(path)
		if err == nil {
			if len(tail) > 0 && !info.IsDir() {
				return reservation{}, fmt.Errorf("non-directory input prefix: %s", path)
			}
			id, err := query(path)
			if err != nil {
				return reservation{}, err
			}
			slices.Reverse(tail)
			return reservation{object: id, missing: tail}, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return reservation{}, err
		}
		if _, linkErr := os.Lstat(path); linkErr == nil {
			return reservation{}, fmt.Errorf("unresolved input link: %s", path)
		} else if !errors.Is(linkErr, fs.ErrNotExist) {
			return reservation{}, linkErr
		}
		parent := filepath.Dir(path)
		if parent == path {
			return reservation{}, err
		}
		tail = append(tail, filepath.Base(path))
		path = parent
	}
}
func samePathOrFile(a, b string, query identityQuery) (bool, error) {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true, nil
	}
	first, err := os.Stat(a)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	second, err := os.Stat(b)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if first.Mode().Type() != second.Mode().Type() {
		return false, nil
	}
	x, err := query(a)
	if err != nil {
		return false, err
	}
	y, err := query(b)
	if err != nil {
		return false, err
	}
	return x == y, nil
}

// missingInputRelation returns established collision or unproved separation.
// A report parent must exist when the report is actually published.
func missingInputRelation(report string, input reservation, query identityQuery) (collision, unproved bool, err error) {
	parent := filepath.Dir(report)
	info, err := os.Stat(parent)
	if err != nil {
		return false, false, err
	}
	if !info.IsDir() {
		return false, false, fmt.Errorf("report parent is not a directory: %s", parent)
	}
	id, err := query(parent)
	if err != nil {
		return false, false, err
	}
	if id != input.object {
		return false, false, nil
	}
	if len(input.missing) == 1 && input.missing[0] == filepath.Base(report) {
		return true, false, nil
	}
	return false, true, nil
}
