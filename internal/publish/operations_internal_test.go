// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

type (
	// errReader fails once and then reports the end of its content, so a copy that does not stop at the
	// failure ends in success instead of looping on the failure.
	errReader struct{ failed bool }

	cancelingReader struct {
		cancel func()
		reads  int
	}
)

const (
	modeOwnerOnly = 0o600
	oldContent    = "old"
)

var errInjected = errors.New("injected failure")

func put(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), modeOwnerOnly)
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, path string) string {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	content, readErr := root.ReadFile(filepath.Base(path))
	closeErr := root.Close()

	if readErr != nil || closeErr != nil {
		t.Fatal(errors.Join(readErr, closeErr))
	}

	return string(content)
}

// getIfPresent returns the content of path, or "" when it does not exist.
func getIfPresent(t *testing.T, path string) string {
	t.Helper()

	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}

	return get(t, path)
}

func names(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}

	return result
}

func stageText(t *testing.T, ops operations, target, content string) *Staged {
	t.Helper()

	staged, err := stageWith(context.Background(), ops, target, strings.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	return staged
}

func TestFileFlushesBeforeAndAfterTheRename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged, destination := filepath.Join(dir, stagedPath), filepath.Join(dir, outputPath)
	put(t, staged, "pdf")

	var order []string

	ops := realOperations()
	ops.syncFile = func(string) error {
		order = append(order, "sync file")

		return nil
	}
	ops.replace = func(s, d string, o existingFile) error {
		order = append(order, "rename")

		return replaceFile(s, d, o)
	}
	ops.syncDirectory = func(string) error {
		order = append(order, "sync directory")

		return nil
	}

	err := commitFile(ops, staged, destination, refuseExisting)
	if err != nil || strings.Join(order, ",") != "sync file,rename,sync directory" {
		t.Fatalf("order %v, err %v", order, err)
	}
}

func TestFileFailuresLeaveDestinationUntouched(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*operations){
		"flush staged":  func(o *operations) { o.syncFile = func(string) error { return errInjected } },
		"native rename": func(o *operations) { o.replace = func(string, string, existingFile) error { return errInjected } },
	}
	for name, inject := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			staged, destination := filepath.Join(dir, stagedPath), filepath.Join(dir, outputPath)
			put(t, staged, replacementContent)
			put(t, destination, oldContent)

			ops := realOperations()
			inject(&ops)

			err := commitFile(ops, staged, destination, replaceExisting)
			if !errors.Is(err, errInjected) || get(t, destination) != oldContent {
				t.Fatalf("err %v, destination %q", err, get(t, destination))
			}
		})
	}
}

func TestFileReportsPublishedFileWhenDirectoryFlushFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged, destination := filepath.Join(dir, stagedPath), filepath.Join(dir, outputPath)
	put(t, staged, replacementContent)

	ops := realOperations()
	ops.syncDirectory = func(string) error { return errInjected }

	err := commitFile(ops, staged, destination, refuseExisting)

	var finalization *FinalizationError
	if !errors.As(err, &finalization) || !errors.Is(err, errInjected) || get(t, destination) != replacementContent {
		t.Fatalf("err %v", err)
	}

	if !strings.Contains(err.Error(), "published") {
		t.Fatalf("message %q does not say the file is published", err)
	}
}

func TestNativeNoClobberRefusesDestinationCreatedAfterTheCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, outputPath)

	ops := realOperations()
	ops.replace = func(staged, target string, existing existingFile) error {
		put(t, target, "concurrent")

		return replaceFile(staged, target, existing)
	}

	staged := stageText(t, ops, destination, "mine")

	err := staged.Publish(context.Background(), Policy{})
	if err == nil || get(t, destination) != "concurrent" {
		t.Fatalf("Publish() = %v, destination %q", err, get(t, destination))
	}

	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("staging not cleaned up: %v", got)
	}
}

