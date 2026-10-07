// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	createFixtureErrorFormat = "create fixture: %w"
	unknownReplacement       = "unknown replacement"
	canceledAction           = "canceled"
)

func TestOwnerPinRejectsUnlinkReplacementAndDiscardPreservesUnknownBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	staged, stageErr := Stage(t.Context(), filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	lease := staged.owner.file
	if err := os.Remove(staged.Path()); err != nil {
		t.Fatal(err)
	}

	put(t, staged.Path(), unknownReplacement)

	if err := staged.owner.Verify(staged.Path()); !errors.Is(err, errOwnerChanged) {
		t.Fatalf("replacement masqueraded as original: %v", err)
	}

	if err := staged.Discard(); !errors.Is(err, errOwnerChanged) {
		t.Fatalf("discard accepted replacement: %v", err)
	}

	if get(t, staged.Path()) != unknownReplacement {
		t.Fatal("discard removed unknown bytes")
	}

	requireLeaseClosed(t, lease)
}

func TestOwnerPinClosesAfterEveryStagedTerminalOutcome(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"publish", "discard", canceledAction, "failed"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			staged, stageErr := Stage(t.Context(), filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
			if stageErr != nil {
				t.Fatal(stageErr)
			}

			lease := staged.owner.file
			runStagedTerminalOutcome(t, staged, action)
			requireLeaseClosed(t, lease)
		})
	}
}

func TestLateResultTransfersPinUntilRecoveryCallerReleasesIt(t *testing.T) {
	t.Parallel()

	ops := realOperations()
	fixture := newCommitFixture(t, ops)
	lease := fixture.reportStage.owner.file
	ops.replace = func(staged, target string, policy existingFile) error {
		if target == fixture.reportPath {
			return errInjected
		}

		return replaceFile(staged, target, policy)
	}
	fixture.reportStage.ops = ops

	result, commitErr := commitWith(t.Context(), ops, fixture.pdf(), fixture.reportStage, Policy{})
	if commitErr == nil || !result.PDFPublished || result.RecoveryOwner == nil {
		t.Fatalf("late outcome: %+v %v", result, commitErr)
	}

	if err := result.RecoveryOwner.Verify(result.RecoveryPath); err != nil {
		t.Fatal(err)
	}

	if err := result.CloseRecovery(); err != nil {
		t.Fatal(err)
	}

	if err := result.CloseRecovery(); err != nil {
		t.Fatal(err)
	}

	requireLeaseClosed(t, lease)

	if err := result.RecoveryOwner.Verify(result.RecoveryPath); !errors.Is(err, errOwnerReleased) {
		t.Fatalf("closed ownership accepted: %v", err)
	}

	if get(t, result.RecoveryPath) != reportContent {
		t.Fatal("closing pin removed recovery bytes")
	}
}

func requireLeaseClosed(t *testing.T, file *os.File) {
	t.Helper()

	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("identity descriptor leaked: %v", err)
	}
}

func TestStagePinRejectsSubstitutionBeforeWritingAndPreservesUnknownBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ops := realOperations()

	var (
		writer *os.File
		path   string
	)

	ops.createTemp = func(directory, pattern string) (*os.File, error) {
		file, createErr := os.CreateTemp(directory, pattern)
		if createErr != nil {
			return nil, fmt.Errorf(createFixtureErrorFormat, createErr)
		}

		writer, path = file, file.Name()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}

		put(t, path, unknownReplacement)

		return file, nil
	}

	_, err := stageWith(t.Context(), ops, filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
	if !errors.Is(err, errOwnerChanged) {
		t.Fatalf("substitution accepted: %v", err)
	}

	requireLeaseClosed(t, writer)

	if get(t, path) != unknownReplacement {
		t.Fatal("pin cleanup removed replacement")
	}
}

