// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestGolangciBuildIdentityBindsReviewedPatchAndStableUpstream(t *testing.T) {
	t.Parallel()

	versions, err := repopolicy.ParseToolVersions(string(readRepoFile(t, toolVersionsPath)))
	if err != nil {
		t.Fatal(err)
	}

	patch := readRepoFile(t, "tools/lint-patches/golangci-lint-physical-source.patch")

	digest := sha256.Sum256(patch)
	if versions["GOLANGCI_LINT_PATCH_SHA256"] != hex.EncodeToString(digest[:]) {
		t.Fatal("reviewed patch does not match identity pin")
	}

	identity, err := repopolicy.GolangciBuildIdentity(versions)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(identity.Version, strings.TrimPrefix(versions["GOLANGCI_LINT_VERSION"], "v")+"+physical-source.") ||
		identity.Commit != "patch-sha256:"+versions["GOLANGCI_LINT_PATCH_SHA256"] ||
		identity.Date != "upstream-source:"+versions["GOLANGCI_LINT_SOURCE_TIME"] {
		t.Fatalf("tool identity misstates provenance: %+v", identity)
	}

	for _, key := range []string{
		"GOLANGCI_LINT_VERSION", "GOLANGCI_LINT_SOURCE_ZIP_SHA256", "GOLANGCI_LINT_SOURCE_SUM",
		"GOLANGCI_LINT_SOURCE_MOD_SUM", "GOLANGCI_LINT_SOURCE_TIME", "GOLANGCI_LINT_PATCH_SHA256",
	} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			bad := maps.Clone(versions)

			bad[key] = "invalid"
			if _, pinErr := repopolicy.GolangciBuildIdentity(bad); pinErr == nil {
				t.Fatal("invalid build pin accepted")
			}
		})
	}
}
