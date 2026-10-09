// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func foreignGraphFixture(t *testing.T) (string, []repopolicy.ForeignSource, []discoveredPackage) {
	t.Helper()
	root := t.TempDir()
	sources := []repopolicy.ForeignSource{
		{Root: "third_party/benoitkugler/pdf", Module: "github.com/benoitkugler/pdf", Version: "v0.0.15"},
		{Root: "third_party/benoitkugler/pstokenizer", Module: "github.com/benoitkugler/pstokenizer", Version: "v1.0.1"},
	}

	packages := make([]discoveredPackage, 0, len(sources))

	for index := range sources {
		source := &sources[index]
		directory := filepath.Join(root, filepath.FromSlash(source.Root))
		packages = append(packages, discoveredPackage{
			ImportPath: source.Module, Dir: directory,
			Module: &discoveredModule{
				Path: source.Module, Version: source.Version, Dir: directory,
				Replace: &discoveredModule{Path: "./" + source.Root, Dir: directory},
			},
		})
	}

	return root, sources, packages
}

func foreignGraphJSON(t *testing.T, packages []discoveredPackage) string {
	t.Helper()

	var output strings.Builder

	encoder := json.NewEncoder(&output)
	for index := range packages {
		if err := encoder.Encode(&packages[index]); err != nil {
			t.Fatal(err)
		}
	}

	return output.String()
}

func TestForeignGraphIncludesTransitivePackagesAndExcludesSyntheticTests(t *testing.T) {
	t.Parallel()
	root, sources, packages := foreignGraphFixture(t)
	transitive := packages[0]
	transitive.ImportPath += "/reader/parser/filters"
	transitive.Dir = filepath.Join(transitive.Dir, "reader", "parser", "filters")
	testVariant := transitive
	testVariant.ForTest = transitive.ImportPath
	testVariant.ImportPath += " [" + transitive.ImportPath + ".test]"
	testMain := packages[1]
	testMain.Name, testMain.ImportPath = "main", testMain.ImportPath+".test"
	packages = append(packages, transitive, testVariant, testMain)

	selected, err := parseForeignPackages(root, foreignGraphJSON(t, packages), sources)
	if err != nil || len(selected) != 3 || !strings.Contains(strings.Join(selected, "\n"), transitive.ImportPath) {
		t.Fatalf("effective package selection lost transitive source: %v %v", selected, err)
	}
}

func TestForeignGraphRejectsMissingOrSubstitutedSource(t *testing.T) {
	t.Parallel()

	for _, corrupt := range []func([]discoveredPackage) []discoveredPackage{
		func([]discoveredPackage) []discoveredPackage { return nil },
		func(packages []discoveredPackage) []discoveredPackage { return packages[:1] },
		func(packages []discoveredPackage) []discoveredPackage {
			packages[1].Module.Replace = nil
			return packages
		},
		func(packages []discoveredPackage) []discoveredPackage {
			packages[1].Module.Dir = "/cache"
			return packages
		},
		func(packages []discoveredPackage) []discoveredPackage {
			packages[0].Module.Replace.Dir = "/outside"
			return packages
		},
		func(packages []discoveredPackage) []discoveredPackage {
			packages[0].Module.Replace.Path = "../outside"
			return packages
		},
		func(packages []discoveredPackage) []discoveredPackage {
			packages[0].Module.Version = "v0.0.14"
			return packages
		},
		func(packages []discoveredPackage) []discoveredPackage { packages[0].Dir = "/outside"; return packages },
		func(packages []discoveredPackage) []discoveredPackage {
			packages[0].Error = &discoveryError{Err: "broken"}
			return packages
		},
		func(packages []discoveredPackage) []discoveredPackage {
			packages[0].ImportPath = "other.test/package"
			return packages
		},
	} {
		root, sources, packages := foreignGraphFixture(t)
		if _, err := parseForeignPackages(root, foreignGraphJSON(t, corrupt(packages)), sources); err == nil {
			t.Fatal("incomplete or substituted foreign source accepted")
		}
	}

	root, sources, _ := foreignGraphFixture(t)
	if _, err := parseForeignPackages(root, "{", sources); err == nil {
		t.Fatal("malformed dependency discovery accepted")
	}
}
