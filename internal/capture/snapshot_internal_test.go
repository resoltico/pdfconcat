// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type (
	failingScratch struct {
		writeErr error
		closeErr error
	}

	// brokenReader fails once and then reports the end of its content, so a copy that does not stop at the
	// failure ends in success instead of reading the failure forever.
	brokenReader struct{ failed bool }
)

const (
	kindDirectory = "a directory"
	inputName     = "in.pdf"
)

func TestSnapshotCopiesBytesWithDigestAndIdentity(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	source := filepath.Join(t.TempDir(), inputName)
	content := bytes.Repeat([]byte("0123456789abcdef"), 3*copyChunkSize/16+7)
	writeTestFile(t, source, content)

	captured, err := workspace.Snapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}

	if captured.Size != int64(len(content)) || captured.SourcePath != source {
		t.Fatalf("captured = %+v", captured)
	}

	if want := sha256.Sum256(content); captured.SHA256 != want || len(captured.Digest()) != 2*sha256.Size {
		t.Fatalf("digest %s", captured.Digest())
	}

	if !bytes.Equal(readTestFile(t, captured.Path), content) {
		t.Fatal("private copy differs")
	}

	if filepath.Dir(captured.Path) != workspace.Dir() {
		t.Fatalf("copy %q outside workspace", captured.Path)
	}

	wantIdentity, err := IdentityOf(source)
	if err != nil || captured.Source != wantIdentity {
		t.Fatalf("identity %v, want %v (err %v)", captured.Source, wantIdentity, err)
	}

	if !bytes.Equal(readTestFile(t, source), content) {
		t.Fatal("original was modified")
	}
}

func TestSnapshotCopiesEmptyFile(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	source := filepath.Join(t.TempDir(), "empty.pdf")
	writeTestFile(t, source, nil)

	captured, err := workspace.Snapshot(context.Background(), source)
	if err != nil || captured.Size != 0 || captured.SHA256 != sha256.Sum256(nil) {
		t.Fatalf("captured = %+v, err %v", captured, err)
	}
}

func TestSnapshotFollowsSymbolicLinkToRegularFile(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target.pdf"), filepath.Join(dir, "link.pdf")
	writeTestFile(t, target, []byte("linked"))

	err := os.Symlink(target, link)
	if err != nil {
		t.Fatalf(symlinkUnavailableFormat, err)
	}

	captured, err := workspace.Snapshot(context.Background(), link)
	if err != nil || captured.Size != int64(len("linked")) {
		t.Fatalf("captured = %+v, err %v", captured, err)
	}
}

func TestSnapshotRejectsDanglingSymbolicLinkAndMissingFile(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling.pdf")

	err := os.Symlink(filepath.Join(dir, "gone.pdf"), link)
	if err != nil {
		t.Fatalf(symlinkUnavailableFormat, err)
	}

	for _, path := range []string{link, filepath.Join(dir, missingPDFPath)} {
		_, err = workspace.Snapshot(context.Background(), path)

		var sourceErr *SourceError
		if !errors.As(err, &sourceErr) || !errors.Is(err, fs.ErrNotExist) || sourceErr.Path != path {
			t.Fatalf("Snapshot(%q) = %v", path, err)
		}
	}

	if scratchEntries(t, workspace) != 0 {
		t.Fatal("scratch left behind")
	}
}

func TestSnapshotRejectsDirectory(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)

	_, err := workspace.Snapshot(context.Background(), t.TempDir())

	var irregular *NotRegularFileError
	if !errors.As(err, &irregular) || irregular.Kind != kindDirectory {
		t.Fatalf("Snapshot(dir) = %v", err)
	}
}

func TestDescribeModeCoversEveryKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want string
		mode fs.FileMode
	}{
		{kindDirectory, fs.ModeDir},
		{"a named pipe", fs.ModeNamedPipe},
		{"a device", fs.ModeDevice},
		{"an irregular file", fs.ModeSocket},
	}
	for _, test := range tests {
		if got := describeMode(test.mode); got != test.want {
			t.Errorf("describeMode(%v) = %q, want %q", test.mode, got, test.want)
		}
	}
}

func TestSnapshotDetectsChangeDuringCapture(t *testing.T) {
	t.Parallel()

	tests := map[string]func(t *testing.T, path string){
		"appended bytes": func(t *testing.T, path string) {
			t.Helper()

			appendTestFile(t, path, "more")
		},
		"modification time": func(t *testing.T, path string) {
			t.Helper()

			err := os.Chtimes(path, time.Now(), time.Now().Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			workspace := newTestWorkspace(t)
			source := filepath.Join(t.TempDir(), inputName)
			writeTestFile(t, source, bytes.Repeat([]byte("x"), 2*copyChunkSize))

			changed := false
			workspace.afterChunk = func(int64) {
				if !changed {
					changed = true

					change(t, source)
				}
			}

			_, err := workspace.Snapshot(context.Background(), source)

			var changedErr *SourceChangedError
			if !errors.As(err, &changedErr) || changedErr.Path != source {
				t.Fatalf(snapshotFailureFormat, err)
			}

			if scratchEntries(t, workspace) != 0 {
				t.Fatal("scratch left behind after a changed source")
			}
		})
	}
}

