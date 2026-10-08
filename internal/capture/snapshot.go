// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Captured describes one private copy of a source or font file.
type Captured struct {
	// Path is the private copy inside the workspace; later stages read only this file.
	Path string
	// SourcePath is the path that was named, as given.
	SourcePath string
	// Size is the number of bytes copied.
	Size int64
	// SHA256 is the digest of the bytes copied, computed during the single copy pass.
	SHA256 [sha256.Size]byte
	// Source is the filesystem identity of the original file (symbolic links resolved).
	Source Identity
}

// copyChunkSize bounds the buffer of one copy; cancellation is checked between chunks.
const (
	copyChunkSize        = 256 << 10
	captureFailureFormat = "capture %q: %w"
)

// Digest returns the SHA-256 digest as lowercase hexadecimal.
func (c Captured) Digest() string { return hex.EncodeToString(c.SHA256[:]) }

// Snapshot copies the regular file at path into the workspace once, never modifying the
// original. Symbolic links are followed. The file is opened without blocking and checked for
// being a regular file on the open handle, so a FIFO or device substituted after any earlier stat
// cannot hang the call. The copy is cancellable between chunks and fails with a
// *SourceChangedError when the size or modification time moves while it is read. No descriptor
// outlives the call, and partial copies are removed on failure.
func (w *Workspace) Snapshot(ctx context.Context, path string) (Captured, error) {
	err := ctx.Err()
	if err != nil {
		return Captured{}, fmt.Errorf(captureFailureFormat, path, err)
	}

	source, err := openSource(path)
	if err != nil {
		return Captured{}, &SourceError{Path: path, Operation: "open source", Err: err}
	}

	captured, err := w.snapshotOpen(ctx, source, path)

	err = errors.Join(err, closeSource(source, path))
	if err != nil {
		return Captured{}, errors.Join(err, removeScratch(captured.Path))
	}

	return captured, nil
}

// closeSource closes the source handle, naming the path on failure.
func closeSource(source *os.File, path string) error {
	err := source.Close()
	if err != nil {
		return &SourceError{Path: path, Operation: "close source", Err: err}
	}

	return nil
}

// snapshotOpen copies the opened source and verifies it did not change. On failure the returned
// Captured still names the scratch path so the caller can remove a partial copy.
func (w *Workspace) snapshotOpen(ctx context.Context, source *os.File, path string) (Captured, error) {
	before, err := source.Stat()
	if err != nil {
		return Captured{}, &SourceError{Path: path, Operation: "inspect source", Err: err}
	}

	if !before.Mode().IsRegular() {
		return Captured{}, &NotRegularFileError{Path: path, Kind: describeMode(before.Mode())}
	}

	identity, err := identityOfOpen(source, before)
	if err != nil {
		return Captured{}, &SourceError{Path: path, Operation: "identify source", Err: err}
	}

	captured := Captured{Path: w.NewPath(""), SourcePath: path, Source: identity}

	err = w.copyStream(ctx, source, &captured)
	if err == nil {
		err = verifyUnchanged(source, before, &captured)
	}

	return captured, err
}

// removeScratch removes a scratch file, treating one that was never created as removed.
func removeScratch(path string) error {
	if path == "" {
		return nil
	}

	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &ScratchError{Dir: filepath.Dir(path), Operation: "remove partial copy", Err: err}
	}

	return nil
}

// copyStream copies source into captured.Path and fills Size and SHA256.
func (w *Workspace) copyStream(ctx context.Context, source io.Reader, captured *Captured) error {
	path := captured.SourcePath

	scratch, err := w.openScratch(captured.Path)
	if err != nil {
		return &ScratchError{Dir: w.dir, Operation: "create scratch copy of " + path, Err: err}
	}

	digest := sha256.New()
	copyErr := w.copyChunks(ctx, source, io.MultiWriter(scratch, digest), captured)
	closeErr := scratch.Close()

	if copyErr != nil {
		return copyErr
	}

	if closeErr != nil {
		return &ScratchError{Dir: w.dir, Operation: "finish scratch copy of " + path, Err: closeErr}
	}

	copy(captured.SHA256[:], digest.Sum(nil))

	return nil
}

// copyChunks moves bytes from source to sink in bounded chunks, counting them in
// captured.Size and checking cancellation before each read.
func (w *Workspace) copyChunks(ctx context.Context, source io.Reader, sink io.Writer, captured *Captured) error {
	path := captured.SourcePath
	buffer := make([]byte, copyChunkSize)

	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf(captureFailureFormat, path, err)
		}

		count, readErr := source.Read(buffer)
		if count > 0 {
			err = w.storeChunk(sink, buffer[:count], captured)
			if err != nil {
				return err
			}
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}

		if readErr != nil {
			return &SourceError{Path: path, Operation: "read source", Err: readErr}
		}
	}
}

// storeChunk writes one chunk to the sink (the scratch copy and the digest) and records its size.
func (w *Workspace) storeChunk(sink io.Writer, chunk []byte, captured *Captured) error {
	_, err := sink.Write(chunk)
	if err != nil {
		return &ScratchError{Dir: w.dir, Operation: "copy " + captured.SourcePath, Err: err}
	}

	captured.Size += int64(len(chunk))

	if w.afterChunk != nil {
		w.afterChunk(captured.Size)
	}

	return nil
}

// verifyUnchanged compares the handle's size and modification time with the values seen at open
// and with the number of bytes actually read.
func verifyUnchanged(source *os.File, before fs.FileInfo, captured *Captured) error {
	after, err := source.Stat()
	if err != nil {
		return &SourceError{Path: captured.SourcePath, Operation: "re-inspect source", Err: err}
	}

	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || captured.Size != before.Size() {
		return &SourceChangedError{Path: captured.SourcePath}
	}

	return nil
}

func describeMode(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeDevice != 0:
		return "a device"
	default:
		return "an irregular file"
	}
}

// OpenRegular opens a named input without blocking on a FIFO and verifies the opened object.
// Symbolic links to regular files are accepted. The caller owns the returned handle.
func OpenRegular(path string) (*os.File, error) {
	file, err := openSource(path)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = &NotRegularFileError{Path: path, Kind: describeMode(info.Mode())}
	}

	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	return file, nil
}
