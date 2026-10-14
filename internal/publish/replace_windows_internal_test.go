//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
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

	staged, stageErr := Stage(StageTestContext(t), destination, readerRequiringStorage(strings.NewReader(replacementContent)), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	sourceLease := staged.owner.file
	concurrent := openConcurrentReportOwner(t, staged.Path(), windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err := staged.Publish(t.Context(), Policy{Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	if get(t, destination) != replacementContent {
		t.Fatal("replacement bytes were not published")
	}
	currentOwner := openConcurrentReportOwner(t, destination, windows.GENERIC_READ|windows.GENERIC_WRITE)
	current, currentErr := currentOwner.Stat()
	concurrentInfo, concurrentErr := concurrent.Stat()
	if currentErr != nil || concurrentErr != nil || !os.SameFile(current, concurrentInfo) {
		t.Fatalf("concurrent staged owner lost published identity: %v / %v", currentErr, concurrentErr)
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

func openConcurrentReportOwner(t *testing.T, path string, access uint32) *os.File {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(handle), path)
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	return file
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

	staged, stageErr := Stage(StageTestContext(t), destination, readerRequiringStorage(strings.NewReader(replacementContent)), 1000)
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

	buffer, bufferSize, bufferErr := renameInformationBuffer(destination)
	if bufferErr != nil {
		t.Fatal(bufferErr)
	}
	if uint64(len(buffer)) != uint64(bufferSize) {
		t.Fatalf("native buffer count = %d, allocated %d bytes", bufferSize, len(buffer))
	}

	var header renameInformation

	offset := int(unsafe.Offsetof(header.FileName))
	// FILE_RENAME_INFO uses the Windows 64-bit HANDLE alignment on both supported targets.
	if offset != 20 || unsafe.Offsetof(header.RootDirectory) != 8 ||
		unsafe.Offsetof(header.FileNameLength) != 16 || unsafe.Sizeof(header) != 24 {
		t.Fatal("rename information no longer matches the native FILE_RENAME_INFO layout")
	}
	nameBytes := int(binary.LittleEndian.Uint32(buffer[unsafe.Offsetof(header.FileNameLength):]))
	absolute, absoluteErr := filepath.Abs(destination)
	if absoluteErr != nil {
		t.Fatal(absoluteErr)
	}
	wantUnits := utf16.Encode([]rune(absolute))
	if nameBytes != 2*len(wantUnits) {
		t.Fatalf("native name byte count = %d, want %d without the terminating NUL", nameBytes, 2*len(wantUnits))
	}
	if len(buffer) != offset+nameBytes+2 {
		t.Fatalf("rename buffer has %d bytes, want %d through the terminating NUL", len(buffer), offset+nameBytes+2)
	}

	if binary.LittleEndian.Uint16(buffer[offset+nameBytes:]) != 0 {
		t.Fatal("native path has no in-buffer terminating NUL")
	}

	for index, unit := range wantUnits {
		if actual := binary.LittleEndian.Uint16(buffer[offset+index*2:]); actual != unit {
			t.Fatalf("UTF16 unit %d = %04x, want %04x", index, actual, unit)
		}
	}

	if binary.LittleEndian.Uint32(buffer) != 3 {
		t.Fatal("rename flags do not preserve open target handles")
	}

	if _, _, err := renameInformationBuffer("rename\x00target"); err == nil {
		t.Fatal("NUL path accepted")
	}
}

func TestRenameInformationSizesRespectNativeAndAllocationBounds(t *testing.T) {
	t.Parallel()

	// Win64 FILE_RENAME_INFO has a 20-byte name offset and a 24-byte header.
	// The DWORD buffer count must also include the two-byte terminating NUL.
	for _, item := range []struct {
		units     uint64
		nameBytes uint32
		buffer    uint32
	}{
		{0, 0, 24},
		{1, 2, 24},
		{2, 4, 26},
		{2_147_483_635, 4_294_967_270, 4_294_967_292},
		{2_147_483_636, 4_294_967_272, 4_294_967_294},
	} {
		nameBytes, bufferSize, err := renameInformationSizes(item.units)
		if err != nil || nameBytes != item.nameBytes || bufferSize != item.buffer {
			t.Fatalf("%d UTF16 units: got %d/%d/%v, want %d/%d", item.units,
				nameBytes, bufferSize, err, item.nameBytes, item.buffer)
		}
	}
	for _, units := range []uint64{2_147_483_637, math.MaxUint64} {
		nameBytes, bufferSize, err := renameInformationSizes(units)
		if !errors.Is(err, errRenameInformationTooLarge) || nameBytes != 0 || bufferSize != 0 {
			t.Fatalf("unrepresentable %d UTF16 units accepted: %d/%d/%v", units, nameBytes, bufferSize, err)
		}
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

	staged, stageErr := Stage(StageTestContext(t), filepath.Join(t.TempDir(), reportPath), readerRequiringStorage(strings.NewReader(reportContent)), 1000)
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