func TestSnapshotCancellationStopsLargeCopyAndCleansUp(t *testing.T) {
	t.Parallel()

	const size = 64 << 20

	workspace := newTestWorkspace(t)
	source := filepath.Join(t.TempDir(), "big.pdf")

	createSparseFile(t, source, size)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var copied int64

	workspace.afterChunk = func(total int64) {
		copied = total

		cancel()
	}

	_, err := workspace.Snapshot(ctx, source)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf(snapshotFailureFormat, err)
	}

	if copied >= size || scratchEntries(t, workspace) != 0 {
		t.Fatalf("copied %d bytes, scratch entries %d", copied, scratchEntries(t, workspace))
	}
}

func TestSnapshotHonorsContextBeforeOpening(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := workspace.Snapshot(ctx, filepath.Join(t.TempDir(), "never-opened.pdf"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf(snapshotFailureFormat, err)
	}
}

func (f failingScratch) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	return len(p), nil
}

func (f failingScratch) Close() error { return f.closeErr }

func TestSnapshotReportsScratchFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		scratch failingScratch
		want    string
	}{
		"disk full":    {failingScratch{writeErr: errDiskFull, closeErr: nil}, "volume is full"},
		"close failed": {failingScratch{writeErr: nil, closeErr: syscall.EIO}, "healthy"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			workspace := newTestWorkspace(t)
			workspace.openScratch = func(string) (io.WriteCloser, error) { return test.scratch, nil }
			source := filepath.Join(t.TempDir(), inputName)
			writeTestFile(t, source, []byte(fixtureContent))

			_, err := workspace.Snapshot(context.Background(), source)

			var scratch *ScratchError
			if !errors.As(err, &scratch) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(snapshotFailureFormat, err)
			}
		})
	}
}

func TestSnapshotReportsExistingScratchPath(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	writeTestFile(t, filepath.Join(workspace.Dir(), "scratch-1"), []byte("squatter"))

	source := filepath.Join(t.TempDir(), inputName)
	writeTestFile(t, source, []byte(fixtureContent))

	_, err := workspace.Snapshot(context.Background(), source)

	var scratch *ScratchError
	if !errors.As(err, &scratch) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf(snapshotFailureFormat, err)
	}
}

func (r *brokenReader) Read([]byte) (int, error) {
	if r.failed {
		return 0, io.EOF
	}

	r.failed = true

	return 0, syscall.EIO
}

func TestCopyStreamReportsReadFailure(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	captured := Captured{Path: workspace.NewPath(""), SourcePath: "src.pdf"}

	err := workspace.copyStream(context.Background(), &brokenReader{}, &captured)

	var sourceErr *SourceError
	if !errors.As(err, &sourceErr) || !errors.Is(err, syscall.EIO) {
		t.Fatalf("copyStream() = %v", err)
	}
}

func TestCloseSourceNamesThePathWhenTheHandleCannotBeClosed(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), inputName)
	writeTestFile(t, path, []byte("x"))

	source, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	err = closeSource(source, path)
	if err != nil {
		t.Fatalf("closing an open handle: %v", err)
	}

	// Closing a handle a second time is the failure a real descriptor reports when it is already gone.
	err = closeSource(source, path)

	var failure *SourceError
	if !errors.As(err, &failure) || failure.Path != path || failure.Operation != "close source" || !errors.Is(err, os.ErrClosed) {
		t.Errorf("a handle that cannot be closed: %v", err)
	}
}

func TestRemoveScratchReportsWhatItCannotRemove(t *testing.T) {
	t.Parallel()

	err := removeScratch("")
	if err != nil {
		t.Errorf("no scratch file was created: %v", err)
	}

	err = removeScratch(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Errorf("a scratch file that is already gone: %v", err)
	}

	// A directory that still holds a file cannot be removed with Remove, on any platform.
	scratch := filepath.Join(t.TempDir(), "partial")

	err = os.Mkdir(scratch, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(scratch, "inside"), []byte("x"))

	err = removeScratch(scratch)

	var failure *ScratchError
	if !errors.As(err, &failure) || failure.Operation != "remove partial copy" || failure.Dir != filepath.Dir(scratch) {
		t.Errorf("a scratch path that cannot be removed: %v", err)
	}
}
