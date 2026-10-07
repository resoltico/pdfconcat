//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOverwritePreservesPinnedOldObjectAndPublishesUnicodeName(t *testing.T) {
	t.Parallel()

	for _, padding := range []int{0, 1, 3, 7, 15, 31} {
		t.Run(strconv.Itoa(padding), func(t *testing.T) {
			t.Parallel()
			destination := filepath.Join(t.TempDir(), strings.Repeat("x", padding)+"atgūšana-α-😀.json")
			checkPinnedReplacement(t, destination)
		})
	}
}

func checkPinnedReplacement(t *testing.T, destination string) {
	t.Helper()
	put(t, destination, reportContent)

	lease, leaseErr := openReportLease(destination)
	if leaseErr != nil {
		t.Fatal(leaseErr)
	}

	owner := &RecoveryOwner{file: lease}

	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})

	original, statErr := lease.Stat()
	if statErr != nil {
		t.Fatal(statErr)
	}

	staged, stageErr := Stage(t.Context(), destination, strings.NewReader(replacementContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	sourceLease := staged.owner.file
	if err := staged.Publish(t.Context(), Policy{Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	if get(t, destination) != replacementContent {
		t.Fatal("replacement bytes were not published")
	}

	pinned, pinErr := lease.Stat()
	if pinErr != nil || !os.SameFile(original, pinned) {
		t.Fatalf("old object pin lost: %v", pinErr)
	}

	if err := owner.Verify(destination); !errors.Is(err, errOwnerChanged) {
		t.Fatalf("replacement accepted as old object: %v", err)
	}

	requireLeaseClosed(t, sourceLease)
}

func TestNoClobberPreservesPinnedDestination(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(t.TempDir(), reportPath)
	put(t, destination, reportContent)

	lease, leaseErr := openReportLease(destination)
	if leaseErr != nil {
		t.Fatal(leaseErr)
	}

	owner := &RecoveryOwner{file: lease}

	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})

	staged, stageErr := Stage(t.Context(), destination, strings.NewReader(replacementContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	if err := staged.Publish(t.Context(), Policy{}); err == nil {
		t.Fatal("no-clobber overwrote destination")
	}

	if get(t, destination) != reportContent {
		t.Fatal("no-clobber changed destination")
	}

	if err := owner.Verify(destination); err != nil {
		t.Fatal(err)
	}
}

func TestRenameInformationPreservesUTF16NameAndNativeAlignment(t *testing.T) {
	t.Parallel()

	for _, padding := range []int{0, 1, 3, 7, 15, 31} {
		t.Run(strconv.Itoa(padding), func(t *testing.T) {
			t.Parallel()
			destination := filepath.Join(t.TempDir(), strings.Repeat("x", padding)+"α😀.json")
			checkRenameBuffer(t, destination)
		})
	}
}

func checkRenameBuffer(t *testing.T, destination string) {
	t.Helper()

	buffer, bufferErr := renameInformationBuffer(destination)
	if bufferErr != nil {
		t.Fatal(bufferErr)
	}

	var header renameInformation

	offset := int(unsafe.Offsetof(header.FileName))
	nameBytes := int(binary.LittleEndian.Uint32(buffer[unsafe.Offsetof(header.FileNameLength):]))

	if len(buffer) < offset+nameBytes+2 || binary.LittleEndian.Uint16(buffer[offset+nameBytes:]) != 0 {
		t.Fatal("native path has no in-buffer terminating NUL")
	}

	units := make([]uint16, nameBytes/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(buffer[offset+index*2:])
	}

	if windows.UTF16ToString(units) != destination {
		t.Fatalf("incorrect UTF16 destination: %q", windows.UTF16ToString(units))
	}

	if binary.LittleEndian.Uint32(buffer) != 3 {
		t.Fatal("rename flags do not preserve open target handles")
	}

	if _, err := renameInformationBuffer("rename\x00target"); err == nil {
		t.Fatal("NUL path accepted")
	}
}

func TestOverwriteNativeFailuresPreserveOriginalBytes(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"missing source", "invalid source", "invalid destination", "read-only destination"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			staged, destination := filepath.Join(dir, "staged.json"), filepath.Join(dir, reportPath)
			put(t, staged, replacementContent)
			put(t, destination, reportContent)
			sourceArg, targetArg := refusedRenameArguments(t, scenario, staged, destination)

			if err := replaceFile(sourceArg, targetArg, replaceExisting); err == nil {
				t.Fatal("native refusal accepted")
			}

			if get(t, destination) != reportContent || get(t, staged) != replacementContent {
				t.Fatal("native refusal changed bytes")
			}
		})
	}
}

func refusedRenameArguments(t *testing.T, scenario, staged, destination string) (string, string) {
	t.Helper()

	sourceArg, targetArg := staged, destination

	switch scenario {
	case "missing source":
		sourceArg = filepath.Join(filepath.Dir(staged), "missing.json")
	case "invalid source":
		sourceArg = "source\x00name"
	case "invalid destination":
		targetArg = "target\x00name"
	case "read-only destination":
		if modeErr := os.Chmod(destination, 0o400); modeErr != nil {
			t.Fatal(modeErr)
		}

		t.Cleanup(func() {
			if modeErr := os.Chmod(destination, 0o600); modeErr != nil {
				t.Error(modeErr)
			}
		})
	default:
		t.Fatalf("unknown native refusal scenario: %s", scenario)
	}

	return sourceArg, targetArg
}

func TestReportPinRejectsNULInsteadOfOpeningValidPrefix(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), reportPath)
	put(t, path, reportContent)

	lease, err := openReportLease(path + "\x00suffix")
	if lease != nil {
		if closeErr := lease.Close(); closeErr != nil {
			t.Error(closeErr)
		}

		t.Fatal("invalid native pin path opened its prefix")
	}

	if err == nil || !strings.Contains(err.Error(), "encode report pin path") {
		t.Fatalf("native pin encoding failure lost: %v", err)
	}
}

func TestReportNamespaceGuardPreservesDisappearanceAfterLstat(t *testing.T) {
	t.Parallel()

	staged, stageErr := Stage(t.Context(), filepath.Join(t.TempDir(), reportPath), strings.NewReader(reportContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	t.Cleanup(func() {
		if discardErr := staged.Discard(); discardErr != nil {
			t.Error(discardErr)
		}
	})

	live, statErr := staged.owner.file.Stat()
	if statErr != nil {
		t.Fatal(statErr)
	}

	current, lstatErr := os.Lstat(staged.Path())
	if lstatErr != nil {
		t.Fatal(lstatErr)
	}

	if removeErr := os.Remove(staged.Path()); removeErr != nil {
		t.Fatal(removeErr)
	}

	err := checkReportNamespace(live, current, staged.Path())
	if err == nil || !strings.Contains(err.Error(), "open report identity pin") {
		t.Fatalf("native disappearance was not propagated: %v", err)
	}
}
