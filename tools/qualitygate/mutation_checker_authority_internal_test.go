// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestMutationCheckerUsesFrozenPinsBeforeBinaryControls(t *testing.T) {
	t.Parallel()

	lookup, snapshot := t.TempDir(), t.TempDir()
	name := ".tools/bin/golangci-lint"

	if runtime.GOOS == windowsOS {
		name += exeSuffix
	}

	writeMutationAuthorityFixture(t, lookup, name, []byte("fixture is not a checker"))
	writeMutationAuthorityFixture(t, snapshot, toolVersionsFileName, []byte("broken frozen pins"))

	_, err := mutationEnvironment(t.Context(), &mutationOptions{root: lookup, snapshot: snapshot, integration: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "parse tools/versions.env") {
		t.Fatalf("frozen metadata must fail before external checker inspection: %v", err)
	}
}
