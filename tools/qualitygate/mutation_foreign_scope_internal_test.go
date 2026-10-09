// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"regexp"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const mutationObservationPackage = "internal/observation"

func TestMutationScopeExcludesValidatedNestedForeignRootsOnly(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	sources, err := repopolicy.ForeignSourcesContext(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	module, err := modulePath(root)
	if err != nil {
		t.Fatal(err)
	}

	scope, err := mutationScope(t.Context(), root, module, []string{mutationObservationPackage})
	if err != nil {
		t.Fatal(err)
	}

	for _, source := range sources {
		wanted := "^" + regexp.QuoteMeta(source.Root) + "/"
		found := false

		for _, pattern := range scope.excludes {
			if pattern == wanted {
				found = true
			}
		}

		if !found {
			t.Fatalf("validated foreign root missing: %s", source.Root)
		}

		if regexp.MustCompile(wanted).MatchString(source.Root + "-owned/guard.go") {
			t.Fatal("foreign exclusion hides neighboring owned source")
		}
	}

	if !scope.hostFiles["internal/observation/observation.go"] {
		t.Fatalf("host source lost: %v", scope.hostFiles)
	}
}
