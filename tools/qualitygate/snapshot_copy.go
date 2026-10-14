// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	snapshotListingBytes int64 = 32 << 20
	snapshotCopyBytes    int64 = 512 << 20
	snapshotCopyEntries        = 65536
	snapshotCopyChunk          = 32 << 10
)

type snapshotCopyBudget struct {
	remainingBytes   int64
	remainingEntries int
}

func newSnapshotCopyBudget() *snapshotCopyBudget {
	return &snapshotCopyBudget{snapshotCopyBytes, snapshotCopyEntries}
}

func listSnapshotFiles(ctx context.Context, root string, limit int64) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	budget := &evidenceBudget{remaining: limit, limit: limit, cancel: cancel}
	var stdout, stderr bytes.Buffer
	// ls-files needs no optional filesystem-monitor subprocess. Disable it so
	// cancellation owns the builtin Git producer rather than a configured hook.
	run := &command{dir: root, name: gitTool, args: []string{"-c", "core.fsmonitor=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard"}, stdout: budgetedEvidenceWriter{budget, &stdout}, stderr: budgetedEvidenceWriter{budget, &stderr}}
	if err := errors.Join(run.run(ctx), budget.err(), ctx.Err()); err != nil {
		return "", fmt.Errorf("snapshot Git listing: %w\n%s", err, indent(stderr.String()))
	}
	return stdout.String(), nil
}

// copyListed shares one aggregate byte/entry allowance across files and newly
// created parents. Existing foreign-overlay files may be replaced as before.
func copyListed(ctx context.Context, source, target, listing string, budget *snapshotCopyBudget) (int, error) {
	from, err := os.OpenRoot(source)
	if err != nil {
		return 0, fmt.Errorf(openFileError, source, err)
	}
	dest, err := os.OpenRoot(target)
	if err != nil {
		return 0, errors.Join(fmt.Errorf(openFileError, target, err), from.Close())
	}
	copied := 0
	var copyErr error
	for name := range strings.SplitSeq(listing, "\x00") {
		if err := ctx.Err(); err != nil {
			copyErr = err
			break
		}
		if name == "" {
			continue
		}
		err = copyFile(ctx, from, dest, filepath.FromSlash(name), budget)
		if errors.Is(err, fs.ErrNotExist) {
			// Only a missing source represents a working-tree deletion. A missing
			// destination parent or file is a copy failure, not an omitted input.
			if _, sourceErr := from.Lstat(filepath.FromSlash(name)); errors.Is(sourceErr, fs.ErrNotExist) {
				continue
			}
		}
		if err != nil {
			copyErr = err
			break
		}
		copied++
	}
	return copied, errors.Join(copyErr, from.Close(), dest.Close())
}

func copyFile(ctx context.Context, from, dest *os.Root, name string, budget *snapshotCopyBudget) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("snapshot copy canceled: %w", err)
	}
	source, info, err := openSnapshotInput(from, name)
	if err != nil {
		return err
	}
	result := func() error {
		if info.Size() > budget.remainingBytes {
			return fmt.Errorf("%w: snapshot exceeds aggregate %d-byte input allowance at %s", errGate, snapshotCopyBytes, name)
		}
		if err = prepareSnapshotParents(dest, filepath.Dir(name), budget); err != nil {
			return err
		}
		output, err := openSnapshotOutput(dest, name, info.Mode().Perm(), budget)
		if err != nil {
			return err
		}
		streamErr := streamSnapshotFile(ctx, output, source, budget)
		return errors.Join(streamErr, output.Close())
	}()
	return errors.Join(result, source.Close())
}

func prepareSnapshotParents(dest *os.Root, parent string, budget *snapshotCopyBudget) error {
	if parent == "." {
		return nil
	}
	if !filepath.IsLocal(parent) {
		return fmt.Errorf("%w: nonlocal snapshot path: %s", errGate, parent)
	}
	path := ""
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := dest.Lstat(path)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: snapshot parent is not a directory: %s", errGate, path)
			}
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if budget.remainingEntries <= 0 {
			return fmt.Errorf("%w: snapshot exceeds %d new file/directory entries", errGate, snapshotCopyEntries)
		}
		if err = dest.Mkdir(path, dirMode); err != nil {
			return err
		}
		budget.remainingEntries--
	}
	return nil
}

func openSnapshotOutput(dest *os.Root, name string, mode fs.FileMode, budget *snapshotCopyBudget) (*os.File, error) {
	before, err := dest.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		if budget.remainingEntries <= 0 {
			return nil, fmt.Errorf("%w: snapshot exceeds %d new file/directory entries", errGate, snapshotCopyEntries)
		}
		file, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return nil, err
		}
		budget.remainingEntries--
		if err = file.Chmod(mode); err != nil {
			return nil, errors.Join(err, file.Close())
		}
		return file, nil
	}
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: nonregular snapshot output: %s", errGate, name)
	}
	file, err := dest.OpenFile(name, os.O_WRONLY, mode)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, errors.Join(fmt.Errorf("%w: snapshot output identity changed: %s", errGate, name), err, file.Close())
	}
	if err = file.Truncate(0); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// Probe one extra input byte rather than writing one extra destination byte.
// This also detects growth after the initial source size check.
func streamSnapshotFile(ctx context.Context, output io.Writer, input io.Reader, budget *snapshotCopyBudget) error {
	buffer := make([]byte, snapshotCopyChunk)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("snapshot copy canceled: %w", err)
		}
		count, readErr := input.Read(buffer[:min(int64(len(buffer)), budget.remainingBytes+1)])
		if int64(count) > budget.remainingBytes {
			return fmt.Errorf("%w: snapshot exceeds aggregate %d-byte input allowance", errGate, snapshotCopyBytes)
		}
		if count > 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("snapshot copy canceled: %w", err)
			}
			written, writeErr := output.Write(buffer[:count])
			budget.remainingBytes -= int64(written)
			if writeErr != nil {
				return writeErr
			}
			if written != count {
				return io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// Open through the root and compare the resulting regular file with the named
// input, so a changed leaf cannot silently replace the inspected identity.
func openSnapshotInput(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", name, err)
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: snapshot input is not a regular file: %s", errGate, name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("open snapshot input %s: %w", name, err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, nil, errors.Join(fmt.Errorf("%w: snapshot source identity changed: %s", errGate, name), err, file.Close())
	}
	return file, opened, nil
}

func compareSnapshotStreams(ctx context.Context, original, copied io.Reader, size int64) error {
	left, right := make([]byte, snapshotCopyChunk), make([]byte, snapshotCopyChunk)
	for remaining := size; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := min(int64(len(left)), remaining)
		if _, err := io.ReadFull(original, left[:count]); err != nil {
			return errors.Join(errGate, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.ReadFull(copied, right[:count]); err != nil {
			return errors.Join(errGate, err)
		}
		if !bytes.Equal(left[:count], right[:count]) {
			return errGate
		}
		remaining -= count
	}
	// Probe at most copied size + 1, even if an input grows while comparing.
	if err := ctx.Err(); err != nil {
		return err
	}
	count, leftErr := original.Read(left[:1])
	if count != 0 || !errors.Is(leftErr, io.EOF) {
		return errors.Join(errGate, leftErr)
	}
	count, rightErr := copied.Read(right[:1])
	if count != 0 || !errors.Is(rightErr, io.EOF) {
		return errors.Join(errGate, rightErr)
	}
	return nil
}
