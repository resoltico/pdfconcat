// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"io"

	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

// assemblySourceSet retains small repeated snapshots within the operation's
// memory allowance. Large or evicted sources spill for a single admission so
// private disk space scales with the largest source rather than the packet.
// Every reacquisition is checked against the admitted source digest.
type assemblySourceSet struct {
	paths map[string]*assemblySource
	free  int64
}

type assemblySource struct {
	owner  *assemblySourceSet
	path   string
	left   int
	cached *files.Snapshot
}

func assemblySources(parts []pdf.Part) *assemblySourceSet {
	sources := &assemblySourceSet{paths: make(map[string]*assemblySource), free: 16 << 20}
	for i := range parts {
		if path := parts[i].Source; path != "" {
			source := sources.paths[path]
			if source == nil {
				source = &assemblySource{owner: sources, path: path}
				sources.paths[path] = source
			}
			source.left++
			parts[i].Input = source
		}
	}
	return sources
}

func (s *assemblySourceSet) close() {
	for _, source := range s.paths {
		if source.cached != nil {
			_ = source.cached.Close()
		}
	}
}

func (s *assemblySource) Open(ctx context.Context) (io.ReadSeekCloser, [32]byte, error) {
	var zero [32]byte
	snapshot := s.cached
	if snapshot == nil {
		var err error
		snapshot, err = files.Acquire(ctx, s.path, min(s.owner.free, sourceMemoryThreshold))
		if err != nil {
			return nil, zero, err
		}
	}
	s.left--
	keep := s.left > 0 && snapshot.Resident() > 0
	reader, err := snapshot.Open()
	if err != nil {
		if s.cached == nil {
			_ = snapshot.Close()
		}
		return nil, zero, err
	}
	if keep {
		if s.cached == nil {
			s.owner.free -= snapshot.Resident()
			s.cached = snapshot
		}
		return reader, snapshot.Digest(), nil
	}
	if s.cached != nil {
		s.owner.free += snapshot.Resident()
		s.cached = nil
	}
	return sourceLease{reader, snapshot}, snapshot.Digest(), nil
}

type sourceLease struct {
	io.ReadSeekCloser
	snapshot *files.Snapshot
}

func (s sourceLease) Close() error {
	return errors.Join(s.ReadSeekCloser.Close(), s.snapshot.Close())
}
