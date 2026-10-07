// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteChunkReportsWriteFailure(t *testing.T) {
	t.Parallel()

	file, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}

	err = file.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = writeChunk(file, []byte("x"))
	if err == nil {
		t.Fatal("writeChunk() succeeded on a closed file")
	}
}

func TestDestinationInspectionFailureIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	put(t, file, "x")

	err := CheckDestination(filepath.Join(file, "child"), true)
	if err == nil {
		t.Fatal("CheckDestination() accepted a path below a regular file")
	}

	err = File(file, filepath.Join(file, "child"), true)

	failure, isDestination := errors.AsType[*DestinationError](err)
	if !isDestination || failure.Subject != "output directory" || failure.Path != file {
		t.Fatalf("File() = %v", err)
	}
}

func TestReplaceFileReportsNativeOverwriteFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := replaceFile(filepath.Join(dir, missingPath), filepath.Join(dir, "out"), replaceExisting)
	if err == nil {
		t.Fatal("replaceFile() succeeded without a staged file")
	}
}

func TestRemoveStageReportsUnaddressablePath(t *testing.T) {
	t.Parallel()

	// A path with a NUL byte can never be removed on any platform.
	err := removeStaged("unremovable\x00file")
	if err == nil || !strings.Contains(err.Error(), "remove staged") {
		t.Fatalf("removeStaged() = %v", err)
	}
}

func TestReplaceFileRefusesPathsTheSystemCannotAddressAndLeavesBothFilesAlone(t *testing.T) {
	t.Parallel()

	for name, existing := range map[string]existingFile{"no-clobber": refuseExisting, "overwrite": replaceExisting} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			staged := filepath.Join(dir, "staged")
			put(t, staged, stagedContent)

			// A NUL byte cannot be part of a path on any platform, whichever way the path is encoded.
			unaddressable := dir + string(filepath.Separator) + "bad\x00name"
			valid := filepath.Join(dir, "out")

			for _, paths := range [][2]string{{staged, unaddressable}, {unaddressable, valid}} {
				err := replaceFile(paths[0], paths[1], existing)
				if err == nil {
					t.Fatalf("replaceFile(%q, %q) succeeded", paths[0], paths[1])
				}
			}

			if get(t, staged) != stagedContent || len(names(t, dir)) != 1 {
				t.Fatalf("the failed replacements changed the directory: %v", names(t, dir))
			}
		})
	}
}

func TestDestinationInspectionRejectsUnaddressableNameAndPreservesStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	put(t, staged, stagedContent)

	destination := dir + string(filepath.Separator) + "invalid-output\x00"

	err := File(staged, destination, true)

	pathErr, isPath := errors.AsType[*os.PathError](err)
	if !isPath || pathErr.Path != destination || !strings.Contains(err.Error(), "inspect output") {
		t.Fatalf("unaddressable destination lost its cause or path: %v", err)
	}

	if get(t, staged) != stagedContent || len(names(t, dir)) != 1 {
		t.Fatal("destination inspection failure changed staged bytes or namespace")
	}
}
