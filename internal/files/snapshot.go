// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// Snapshot owns the bytes observed from a regular input. Large inputs live
// in a private spill file. Open returns a fresh reader for each admission.
type Snapshot struct {
	data      []byte
	spillRoot *os.Root
	spillName string
	size      int64
	digest    [32]byte
	identity  InputIdentity
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
	if s.spillRoot != nil {
		reader, err := s.spillRoot.Open(s.spillName)
		if err != nil {
			return nil, scratchError("reopen private source snapshot", err)
		}
		return spillReader{reader}, nil
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
	if s.spillRoot == nil {
		return nil
	}
	root, name := s.spillRoot, s.spillName
	s.spillRoot, s.spillName = nil, ""
	var removeErr error
	if name != "" {
		removeErr = root.Remove(name)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
	}
	return errors.Join(scratchError("remove private source snapshot", removeErr),
		scratchError("close private source snapshot directory", root.Close()))
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
	s.spillRoot, err = os.OpenRoot(os.TempDir())
	if err != nil {
		return nil, scratchError("create private source snapshot", err)
	}
	defer func() {
		if err != nil {
			if closeErr := s.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	name := "pdfconcat-source-" + rand.Text() + ".pdf"
	spill, err := s.spillRoot.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, scratchError("create private source snapshot", err)
	}
	s.spillName = name
	// Distinguish input read errors from scratch writes, even when the system
	// reports the same native cause. Hash exactly the bytes written to the spill.
	n, copyErr := io.CopyBuffer(io.MultiWriter(spillWriter{spill}, hash), io.MultiReader(bytes.NewReader(s.data), r), make([]byte, 128<<10))
	closeErr := scratchError("close private source snapshot", spill.Close())
	if copyErr != nil || closeErr != nil {
		return nil, errors.Join(copyErr, closeErr)
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

func scratchError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &diag.SourceScratchError{Cause: fmt.Errorf("%s: %w", operation, err)}
}

type spillReader struct{ *os.File }

func (r spillReader) Close() error {
	return scratchError("close private source snapshot reader", r.File.Close())
}

type spillWriter struct{ *os.File }

func (w spillWriter) Write(data []byte) (int, error) {
	n, err := w.File.Write(data)
	return n, scratchError("stage private source snapshot", err)
}
