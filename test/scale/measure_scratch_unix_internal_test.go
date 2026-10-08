// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

const sleepingChildExecutable = "/bin/sleep"

func TestScratchInspectionCountsOnlyRegularFilesAndAllowsVanishedWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	data := []byte("owned scratch bytes")
	if err := os.WriteFile(filepath.Join(root, "regular"), data, fileMode); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(target, make([]byte, 1000), fileMode); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, filepath.Join(root, "symlink")); err != nil {
		t.Fatalf("required symlink capability unavailable: %v", err)
	}

	total, err := directoryBytes(root)
	if err != nil || total != int64(len(data)) {
		t.Fatalf("regular scratch accounting: %d %v", total, err)
	}

	vanished := filepath.Join(root, "vanished")
	if mkdirErr := os.Mkdir(vanished, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}

	if removeErr := os.Remove(vanished); removeErr != nil {
		t.Fatal(removeErr)
	}

	total, err = directoryBytes(vanished)
	if err != nil || total != 0 {
		t.Fatalf("vanished workspace rejected: %d %v", total, err)
	}
}

func TestScratchInspectionUnreadableRootStopsMeasurementWithKnownPeak(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	denyScratchDirectoryReads(t, root)

	watch := processWatch{peaks: processPeaks{scratchBytes: 123}}
	if watch.sampleScratch(root) || !errors.Is(watch.scratchErr, os.ErrPermission) || watch.peaks.scratchBytes != 123 {
		t.Fatalf("scratch fault erased: %+v %v", watch.peaks, watch.scratchErr)
	}

	measurement, measureErr := Measure(t.Context(), RunSpec{Binary: sleepingChildExecutable, Args: []string{"0.03"}, ScratchDir: root})
	if !errors.Is(measureErr, os.ErrPermission) || len(measurement.ReadingFailures) == 0 {
		t.Fatalf("real executable concealed scratch error: %+v %v", measurement, measureErr)
	}

	failure := watch.peaks.failures[0]
	if failure.Metric != "scratch" || failure.Phase != "live_error" {
		t.Fatalf("scratch category lost: %+v", failure)
	}

	assertReadingFailureEvidence(t, watch.peaks.failures)
}

func TestScratchInspectionOperationalPathErrorIsNotMissingWorkspace(t *testing.T) {
	t.Parallel()

	total, err := directoryBytes("invalid\x00scratch")

	var pathErr *os.PathError
	if total != 0 || !errors.As(err, &pathErr) || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("operational scratch error discarded: %d %v", total, err)
	}
}

func denyScratchDirectoryReads(t *testing.T, root string) {
	t.Helper()
	permissiontest.RequireEnforcement(t)

	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}

	if chmodErr := os.Chmod(root, 0); chmodErr != nil {
		t.Fatal(chmodErr)
	}

	t.Cleanup(func() {
		if restoreErr := os.Chmod(root, info.Mode().Perm()); restoreErr != nil {
			t.Error(restoreErr)
		}
	})
}

func assertReadingFailureEvidence(t *testing.T, failures []ReadingFailure) {
	t.Helper()

	for index := range failures {
		failure := &failures[index]
		if failure.Cause == "" || failure.ReadStarted.IsZero() || failure.ReadFinished.Before(failure.ReadStarted) {
			t.Fatalf("error evidence lost: %+v", failure)
		}
	}
}

func TestScratchInspectionPreservesActuallyCountedBytesOnReadFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	data := []byte("known partial scratch bytes")
	if err := os.WriteFile(filepath.Join(root, "a-regular"), data, fileMode); err != nil {
		t.Fatal(err)
	}

	inaccessible := filepath.Join(root, "z-inaccessible")
	if err := os.Mkdir(inaccessible, 0o700); err != nil {
		t.Fatal(err)
	}

	denyScratchDirectoryReads(t, inaccessible)

	total, err := directoryBytes(root)
	if total != int64(len(data)) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("actual partial scratch bytes lost: %d %v", total, err)
	}
}
