// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publication

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNoReplacePreservesLateTargetAndAbortCleansTemp(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("other writer"), 0600); err != nil {
		t.Fatal(err)
	}
	published, err := f.Commit(false)
	if err == nil || published || f.Published() {
		t.Fatalf("late target was replaced: published=%v %v", published, err)
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(f.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp remains: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "other writer" {
		t.Fatalf("target %q %v", data, err)
	}
}

func TestCommitAndOverwritePreserveMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(target, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0640); err != nil {
			t.Fatal(err)
		}
	}
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("replacement"); err != nil {
		t.Fatal(err)
	}
	published, err := f.Commit(true)
	if err != nil || !published || !f.Published() {
		t.Fatalf("commit %v %v", published, err)
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("target %q %v", data, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0640 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected temp entries %v %v", entries, err)
	}
}

func TestNewOutputUsesConventionalPermissionsAndAbortRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "umask-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0666); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "output.pdf")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != expected.Mode().Perm() {
		t.Fatalf("temp mode %o expected umask mode %o", info.Mode().Perm(), expected.Mode().Perm())
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(f.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp remains: %v", err)
	}
	if _, err = os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("abort created target")
	}
}

func TestRealNoReplaceCommit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("complete"); err != nil {
		t.Fatal(err)
	}
	published, err := f.Commit(false)
	if err != nil || !published {
		t.Fatalf("no-replace commit %v %v", published, err)
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "complete" {
		t.Fatalf("published target %q %v", data, err)
	}
}

func TestPublicationStatePrecedesTrailingErrors(t *testing.T) {
	late := errors.New("late failure")
	for _, publicationErr := range []error{nil, late} {
		f := &File{}
		called := false
		published, err := f.finishCommit(true, publicationErr, func() error {
			called = true
			if !f.Published() {
				t.Fatal("visibility was not recorded before sync")
			}
			return late
		})
		if !published || !f.Published() || !errors.Is(err, late) || called != (publicationErr == nil) {
			t.Fatalf("state/error order %v %v called=%v", published, err, called)
		}
		f.recordPublication(false)
		if !f.Published() {
			t.Fatal("publication state went backwards")
		}
	}
}

func TestLinkFallbackStateAndNoClobber(t *testing.T) {
	dir := t.TempDir()
	temp := filepath.Join(dir, "temp")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(temp, []byte("complete"), 0600); err != nil {
		t.Fatal(err)
	}
	unsupported := unsupportedError()
	rename := func(string, string) error { return unsupported }
	late := errors.New("temp removal failed")
	published, err := publish(temp, target, false, rename, os.Link, func(string) error { return late })
	if !published || !errors.Is(err, late) {
		t.Fatalf("link state lost after cleanup failure: %v %v", published, err)
	}
	if data, e := os.ReadFile(target); e != nil || string(data) != "complete" {
		t.Fatalf("linked target %q %v", data, e)
	}
	f := &File{target: target, temp: temp, closed: true, published: published}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	if data, e := os.ReadFile(target); e != nil || string(data) != "complete" {
		t.Fatalf("abort undid publication %q %v", data, e)
	}
	// The real link boundary also refuses an existing target.
	if err = os.WriteFile(temp, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	published, err = publish(temp, target, false, rename, os.Link, os.Remove)
	if published || err == nil {
		t.Fatalf("link fallback clobbered target: %v %v", published, err)
	}
	if !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("fallback failure is not actionable: %v", err)
	}
}
