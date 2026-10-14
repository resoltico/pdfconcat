// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/capture"
)

// campaignScratch owns a disposable campaign directory through a live root.
// Its caller must prove producer termination before requesting removal.
type campaignScratch struct {
	path     string
	root     *os.Root
	identity os.FileInfo
	closed   bool
}

func newCampaignScratch(sourceRoot, prefix string) (*campaignScratch, error) {
	parent, err := externalCampaignTemporaryParent(sourceRoot)
	if err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return nil, fmt.Errorf("create owned campaign scratch: %w", err)
	}
	return openCampaignScratch(path)
}

func openCampaignScratch(path string) (*campaignScratch, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("retain unverified campaign scratch %q after opening failed: %w", path, err)
	}
	identity, err := root.Stat(".")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("retain unverified campaign scratch %q after identity inspection failed: %w", path, err), root.Close())
	}
	return &campaignScratch{path: path, root: root, identity: identity}, nil
}

func externalCampaignTemporaryParent(sourceRoot string) (string, error) {
	parent, err := nativeCampaignDirectory(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve native campaign temporary parent: %w", err)
	}
	sourceRoot, err = nativeCampaignDirectory(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve native campaign source root: %w", err)
	}
	// Different physical Windows volumes cannot be descendants of each other.
	if !strings.EqualFold(filepath.VolumeName(sourceRoot), filepath.VolumeName(parent)) {
		return parent, nil
	}
	relative, err := filepath.Rel(sourceRoot, parent)
	if err != nil {
		return "", err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: campaign temporary parent %q is inside source root %q; set TMPDIR (macOS/Linux) or TMP/TEMP (Windows) outside the repository", errGate, parent, sourceRoot)
	}
	return parent, nil
}

func (s *campaignScratch) retain() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.root.Close()
}

func (s *campaignScratch) close() (result error) {
	if s.closed {
		return nil
	}
	s.closed = true
	closeRootOnReturn := true
	defer func() {
		if closeRootOnReturn {
			result = errors.Join(result, s.root.Close())
		}
	}()
	parent, err := os.OpenRoot(filepath.Dir(s.path))
	if err != nil {
		return fmt.Errorf("open campaign scratch parent: %w", err)
	}
	defer func() { result = errors.Join(result, parent.Close()) }()
	if err := s.verifyNamespace(parent); err != nil {
		return err
	}
	for {
		directory, err := s.root.Open(".")
		if err != nil {
			return fmt.Errorf("open owned campaign scratch %q: %w", s.path, err)
		}
		entries, readErr := directory.ReadDir(64)
		closeErr := directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.Join(fmt.Errorf("read owned campaign scratch %q: %w", s.path, readErr), closeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if len(entries) == 0 {
			break
		}
		// Restart each bounded read after removing its batch: no directory
		// offset is relied on while entries are removed, and Windows has no
		// outstanding enumeration handle during removal.
		for _, entry := range entries {
			result = errors.Join(result, s.root.RemoveAll(entry.Name()))
		}
		if result != nil {
			return fmt.Errorf("clean owned campaign scratch %q: %w", s.path, result)
		}
	}
	// Windows OpenRoot retains a directory handle without delete sharing.
	// Close our settled producer's own lease before the final name check and
	// removal; keep the parent lease and original identity for that check.
	closeErr := s.root.Close()
	closeRootOnReturn = false
	if closeErr != nil {
		return fmt.Errorf("close owned campaign scratch %q before removal: %w", s.path, closeErr)
	}
	if err := s.verifyNamespace(parent); err != nil {
		return err
	}
	return parent.Remove(filepath.Base(s.path))
}

func (s *campaignScratch) verifyNamespace(parent *os.Root) error {
	current, err := parent.Lstat(filepath.Base(s.path))
	if err != nil {
		return fmt.Errorf("inspect owned campaign scratch %q: %w", s.path, err)
	}
	if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(s.identity, current) {
		return fmt.Errorf("%w: owned campaign scratch %q was substituted; preserve unknown entry", errGate, s.path)
	}
	return nil
}

// Resolve native traversal before lexical operations: link/.. must select the
// same directory as the operating system, including native repository aliases.
func nativeCampaignDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		caller, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = caller + string(filepath.Separator) + path
	}
	bound, err := capture.NativeArtifactPath(path + string(filepath.Separator) + "campaign-directory-anchor")
	if err != nil {
		return "", err
	}
	qualified := capture.NativeParent(bound)
	if runtime.GOOS != windowsOS {
		return qualified, nil
	}
	// Go's embed loader quotes the '?' in the extended Windows prefix as a
	// glob metacharacter. Select the native directory first, then qualify an
	// ordinary DOS/UNC spelling against its actual filesystem identity.
	ordinary := qualified
	switch {
	case len(qualified) >= 8 && strings.EqualFold(qualified[:8], `\\?\UNC\`):
		ordinary = `\\` + qualified[8:]
	case strings.HasPrefix(qualified, `\\?\`) && len(qualified) >= 7 && qualified[5] == ':' && qualified[6] == '\\':
		ordinary = qualified[4:]
	case strings.HasPrefix(qualified, `\\?\`):
		return "", fmt.Errorf("%w: native directory %q has no qualified Go workspace spelling", errGate, qualified)
	}
	selected, selectErr := os.Stat(qualified)
	candidate, candidateErr := os.Stat(ordinary)
	if selectErr != nil || candidateErr != nil || !selected.IsDir() || !candidate.IsDir() || !os.SameFile(selected, candidate) {
		return "", errors.Join(fmt.Errorf("%w: ordinary Go workspace spelling %q does not identify native directory %q", errGate, ordinary, qualified), selectErr, candidateErr)
	}
	return ordinary, nil
}