func TestOwnerVerificationMissingPathAndClosedLiveHandle(t *testing.T) {
	t.Parallel()

	staged, stageErr := Stage(t.Context(), filepath.Join(t.TempDir(), reportPath), strings.NewReader(reportContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	if err := os.Remove(staged.Path()); err != nil {
		t.Fatal(err)
	}

	if err := staged.owner.Verify(staged.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path: %v", err)
	}

	if err := staged.owner.file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := staged.owner.Verify(staged.Path()); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed live object: %v", err)
	}
	// Close failure is reported even when the path has vanished; no foreign bytes are touched.
	if err := staged.Discard(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("cleanup close failure: %v", err)
	}
}

func runStagedTerminalOutcome(t *testing.T, staged *Staged, action string) {
	t.Helper()

	switch action {
	case "publish":
		if err := staged.Publish(t.Context(), Policy{}); err != nil {
			t.Fatal(err)
		}
	case "discard":
		if err := staged.Discard(); err != nil {
			t.Fatal(err)
		}
	case canceledAction:
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if err := staged.Publish(ctx, Policy{}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case "failed":
		put(t, staged.Target(), "existing")

		if err := staged.Publish(t.Context(), Policy{}); err == nil {
			t.Fatal("no-clobber unexpectedly published")
		}
	default:
		t.Fatal("unknown action")
	}
}

func TestPublishRejectsSubstitutedStageBeforeTouchingTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	staged, err := Stage(t.Context(), filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
	if err != nil {
		t.Fatal(err)
	}

	lease := staged.owner.file
	put(t, staged.Target(), "existing report")

	if removeErr := os.Remove(staged.Path()); removeErr != nil {
		t.Fatal(removeErr)
	}

	put(t, staged.Path(), unknownReplacement)

	publishErr := staged.Publish(t.Context(), Policy{Overwrite: true})
	if !errors.Is(publishErr, errOwnerChanged) {
		t.Fatalf("foreign stage published: %v", publishErr)
	}

	if get(t, staged.Target()) != "existing report" || get(t, staged.Path()) != unknownReplacement {
		t.Fatal("ownership rejection changed bytes")
	}

	requireLeaseClosed(t, lease)
}

func TestOwnerRejectsSymlinkToPinnedOriginalAndPreservesNamespace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	staged, stageErr := Stage(t.Context(), filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	original := filepath.Join(dir, "retained-original")
	if err := os.Link(staged.Path(), original); err != nil {
		t.Fatalf("hardlink prerequisite: %v", err)
	}

	if err := os.Remove(staged.Path()); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(original, staged.Path()); err != nil {
		t.Fatalf("symlink prerequisite: %v", err)
	}

	lease := staged.owner.file
	if err := staged.Publish(t.Context(), Policy{}); !errors.Is(err, errOwnerChanged) {
		t.Fatalf("symlink published: %v", err)
	}

	info, err := os.Lstat(staged.Path())
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("unknown namespace removed: %v %v", info, err)
	}

	if get(t, original) != reportContent {
		t.Fatal("original bytes modified")
	}

	requireLeaseClosed(t, lease)
}

func TestOwnerCleanupMissingNamespaceClosesPin(t *testing.T) {
	t.Parallel()

	staged, stageErr := Stage(t.Context(), filepath.Join(t.TempDir(), reportPath), strings.NewReader(reportContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	lease := staged.owner.file
	if err := os.Remove(staged.Path()); err != nil {
		t.Fatal(err)
	}

	if err := staged.Discard(); err != nil {
		t.Fatal(err)
	}

	requireLeaseClosed(t, lease)
}

func TestDiscardWithoutAuthorityPreservesUnknownNamespace(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), reportPath)
	put(t, path, unknownReplacement)

	staged := &Staged{path: path}
	if err := staged.Discard(); !errors.Is(err, errOwnerReleased) {
		t.Fatalf("missing authority: %v", err)
	}

	if get(t, path) != unknownReplacement {
		t.Fatal("unowned path removed")
	}
}

func TestStagePinOpenFailureClosesWriterWithoutRemovingForeignPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ops := realOperations()

	var writer *os.File

	ops.createTemp = func(directory, pattern string) (*os.File, error) {
		file, createErr := os.CreateTemp(directory, pattern)
		if createErr != nil {
			return nil, fmt.Errorf(createFixtureErrorFormat, createErr)
		}

		writer = file
		if err := os.Remove(file.Name()); err != nil {
			t.Fatal(err)
		}

		return file, nil
	}

	_, err := stageWith(t.Context(), ops, filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing pin entry accepted: %v", err)
	}

	requireLeaseClosed(t, writer)
}

func TestStagePinRejectsInitialSymlinkToOriginalObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ops := realOperations()

	var (
		writer *os.File
		path   string
	)

	ops.createTemp = func(directory, pattern string) (*os.File, error) {
		file, createErr := os.CreateTemp(directory, pattern)
		if createErr != nil {
			return nil, fmt.Errorf(createFixtureErrorFormat, createErr)
		}

		writer, path = file, file.Name()

		original := filepath.Join(dir, "initial-original")
		if err := os.Link(path, original); err != nil {
			t.Fatalf("hardlink prerequisite: %v", err)
		}

		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(original, path); err != nil {
			t.Fatalf("symlink prerequisite: %v", err)
		}

		return file, nil
	}

	_, stageErr := stageWith(t.Context(), ops, filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
	if !errors.Is(stageErr, errOwnerChanged) {
		t.Fatalf("initial same-object symlink accepted: %v", stageErr)
	}

	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("substituted namespace lost: %v %v", info, err)
	}

	requireLeaseClosed(t, writer)
}
