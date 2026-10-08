// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"errors"
	"fmt"
	"os"
)

// RecoveryOwner keeps the originally created file object alive through recovery verification.
// Its Close must be called after the caller finishes verifying or refreshing a retained report.
type RecoveryOwner struct {
	file *os.File
}

var (
	errOwnerChanged  = errors.New("staged report path no longer names the owned file object")
	errOwnerReleased = errors.New("report identity pin has been released")
)

func pinReportOwner(writer *os.File, expected os.FileInfo) (*RecoveryOwner, error) {
	lease, err := openReportLease(writer.Name())
	if err != nil {
		return nil, fmt.Errorf("pin staged report: %w", err)
	}

	owner := &RecoveryOwner{file: lease}

	live, infoErr := owner.inspect()
	if infoErr != nil || !os.SameFile(expected, live) {
		return nil, errors.Join(errOwnerChanged, infoErr, owner.Close())
	}

	if verifyErr := owner.Verify(writer.Name()); verifyErr != nil {
		return nil, errors.Join(verifyErr, owner.Close())
	}

	return owner, nil
}

// Verify compares a current path with the live original object, preventing inode reuse from masquerading as ownership.
func (o *RecoveryOwner) Verify(path string) error {
	if o == nil || o.file == nil {
		return errOwnerReleased
	}

	live, err := o.inspect()
	if err != nil {
		return err
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect staged report namespace: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: staged report namespace is not a regular file", errOwnerChanged)
	}

	return checkReportNamespace(live, info, path)
}

// Close releases the identity pin; repeated calls are safe.
func (o *RecoveryOwner) Close() error {
	if o == nil || o.file == nil {
		return nil
	}

	file := o.file

	o.file = nil
	if err := file.Close(); err != nil {
		return fmt.Errorf("release report identity pin: %w", err)
	}

	return nil
}

func (o *RecoveryOwner) inspect() (os.FileInfo, error) {
	info, err := o.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("identify live report owner: %w", err)
	}

	return info, nil
}

func (s *Staged) releaseOwner() error { return s.owner.Close() }

// removeOwnedStage only unlinks the namespace entry while its original object is still pinned.
func (s *Staged) removeOwnedStage() error {
	if s.owner == nil {
		return errOwnerReleased
	}

	if err := s.owner.Verify(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	return removeStaged(s.path)
}

// CloseRecovery releases a late-result ownership pin without removing the retained report.
// Callers of Commit must call it after completing their recovery verification or repair.
func (r *Result) CloseRecovery() error { return r.RecoveryOwner.Close() }

// compareReportObjects reports a namespace mismatch consistently on every host.
func compareReportObjects(live, current os.FileInfo, path string) error {
	if !os.SameFile(live, current) {
		return fmt.Errorf("%w: %s", errOwnerChanged, path)
	}

	return nil
}
