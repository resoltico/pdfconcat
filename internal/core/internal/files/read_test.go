// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/files"
)

func TestRegularFilesSymlinksAndLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{0, 3, 4} {
		data, err := files.Read(t.Context(), path, limit)
		if err != nil || string(data) != "abc" {
			t.Fatalf("limit %d: %q %v", limit, data, err)
		}
	}
	if _, err := files.Read(t.Context(), path, 2); !errors.Is(err, files.ErrTooLarge) {
		t.Fatalf("oversized input: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Logf("symlink creation unavailable: %v", err)
	} else {
		data, err := files.Read(t.Context(), link, 3)
		if err != nil || string(data) != "abc" {
			t.Fatalf("regular symlink: %q %v", data, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := files.Read(ctx, path, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := files.Read(t.Context(), filepath.Join(dir, "missing"), 0); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing input: %v", err)
	}
}

func TestNonregularNamedInputsAreRejectedBeforeReading(t *testing.T) {
	paths := []string{t.TempDir()}
	if runtime.GOOS != "windows" {
		paths = append(paths, "/dev/null")
		if program, err := exec.LookPath("mkfifo"); err == nil {
			fifo := filepath.Join(t.TempDir(), "pipe")
			if out, err := exec.CommandContext(t.Context(), program, fifo).CombinedOutput(); err != nil {
				t.Fatalf("mkfifo %v %s", err, out)
			}
			paths = append(paths, fifo)
		} else {
			t.Log("mkfifo unavailable; directory/device controls still run")
		}
	}
	for _, path := range paths {
		if _, err := files.Read(t.Context(), path, 0); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("nonregular %s: %v", path, err)
		}
	}
}

type countingReader struct {
	reader io.Reader
	bytes  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}

func TestStreamLimitStopsReadingBeyondDetectionByte(t *testing.T) {
	reader := &countingReader{reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, 1000))}
	if _, err := files.ReadStream(t.Context(), reader, 3); !errors.Is(err, files.ErrTooLarge) || reader.bytes != 4 {
		t.Fatalf("limit retained/read too much: bytes=%d err=%v", reader.bytes, err)
	}
	data, err := files.ReadStream(t.Context(), bytes.NewBufferString("abc"), 3)
	if err != nil || string(data) != "abc" {
		t.Fatalf("inclusive stream limit %q %v", data, err)
	}
}
