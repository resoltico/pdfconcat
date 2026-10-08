// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/capture"
)

type (
	// stagingWriter sends each bounded chunk to disk immediately, retaining no content.
	stagingWriter struct {
		file        io.Writer
		failure     error
		interrupted func() error
		ops         operations
		remaining   int64
		size        int64
	}

	// Policy says how an existing destination is treated.
	Policy struct {
		// Overwrite allows replacing an existing regular file.
		Overwrite bool
		// NoClobberOnly forces creation of a new file only and ignores Overwrite. Failure reports
		// written before every input is known use it, because an existing file at that path might be
		// an input the malformed job named.
		NoClobberOnly bool
	}

	// SizeLimitError reports staged content larger than its limit.
	SizeLimitError struct {
		// Target is the destination the content was staged for.
		Target string
		// Limit is the maximum number of bytes.
		Limit int64
	}

	// StageError reports a failure to write the staged file beside its target.
	StageError struct {
		// Err is the underlying error.
		Err error
		// Target is the destination the content was staged for.
		Target string
	}

	// Staged is complete, flushed, owner-only content (0600 on Unix; protected owner ACL on Windows)
	// waiting beside its target. Publish, Discard or a retained recovery file ends its life; Discard after any is
	// safe.
	Staged struct {
		// Verify checks protected identities immediately before publication.
		Verify     func() error
		cleanupErr error
		owner      *RecoveryOwner
		ops        operations
		path       string
		target     string
		size       int64
		settled    bool
	}
)

// stageChunkSize bounds the buffer of one staging copy; cancellation is checked between chunks.
const stageChunkSize = 256 << 10

// ErrNotAbsolute is returned when a path to stage beside is not absolute.
var ErrNotAbsolute = errors.New("path must be absolute")

// existing is how publication treats a file already at the destination.
func (p Policy) existing() existingFile {
	if p.Overwrite && !p.NoClobberOnly {
		return replaceExisting
	}

	return refuseExisting
}

// Preflight checks, without writing, that target's directory exists and that policy allows
// creating or replacing target now. It catches most failures before the PDF is committed; the
// check is repeated at publication because the filesystem can change in between.
func Preflight(target string, policy Policy) error {
	return validateDestination(target, policy.existing())
}

// Error names the destination and the limit.
func (e *SizeLimitError) Error() string {
	return fmt.Sprintf("content for %q exceeds the %d byte limit", e.Target, e.Limit)
}

// Error includes an actionable hint for full volumes and permission failures.
func (e *StageError) Error() string {
	hint := ""

	switch {
	case capture.IsDiskFull(e.Err):
		hint = "; the volume is full: free space or choose another location"
	case errors.Is(e.Err, os.ErrPermission):
		hint = "; the directory is not writable: choose a location you can write to"
	default:
		// Other failures need no extra hint.
	}

	return fmt.Sprintf("stage file beside %q: %v%s", e.Target, e.Err, hint)
}

// Unwrap exposes the underlying error.
func (e *StageError) Unwrap() error { return e.Err }

// Path returns the staged file's absolute path.
func (s *Staged) Path() string { return s.path }

// Target returns the destination the content will be published to.
func (s *Staged) Target() string { return s.target }

// Size returns the number of staged bytes.
func (s *Staged) Size() int64 { return s.size }

// CleanupError reports owner-release failure after visible publication; it never changes publication success.
func (s *Staged) CleanupError() error { return s.cleanupErr }

// Stage writes src to a new private file beside target (same filesystem, so publication is a
// rename), flushes it, and fails with *SizeLimitError when src holds more than maxBytes. The
// content is copied cancellably. target must be absolute and its directory must exist.
// On failure owned partial content is removed; an unverified or substituted namespace entry is preserved.
func Stage(ctx context.Context, target string, src io.Reader, maxBytes int64) (*Staged, error) {
	return stageWith(ctx, realOperations(), target, src, maxBytes)
}

// StageProduced streams content directly into the owned private staging file. produce must
// return after writing and must not retain the writer. Writes are bounded and cancellable;
// producer, close and flush failures discard only content whose identity remains owned.
func StageProduced(ctx context.Context, target string, maxBytes int64, produce func(io.Writer) error) (*Staged, error) {
	return stageProducedWith(ctx, realOperations(), target, maxBytes, produce)
}

func stageWith(ctx context.Context, ops operations, target string, src io.Reader, maxBytes int64) (*Staged, error) {
	return stageContentWith(ctx, ops, target, func(file *os.File) (int64, error) {
		return copyChunks(ctx, ops, file, src, maxBytes)
	})
}

func stageProducedWith(ctx context.Context, ops operations, target string, maxBytes int64, produce func(io.Writer) error) (*Staged, error) {
	return stageContentWith(ctx, ops, target, func(file *os.File) (int64, error) {
		writer := &stagingWriter{interrupted: ctx.Err, ops: ops, file: file, remaining: maxBytes}
		err := produce(writer)

		return writer.size, errors.Join(err, writer.failure, ctx.Err())
	})
}

