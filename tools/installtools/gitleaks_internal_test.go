// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestSecretScannerOfficialAssetsCoverEverySupportedTarget(t *testing.T) {
	t.Parallel()

	var scanner archiveTool

	for _, tool := range archiveTools() {
		if tool.name == "gitleaks" {
			scanner = tool
		}
	}

	if scanner.checksumPinKey != "GITLEAKS_CHECKSUMS_SHA256" || scanner.repo != "gitleaks/gitleaks" {
		t.Fatalf("scanner has no authoritative official-manifest pin: %+v", scanner)
	}

	for _, expected := range []struct{ os, arch, asset string }{
		{darwinOS, arm64Arch, "gitleaks_8.30.1_darwin_arm64.tar.gz"},
		{darwinOS, amd64Arch, "gitleaks_8.30.1_darwin_x64.tar.gz"},
		{linuxOS, arm64Arch, "gitleaks_8.30.1_linux_arm64.tar.gz"},
		{linuxOS, amd64Arch, "gitleaks_8.30.1_linux_x64.tar.gz"},
		{windowsOS, arm64Arch, "gitleaks_8.30.1_windows_arm64.zip"},
		{windowsOS, amd64Arch, "gitleaks_8.30.1_windows_x64.zip"},
	} {
		if got := scanner.asset(expected.os, expected.arch, "8.30.1"); got != expected.asset {
			t.Fatalf("%s/%s: wrong official asset %s", expected.os, expected.arch, got)
		}
	}
}

func TestPinnedManifestRejectsTamperingAndMissingReference(t *testing.T) {
	t.Parallel()

	original := []byte("reviewed official checksum manifest")
	digest := sha256.Sum256(original)
	pin := hex.EncodeToString(digest[:])

	if validErr := verifyManifestDigest(original, pin); validErr != nil {
		t.Fatal(validErr)
	}

	for _, corrupted := range []struct {
		pin     string
		content []byte
	}{{pin, []byte("changed release manifest")}, {"", original}, {"not-sha256", original}} {
		if rejectedErr := verifyManifestDigest(corrupted.content, corrupted.pin); !errors.Is(rejectedErr, errInstall) {
			t.Fatalf("unverified manifest accepted: %v", rejectedErr)
		}
	}
}
