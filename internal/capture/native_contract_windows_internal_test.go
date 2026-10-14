// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package capture

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsNativeObservationCooperatesWithReadWriteDeleteLeases(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	file := filepath.Join(directory, "leased.pdf")
	writeTestFile(t, file, []byte("identity while another owner can read, write and delete"))

	for _, path := range []string{directory, file} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			handle := nativeReadWriteDeleteLease(t, path)
			var info windows.ByHandleFileInformation
			if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
				t.Fatal(err)
			}
			// Multiplication and addition independently combine the two disjoint
			// native fields, including any high-order file-index bits.
			want := Identity{volume: uint64(info.VolumeSerialNumber), index: uint64(info.FileIndexHigh)*4294967296 + uint64(info.FileIndexLow)}
			if want.index == 0 {
				t.Fatal("native fixture did not supply a nonzero file index")
			}
			got, isDirectory, err := inspectIdentity(path)
			if err != nil || got != want || isDirectory != (path == directory) {
				t.Fatalf("native observation conflicted with compatible lease or lost identity bits: %v/%v/%v; want %v", got, isDirectory, err, want)
			}
			if path == directory {
				resolved, pathErr := nativeParentPath(path)
				identity, identityErr := IdentityOf(resolved)
				if pathErr != nil || identityErr != nil || identity != want {
					t.Fatalf("parent observation conflicted with compatible lease: %q/%v/%v/%v", resolved, pathErr, identity, identityErr)
				}
			}
		})
	}
}

func nativeReadWriteDeleteLease(t *testing.T, path string) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	})
	return handle
}

func TestWindowsIdentityShareModePermitsConcurrentDataOwners(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	file := filepath.Join(directory, "shared.pdf")
	writeTestFile(t, file, []byte("concurrent native sharing contract"))
	for _, path := range []string{directory, file} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			nativeReadWriteDeleteLease(t, path)
			name, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			// Metadata-only opens can bypass sharing conflicts. Exercise the
			// configured flags with real data/delete access to prove their native
			// semantics, while inspectIdentity continues to request zero access.
			handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
				identityShareMode, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
			if err != nil {
				t.Fatalf("identity sharing configuration conflicted with a native read/write/delete owner: %v", err)
			}
			if err := windows.CloseHandle(handle); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWindowsNativeParentRejectsUnrepresentableUTF16Target(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	raw, err := windows.UTF16FromString(filepath.Join(directory, "unpaired-"))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], 0xd800, 0)
	if err := windows.CreateDirectory(&raw[0], nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.RemoveDirectory(&raw[0]); err != nil {
			t.Error(err)
		}
	})

	alias := filepath.Join(directory, "parent-alias")
	link, err := windows.UTF16PtrFromString(alias)
	if err != nil {
		t.Fatal(err)
	}
	// Native flag SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE (0x2),
	// also used by os.Symlink on supported Windows hosts.
	if err := windows.CreateSymbolicLink(link, &raw[0], windows.SYMBOLIC_LINK_FLAG_DIRECTORY|0x2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(alias); err != nil {
			t.Error(err)
		}
	})
	requireNativeUnpairedSurrogateTarget(t, alias)
	if path, err := nativeParentPath(alias); path != "" || !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("unrepresentable native parent was accepted with a lossy name: %q/%v", path, err)
	}
}

func requireNativeUnpairedSurrogateTarget(t *testing.T, alias string) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(alias)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	}()
	var buffer [32768]uint16
	count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil || count >= uint32(len(buffer)) || !slices.Contains(buffer[:count], 0xd800) {
		t.Fatalf("native fixture did not preserve the unpaired surrogate: %d/%v", count, err)
	}
}

// The W API excludes the NUL on success and includes it in an insufficient
// buffer's required size. Exactly buffer-length returned characters therefore
// cannot denote either a successful fit or a too-small buffer.
func TestWindowsFinalPathBufferBoundaryContract(t *testing.T) {
	t.Parallel()
	handle := nativeReadWriteDeleteLease(t, t.TempDir())
	var full [32768]uint16
	count, err := windows.GetFinalPathNameByHandle(handle, &full[0], uint32(len(full)), 0)
	if err != nil || count == 0 || count >= uint32(len(full)) {
		t.Fatalf("complete native path prerequisite: %d/%v", count, err)
	}
	tooSmall := make([]uint16, count)
	required, err := windows.GetFinalPathNameByHandle(handle, &tooSmall[0], count, 0)
	if err != nil || required != count+1 || required <= count {
		t.Fatalf("insufficient buffer did not require space beyond its end: %d/%v", required, err)
	}
	exact := make([]uint16, required)
	written, err := windows.GetFinalPathNameByHandle(handle, &exact[0], required, 0)
	if err != nil || written != count || exact[written] != 0 || !slices.Equal(exact[:written], full[:count]) {
		t.Fatalf("exact NUL-inclusive allocation did not return the same NUL-exclusive path: %d/%v", written, err)
	}
}

func TestWindowsNativeParentPreservesSupplementaryUnicode(t *testing.T) {
	t.Parallel()
	parent := filepath.Join(t.TempDir(), "日本語-😀-\ufffd")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	want, err := IdentityOf(parent)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := nativeParentPath(parent)
	if err != nil {
		t.Fatal(err)
	}
	got, err := IdentityOf(resolved)
	if err != nil || got != want {
		t.Fatalf("lossless Unicode parent changed its native identity: %q/%v/%v; want %v", resolved, got, err, want)
	}
}