func (w *stagingWriter) Write(content []byte) (int, error) {
	written := 0

	for len(content) > 0 {
		if w.failure != nil {
			return written, w.failure
		}

		if err := w.interrupted(); err != nil {
			w.failure = err
			return written, fmt.Errorf("write interrupted: %w", err)
		}

		count := min(len(content), stageChunkSize)
		if int64(count) > w.remaining {
			w.failure = &SizeLimitError{Limit: w.size + w.remaining}
			return written, w.failure
		}

		if err := w.ops.writeChunk(w.file, content[:count]); err != nil {
			w.failure = err
			return written, err
		}

		w.size += int64(count)
		w.remaining -= int64(count)
		written += count
		content = content[count:]
	}

	return written, nil
}

func stageContentWith(ctx context.Context, ops operations, target string, produce func(*os.File) (int64, error)) (*Staged, error) {
	if !filepath.IsAbs(target) {
		return nil, fmt.Errorf("%w: %q", ErrNotAbsolute, target)
	}

	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("stage %q: %w", target, err)
	}

	file, err := ops.createTemp(filepath.Dir(target), ".pdfconcat-report-*")
	if err != nil {
		return nil, &StageError{Target: target, Err: err}
	}

	info, identityErr := file.Stat()
	if identityErr != nil {
		return nil, &StageError{Target: target, Err: errors.Join(identityErr, file.Close())}
	}

	owner, pinErr := pinReportOwner(file, info)
	if pinErr != nil {
		// The writer still pins the original object; do not remove a substituted namespace entry.
		original := &RecoveryOwner{file: file}
		owned := &Staged{path: file.Name(), owner: original}
		cleanupErr := owned.removeOwnedStage()

		return nil, &StageError{Target: target, Err: errors.Join(pinErr, cleanupErr, original.Close())}
	}

	size, err := finishStaged(file, produce)
	if err == nil {
		err = ops.syncFile(file.Name())
	}

	if err != nil {
		failed := &Staged{path: file.Name(), owner: owner}
		err = errors.Join(err, failed.removeOwnedStage(), owner.Close())

		tooLarge, isTooLarge := errors.AsType[*SizeLimitError](err)
		if isTooLarge {
			tooLarge.Target = target

			return nil, err
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("stage %q: %w", target, err)
		}

		return nil, &StageError{Target: target, Err: err}
	}

	return &Staged{ops: ops, path: file.Name(), target: target, size: size, owner: owner}, nil
}

// finishStaged preserves independent production and close failures.
func finishStaged(file *os.File, produce func(*os.File) (int64, error)) (int64, error) {
	size, err := produce(file)

	closeErr := file.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close: %w", closeErr)
	}

	err = errors.Join(err, closeErr)

	return size, err
}

func copyChunks(ctx context.Context, ops operations, dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	buffer := make([]byte, stageChunkSize)

	var size int64

	for {
		err := ctx.Err()
		if err != nil {
			return 0, fmt.Errorf("copy interrupted: %w", err)
		}

		count, readErr := src.Read(buffer)
		if count > 0 {
			size += int64(count)
			if size > maxBytes {
				return 0, &SizeLimitError{Limit: maxBytes}
			}

			err = ops.writeChunk(dst, buffer[:count])
			if err != nil {
				return 0, fmt.Errorf("write: %w", err)
			}
		}

		if errors.Is(readErr, io.EOF) {
			return size, nil
		}

		if readErr != nil {
			return 0, fmt.Errorf("read content: %w", readErr)
		}
	}
}

// Publish makes the staged file visible at its target under policy, honoring cancellation until
// the rename starts. The staged file is removed on every failure. Failures the caller can act
// on: an existing target without overwrite, a symbolic-link or non-regular target, a native
// no-clobber refusal when another process created the target first, cancellation, and I/O errors.
func (s *Staged) Publish(ctx context.Context, policy Policy) error {
	err := ctx.Err()
	if err != nil {
		return errors.Join(fmt.Errorf(publicationFailureFormat, s.target, err), s.Discard())
	}

	_, err = s.publishRetainingContext(ctx, policy)
	if err != nil {
		return errors.Join(err, s.Discard())
	}

	return nil
}

// Discard removes the staged file unless it was published or kept as a recovery file.
func (s *Staged) Discard() error {
	if s.settled {
		return nil
	}

	s.settled = true

	return errors.Join(s.removeOwnedStage(), s.releaseOwner())
}

// removeStaged removes a staged file, treating one that is already gone as removed.
func removeStaged(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged %q: %w", path, err)
	}

	return nil
}

// publishRetainingContext preserves staged content on pre-commit failure for recovery.
func (s *Staged) publishRetainingContext(ctx context.Context, policy Policy) (bool, error) {
	guard := func() error {
		if err := s.owner.Verify(s.path); err != nil {
			return err
		}

		if s.Verify != nil {
			return s.Verify()
		}

		return nil
	}
	err := commitFileChecked(ctx, s.ops, s.path, s.target, policy.existing(), guard)

	var finalization *FinalizationError

	renamed := errors.As(err, &finalization)
	if err == nil || renamed {
		s.settled = true

		s.cleanupErr = s.releaseOwner()
	}

	return renamed, err
}
