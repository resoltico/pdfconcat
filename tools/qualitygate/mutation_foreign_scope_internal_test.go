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

func TestMutationScopeCoversPhysicalSourcesOutsideCompiledRuntime(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	module, err := modulePath(root)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := mutationScope(t.Context(), root, module, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := repopolicy.OwnedGoSources(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	patterns := make([]*regexp.Regexp, len(scope.excludes))
	for index, expression := range scope.excludes {
		patterns[index] = regexp.MustCompile(expression)
	}
	for _, file := range files {
		excluded := false
		for _, pattern := range patterns {
			if pattern.MatchString(file) {
				excluded = true
				break
			}
		}
		if scope.hostFiles[file] == excluded {
			t.Fatalf("physical mutation selection is inconsistent for %s: runtime=%t excluded=%t", file, scope.hostFiles[file], excluded)
		}
	}
}

func TestMutationListingUsesModulePackageIdentity(t *testing.T) {
	const module = "example.org/product"
	scope := scopeOfMutation{hostFiles: map[string]bool{}}
	listing := module + "/internal/assembly|origin.go,\n" + module + "/tools/check|main.go,"
	err := scope.collectRuntimeListing(module, listing, map[string]bool{module + "/internal/assembly": true}, nil)
	if err != nil || !scope.hostFiles["internal/assembly/origin.go"] || scope.packages != 1 {
		t.Fatalf("module-relative source inventory: %v, %v", scope, err)
	}
	for _, invalid := range []string{"example.org/product-other/internal/assembly|origin.go,", module + "/../outside|origin.go,"} {
		if err := scope.collectRuntimeListing(module, invalid, nil, nil); err == nil {
			t.Fatalf("accepted package outside module: %s", invalid)
		}
	}
}
