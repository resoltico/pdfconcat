// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"fmt"
	"io/fs"
)

type (
	// ScratchError reports a failure to create, write or remove the job's private scratch files.
	ScratchError struct {
		// Err is the underlying operating-system error.
		Err error
		// Dir is the scratch directory involved.
		Dir string
		// Operation names the failed step, for example "create workspace" or "copy source".
		Operation string
	}

	// NotRegularFileError reports a source that is not a regular file once opened.
	NotRegularFileError struct {
		// Path is the path the caller named.
		Path string
		// Kind describes what the path resolved to, for example "a directory" or "a named pipe".
		Kind string
	}

	// SourceChangedError reports a source that was observed to change while it was being captured.
	SourceChangedError struct {
		// Path is the path the caller named.
		Path string
	}

	// SourceError wraps a failure to open, read or inspect a named source.
	SourceError struct {
		// Err is the underlying error.
		Err error
		// Path is the path the caller named.
		Path string
		// Operation names the failed step.
		Operation string
	}
)

// Error includes a repair action chosen from the kind of failure.
func (e *ScratchError) Error() string {
	return fmt.Sprintf("%s in %q: %v; %s", e.Operation, e.Dir, e.Err, scratchAdvice(e.Err))
}

// Unwrap exposes the underlying operating-system error.
func (e *ScratchError) Unwrap() error { return e.Err }

func scratchAdvice(err error) string {
	switch {
	case IsDiskFull(err):
		return "the scratch volume is full; free space (a job needs room for a copy of every distinct " +
			"source and font file) or choose an output location on a volume with more space"
	case errors.Is(err, fs.ErrPermission):
		return "the scratch directory is not writable; choose an output location you can write to"
	default:
		return "check that the output location is writable and the volume is healthy"
	}
}

// Error names the path and what it actually is.
func (e *NotRegularFileError) Error() string {
	return fmt.Sprintf("source %q is %s, not a regular file", e.Path, e.Kind)
}

// Error tells the caller to retry once the writer is finished.
func (e *SourceChangedError) Error() string {
	return fmt.Sprintf("source %q changed while it was being captured; finish writing it and run again", e.Path)
}

// Error names the path and the failed step.
func (e *SourceError) Error() string {
	return fmt.Sprintf("%s %q: %v", e.Operation, e.Path, e.Err)
}

// Unwrap exposes the underlying error.
func (e *SourceError) Unwrap() error { return e.Err }
