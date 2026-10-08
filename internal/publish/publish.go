// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package publish enforces output policy and final same-filesystem publication of the PDF and
// of the report.
//
// Atomic visibility: a destination is made visible by one native rename, so a reader sees the
// old file or the complete new one, never a partial file. Without overwrite the rename is the
// platform's no-clobber primitive (renameat2 RENAME_NOREPLACE on Linux, renamex_np RENAME_EXCL on
// macOS, MoveFileEx without MOVEFILE_REPLACE_EXISTING on Windows); there is no weaker fallback.
//
// Crash durability: staged file contents are flushed with fsync before the rename and the parent
// directory is flushed after it on Linux/macOS (macOS fsync issues F_FULLFSYNC). Windows
// flushes file contents but does not establish directory-entry crash durability. Filesystems
// that cannot flush a directory are tolerated.
// That is everything claimed: the operating system and storage hardware decide what survives a
// power loss, and nothing stronger than the platform's flush semantics is promised.
package publish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type (
	// DestinationError reports a destination or its directory that the output policy refuses.
	DestinationError struct {
		cause error
		// Subject names what was inspected, such as "output" or "output directory".
		Subject string
		// Path is the inspected path.
		Path string
		// Reason says why the path is refused.
		Reason string
	}

	// FinalizationError reports successful publication followed by a flush or owned-handle release failure.
	// The visible destination must never be described as untouched.
	FinalizationError struct {
		// Err is the operation-specific finalization failure.
		Err error
		// Path is the published destination.
		Path string
	}
)

// outputSubject names the destination file in a DestinationError.
const (
	outputSubject            = "output"
	publicationFailureFormat = "publish %q: %w"
)

var (
	// ErrPathRequired is returned when a staged or destination path is empty.
	ErrPathRequired = errors.New("staged and destination paths are required")
	// ErrOutputPathEmpty is returned when the output path is empty.
	ErrOutputPathEmpty = errors.New("output path is empty")
)

// Error names the subject, the path and the reason.
func (e *DestinationError) Error() string {
	return fmt.Sprintf("%s %q %s", e.Subject, e.Path, e.Reason)
}

// Unwrap distinguishes a refused existing target from unrelated destination faults.
func (e *DestinationError) Unwrap() error { return e.cause }

// Error states both facts so a caller never describes the output as untouched.
func (e *FinalizationError) Error() string {
	return fmt.Sprintf("published %q, but finalizing publication failed: %v", e.Path, e.Err)
}

// Unwrap exposes the operation-specific finalization failure.
func (e *FinalizationError) Unwrap() error { return e.Err }

// File exposes a verified staged file using platform-native rename semantics. The staged file is
// flushed first; Linux/macOS also flush the destination directory afterwards. A *FinalizationError means the file is
// published.
func File(staged, destination string, overwrite bool) error {
	return commitFile(realOperations(), staged, destination, Policy{Overwrite: overwrite}.existing())
}

func commitFile(ops operations, staged, destination string, existing existingFile) error {
	return commitFileChecked(context.Background(), ops, staged, destination, existing, nil)
}

func commitFileChecked(ctx context.Context, ops operations, staged, destination string, existing existingFile, verify func() error) error {
	if staged == "" || destination == "" {
		return ErrPathRequired
	}

	err := validateDestination(destination, existing)
	if err != nil {
		return err
	}

	err = ops.syncFile(staged)
	if err != nil {
		return fmt.Errorf("flush staged %q: %w", staged, err)
	}

	if verify != nil {
		if err = verify(); err != nil {
			return fmt.Errorf("verify publication: %w", err)
		}
	}

	if policyErr := ensureDestinationPolicy(destination, existing); policyErr != nil {
		return policyErr
	}

	if err = ctx.Err(); err != nil {
		return fmt.Errorf(publicationFailureFormat, destination, err)
	}

	err = ops.replace(staged, destination, existing)
	if err != nil {
		return fmt.Errorf(publicationFailureFormat, destination, err)
	}

	err = ops.syncDirectory(filepath.Dir(destination))
	if err != nil {
		return &FinalizationError{Path: destination, Err: fmt.Errorf("flush publication directory: %w", err)}
	}

	return nil
}

// CheckDestination validates the destination directory and current overwrite policy.
func CheckDestination(destination string, overwrite bool) error {
	return validateDestination(destination, Policy{Overwrite: overwrite}.existing())
}

func validateDestination(destination string, existing existingFile) error {
	if destination == "" {
		return ErrOutputPathEmpty
	}

	dir := filepath.Dir(destination)

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("output directory %q: %w", dir, err)
	}

	if !info.IsDir() {
		return &DestinationError{Subject: "output directory", Path: dir, Reason: "is not a directory"}
	}

	return ensureDestinationPolicy(destination, existing)
}

func ensureDestinationPolicy(destination string, existing existingFile) error {
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect output %q: %w", destination, err)
	}

	return checkExistingDestination(destination, info, existing)
}

// checkExistingDestination decides whether an existing destination may be replaced.
func checkExistingDestination(destination string, info os.FileInfo, existing existingFile) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return &DestinationError{Subject: outputSubject, Path: destination, Reason: "is a symbolic link; choose a regular-file path"}
	case info.IsDir():
		return &DestinationError{Subject: outputSubject, Path: destination, Reason: "is a directory"}
	case !info.Mode().IsRegular():
		return &DestinationError{Subject: outputSubject, Path: destination, Reason: "is not a regular file"}
	case existing == refuseExisting:
		return &DestinationError{
			Subject: outputSubject, Path: destination,
			Reason: "already exists; choose an unused target", cause: os.ErrExist,
		}
	default:
		return nil
	}
}
