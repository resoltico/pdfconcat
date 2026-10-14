// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func requireCampaignProducerExited(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		t.Fatalf("invalid native PID %d", pid)
		return
	}
	nativePID := uint64(pid)
	if nativePID > uint64(^uint32(0)) {
		t.Fatalf("invalid native PID %d", pid)
		return
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(nativePID))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	}()
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("producer not joined: %d/%v", result, err)
	}
}

func TestCampaignOwnerAcceptsNativeSystemAndTemporaryVolumes(t *testing.T) {
	t.Parallel()
	system := os.Getenv("SystemRoot")
	if system == "" {
		t.Fatal("native Windows system directory unavailable")
	}
	source, err := nativeCampaignDirectory(system)
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := nativeCampaignDirectory(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native source/temp volumes %q/%q; different-volume boundary=%v", filepath.VolumeName(source), filepath.VolumeName(temporary), !strings.EqualFold(filepath.VolumeName(source), filepath.VolumeName(temporary)))
	scratch, err := newCampaignScratch(system, "qualitygate-volume-control-")
	if err != nil {
		t.Fatal(err)
	}
	if err = scratch.close(); err != nil {
		t.Fatal(err)
	}
}