func TestConcurrentPublishersCreateExactlyOneDestination(t *testing.T) {
	t.Parallel()

	const writers = 12

	dir := t.TempDir()
	destination := filepath.Join(dir, outputReportPath)

	var (
		group     sync.WaitGroup
		guard     sync.Mutex
		successes []string
	)

	for _, content := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}[:writers] {
		group.Go(func() {
			staged, err := Stage(context.Background(), destination, strings.NewReader(content), 16)
			if err != nil {
				t.Error(err)

				return
			}

			if staged.Publish(context.Background(), Policy{}) == nil {
				guard.Lock()

				successes = append(successes, content)

				guard.Unlock()
			}
		})
	}

	group.Wait()

	if len(successes) != 1 || get(t, destination) != successes[0] || len(names(t, dir)) != 1 {
		t.Fatalf("successes %v, directory %v", successes, names(t, dir))
	}
}

func TestOverwriteReplacesByRenameAndDoesNotWriteThroughHardLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination, alias := filepath.Join(dir, outputPath), filepath.Join(dir, "alias.pdf")
	put(t, destination, oldContent)

	err := os.Link(destination, alias)
	if err != nil {
		t.Fatalf("required hard-link capability unavailable: %v", err)
	}

	staged := stageText(t, realOperations(), destination, replacementContent)

	err = staged.Publish(context.Background(), Policy{Overwrite: true})
	if err != nil || get(t, destination) != replacementContent || get(t, alias) != oldContent {
		t.Fatalf("Publish() = %v; destination %q alias %q", err, get(t, destination), get(t, alias))
	}
}

func TestNoClobberOnlyIgnoresOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, reportPath)
	put(t, destination, existingContent)

	staged := stageText(t, realOperations(), destination, "failure report")

	err := staged.Publish(context.Background(), Policy{Overwrite: true, NoClobberOnly: true})
	if err == nil || get(t, destination) != existingContent || len(names(t, dir)) != 1 {
		t.Fatalf("Publish() = %v; directory %v", err, names(t, dir))
	}

	err = Preflight(destination, Policy{Overwrite: true, NoClobberOnly: true})
	if err == nil {
		t.Fatal("Preflight accepted an existing file in no-clobber-only mode")
	}

	err = Preflight(destination, Policy{Overwrite: true})
	if err != nil {
		t.Fatalf("Preflight rejected an allowed overwrite: %v", err)
	}
}

func TestPublishRefusesSymbolicLinkAndDirectoryTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	regular, link, folder := filepath.Join(dir, realFilePath), filepath.Join(dir, "link"), filepath.Join(dir, "folder")
	put(t, regular, realFilePath)

	err := os.Mkdir(folder, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	err = os.Symlink(regular, link)
	if err != nil {
		t.Fatalf("required symbolic-link capability unavailable: %v", err)
	}

	for _, target := range []string{link, folder} {
		staged := stageText(t, realOperations(), target, "x")

		err = staged.Publish(context.Background(), Policy{Overwrite: true})
		if err == nil {
			t.Fatalf("Publish(%q) succeeded", target)
		}
	}

	if get(t, regular) != realFilePath || len(names(t, dir)) != 3 {
		t.Fatalf("directory %v", names(t, dir))
	}
}

func TestExistingDestinationIsRefusedUnderCaseAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	put(t, filepath.Join(dir, outputPath), oldContent)

	aliasPath := filepath.Join(dir, "OUT.PDF")
	_, statErr := os.Lstat(aliasPath)

	distinct := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !distinct {
		t.Fatal(statErr)
	}

	staged := stageText(t, realOperations(), aliasPath, replacementContent)

	err := staged.Publish(t.Context(), Policy{})
	if distinct {
		if err != nil || get(t, aliasPath) != replacementContent {
			t.Fatalf("distinct native name publication: %v", err)
		}
	} else if err == nil {
		t.Fatal("native alias bypassed no-clobber")
	}

	if get(t, filepath.Join(dir, outputPath)) != oldContent {
		t.Fatal("original case-variant bytes changed")
	}
}

func TestCancellationBeforePublishRemovesStagingAndPublishesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, outputReportPath)
	staged := stageText(t, realOperations(), destination, "x")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := staged.Publish(ctx, Policy{})
	if !errors.Is(err, context.Canceled) || len(names(t, dir)) != 0 {
		t.Fatalf("Publish() = %v; directory %v", err, names(t, dir))
	}
}

