// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// Snapshot owns the bytes observed from a regular input. Large inputs live
// in a private spill file. Open returns a fresh reader for each admission.
type Snapshot struct {
	data     []byte
	path     string
	size     int64
	digest   [32]byte
	identity InputIdentity
}

// Size is the byte count observed during acquisition.
func (s *Snapshot) Size() int64 { return s.size }

// Digest is the SHA-256 of the acquired bytes.
func (s *Snapshot) Digest() [32]byte { return s.digest }

// Resident is the retained byte capacity charged to memory.
func (s *Snapshot) Resident() int64 { return int64(cap(s.data)) }

// Identity preserves the original descriptor identity for later alias checks.
func (s *Snapshot) Identity() InputIdentity { return s.identity }

// Open returns a fresh reader on the observed bytes.
func (s *Snapshot) Open() (io.ReadSeekCloser, error) {
	if s.path != "" {
		return os.Open(s.path)
	}
	if s.data == nil {
		return nil, os.ErrClosed
	}
	return memoryReader{bytes.NewReader(s.data)}, nil
}

type memoryReader struct{ *bytes.Reader }

func (memoryReader) Close() error { return nil }

// Close releases the acquired bytes or removes the private spill.
func (s *Snapshot) Close() error {
	s.data = nil
	if s.path == "" {
		return nil
	}
	err := os.Remove(s.path)
	if err == nil {
		s.path = ""
	}
	return err
}

// Acquire streams bytes and their digest in one pass. The memory allowance
// controls spill placement, never whether a source is accepted.
func Acquire(c context.Context, path string, memory int64) (_ *Snapshot, err error) {
	f, err := OpenRegular(c, path, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r := snapshotReader{c, f}
	s := &Snapshot{identity: InputIdentity{path, info}}
	hash := sha256.New()
	if size := info.Size(); size < memory {
		// One extra byte detects growth and moves the input to spill.
		s.data = make([]byte, size+1)
		n, e := io.ReadFull(r, s.data)
		s.data = s.data[:n]
		if e == io.EOF || e == io.ErrUnexpectedEOF {
			s.size, s.digest = int64(n), sha256.Sum256(s.data)
			return s, c.Err()
		}
		if e != nil {
			return nil, e
		}
	}
	spill, err := os.CreateTemp("", "pdfconcat-source-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("create private source snapshot: %w", err)
	}
	s.path = spill.Name()
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	// Keep SHA-256 and the snapshot in agreement, including when a write fails.
	n, writeErr := io.CopyBuffer(io.MultiWriter(spill, hash), io.MultiReader(bytes.NewReader(s.data), r), make([]byte, 128<<10))
	closeErr := spill.Close()
	if writeErr != nil {
		return nil, fmt.Errorf("stage private source snapshot: %w", writeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close private source snapshot: %w", closeErr)
	}
	s.data, s.size = nil, n
	copy(s.digest[:], hash.Sum(nil))
	return s, c.Err()
}

type snapshotReader struct {
	context context.Context
	reader  io.Reader
}

func (r snapshotReader) Read(p []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
