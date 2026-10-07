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

const gremlinsPatchEnv = "GREMLINS_PATCH_SHA256"

func TestGremlinsSourceIdentityRequiresEveryReviewedInput(t *testing.T) {
	t.Parallel()

	versions := map[string]string{
		gremlinsVersionEnv:           "v0.6.0",
		"GREMLINS_SOURCE_ZIP_SHA256": strings.Repeat("a", 64),
		gremlinsPatchEnv:             strings.Repeat("b", 64),
		"GREMLINS_SOURCE_SUM":        "h1:3G2ROO0I3q4bb5bxElQIUITTuEbl1iOfVYFqunGwrJI=",
		"GREMLINS_SOURCE_MOD_SUM":    "h1:LLbvJR33CWsu1sgvQ4qMzU2rqkwYJK3Qy/Al59eHKjA=",
		"GREMLINS_SOURCE_TIME":       "2025-12-05T05:14:22Z",
	}

	identity, err := repopolicy.GremlinsBuildIdentity(versions)
	if err != nil {
		t.Fatal(err)
	}

	if identity.Version != "0.6.0+executor."+versions[gremlinsPatchEnv] {
		t.Fatalf("executor variant not bound to patch: %+v", identity)
	}

	for key := range versions {
		malformed := maps.Clone(versions)

		malformed[key] = "bad"
		if _, malformedErr := repopolicy.GremlinsBuildIdentity(malformed); malformedErr == nil {
			t.Fatalf("malformed %s accepted", key)
		}

		missing := maps.Clone(versions)
		delete(missing, key)

		if _, missingErr := repopolicy.GremlinsBuildIdentity(missing); missingErr == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
}

func TestGremlinsIdentityBindsTheReviewedSourcePatch(t *testing.T) {
	t.Parallel()

	versions, parseErr := repopolicy.ParseToolVersions(string(readRepoFile(t, toolVersionsPath)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}

	patch := readRepoFile(t, "tools/mutation-patches/gremlins-executor.patch")

	digest := sha256.Sum256(patch)
	if versions[gremlinsPatchEnv] != hex.EncodeToString(digest[:]) {
		t.Fatal("reviewed mutation source patch differs from its identity pin")
	}

	identity, identityErr := repopolicy.GremlinsBuildIdentity(versions)

	want := strings.TrimPrefix(versions[gremlinsVersionEnv], "v") + "+executor." + hex.EncodeToString(digest[:])
	if identityErr != nil || identity.Version != want {
		t.Fatalf("mutation source identity does not bind the exact patch: %+v %v", identity, identityErr)
	}
}