func TestStageContentAndPrivacy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, reportPath)

	staged, err := Stage(context.Background(), destination, strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(staged.Path())
	if err != nil {
		t.Fatal(err)
	}

	beside := filepath.Dir(staged.Path()) == dir && staged.Target() == destination
	if !beside || staged.Size() != 5 || get(t, staged.Path()) != "12345" {
		t.Fatalf("staged %+v, mode %v", staged, info.Mode())
	}

	permissiontest.RequirePrivateFile(t, staged.Path())

	err = staged.Discard()
	if err != nil || len(names(t, dir)) != 0 {
		t.Fatalf("Discard() = %v; directory %v", err, names(t, dir))
	}

	err = staged.Discard()
	if err != nil {
		t.Fatalf("second Discard() = %v", err)
	}
}

func TestStageRejectsBadInputsAndCleansUp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, reportPath)

	_, err := Stage(context.Background(), "relative.json", strings.NewReader("x"), 5)
	if !errors.Is(err, ErrNotAbsolute) {
		t.Fatalf("relative target = %v", err)
	}

	_, err = Stage(context.Background(), destination, strings.NewReader("123456"), 5)

	var tooLarge *SizeLimitError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 5 || tooLarge.Target != destination {
		t.Fatalf("oversized content = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = Stage(ctx, destination, strings.NewReader("x"), 5)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled before staging = %v", err)
	}

	_, err = Stage(context.Background(), destination, &errReader{}, 5)
	if !errors.Is(err, errInjected) {
		t.Fatalf("read failure = %v", err)
	}

	_, err = Stage(context.Background(), filepath.Join(dir, missingPath, shortReportPath), strings.NewReader("x"), 5)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing directory = %v", err)
	}

	if len(names(t, dir)) != 0 {
		t.Fatalf(stagingLeftFormat, names(t, dir))
	}
}

func (r *errReader) Read([]byte) (int, error) {
	if r.failed {
		return 0, io.EOF
	}

	r.failed = true

	return 0, errInjected
}

func TestStageCancellationBetweenChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelingReader{cancel: cancel}

	_, err := Stage(ctx, filepath.Join(dir, "big.json"), reader, 1<<40)
	if !errors.Is(err, context.Canceled) || len(names(t, dir)) != 0 || reader.reads != 1 {
		t.Fatalf("Stage() = %v; reads %d; directory %v", err, reader.reads, names(t, dir))
	}
}

func (r *cancelingReader) Read(p []byte) (int, error) {
	r.reads++
	r.cancel()

	return len(p), nil
}

func TestStageDiskFullPermissionAndFlushFailures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, reportPath)

	full := realOperations()
	full.writeChunk = func(io.Writer, []byte) error { return errDiskFull }

	_, err := stageWith(context.Background(), full, destination, strings.NewReader("x"), 5)

	var stageErr *StageError
	if !errors.As(err, &stageErr) || !strings.Contains(err.Error(), "volume is full") || !errors.Is(err, errDiskFull) {
		t.Fatalf("disk full = %v", err)
	}

	finalizationFailed := realOperations()
	finalizationFailed.syncFile = func(string) error { return errInjected }

	_, err = stageWith(context.Background(), finalizationFailed, destination, strings.NewReader("x"), 5)
	if !errors.Is(err, errInjected) {
		t.Fatalf("flush failure = %v", err)
	}

	if len(names(t, dir)) != 0 {
		t.Fatalf(stagingLeftFormat, names(t, dir))
	}

	permissiontest.RequireEnforcement(t)

	readOnly := filepath.Join(dir, "ro")

	err = os.Mkdir(readOnly, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	permissiontest.DenyDirectoryChanges(t, readOnly)

	_, err = Stage(context.Background(), filepath.Join(readOnly, shortReportPath), strings.NewReader("x"), 5)
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("read-only directory = %v", err)
	}

	other := &StageError{Target: "t", Err: errInjected}
	if strings.Contains(other.Error(), "full") || strings.Contains(other.Error(), "writable") {
		t.Fatalf("unrelated failure got a hint: %v", other)
	}
}

func TestStageErrorsAndSizeLimitUnwrapAndDescribe(t *testing.T) {
	t.Parallel()

	if !strings.Contains((&SizeLimitError{Target: shortReportPath, Limit: 9}).Error(), "9") {
		t.Fatal("size limit not in message")
	}
}
