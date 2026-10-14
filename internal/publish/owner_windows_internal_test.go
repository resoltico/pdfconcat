// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package publish

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsReportLeaseSharesConcurrentReadWriteDeleteOwner(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), reportPath)
	put(t, path, reportContent)
	openConcurrentReportOwner(t, path, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE)

	lease, err := openReportLease(path)
	if err != nil {
		t.Fatalf("metadata lease conflicted with concurrent read/write/delete owner: %v", err)
	}
	owner := &RecoveryOwner{file: lease}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := owner.Verify(path); err != nil {
		t.Fatalf("concurrent owner changed report identity verification: %v", err)
	}
}

func TestWindowsReportLeasePreservesNativeOpenErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		path string
		want error
	}{
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing.json"), want: os.ErrNotExist},
		{name: "directory requires backup semantics", path: t.TempDir(), want: windows.ERROR_ACCESS_DENIED},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lease, err := openReportLease(test.path)
			if lease != nil {
				if closeErr := lease.Close(); closeErr != nil {
					t.Error(closeErr)
				}
				t.Error("failed identity lease returned a file")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("native identity lease failure = %v, want %v", err, test.want)
			}
		})
	}
}
