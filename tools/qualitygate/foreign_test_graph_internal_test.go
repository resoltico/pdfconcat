// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	compilerFixtureModule    = "example.test/foreign"
	compilerFixtureDirectory = "foreign"
	compilerFixtureEmbeds    = "fixtures"
)

func TestForeignCompilerGraphRejectsWildcardEmbedAdoptionAndSourceDrift(t *testing.T) {
	t.Parallel()
	root, stage := foreignCompilerFixture(t)

	patterns := []string{compilerFixtureModule}
	if err := stage.compilerEquivalent(t.Context(), root, patterns, testOptions{}); err != nil {
		t.Fatal(err)
	}

	canonical := filepath.Join(root, compilerFixtureDirectory)
	staged := stage.roots[canonical]
	writeForeignGraphFixture(t, filepath.Join(staged, compilerFixtureEmbeds, "added.raw"), "supplemental data adopted by wildcard embed")

	if err := stage.compilerEquivalent(t.Context(), root, patterns, testOptions{}); err == nil {
		t.Fatal("wildcard adoption accepted")
	}

	if err := os.Remove(filepath.Join(staged, compilerFixtureEmbeds, "added.raw")); err != nil {
		t.Fatal(err)
	}

	writeForeignGraphFixture(t, filepath.Join(staged, "source.go"), "package foreign\nconst changed = true\n")

	if err := stage.compilerEquivalent(t.Context(), root, patterns, testOptions{}); err == nil {
		t.Fatal("selected source drift accepted")
	}
}

func TestForeignCompilerGraphRejectsMissingPackageAndReplacement(t *testing.T) {
	t.Parallel()

	root, stage := foreignCompilerFixture(t)
	if err := stage.compilerEquivalent(t.Context(), root, []string{"example.test/foreign/missing"}, testOptions{race: true}); err == nil {
		t.Fatal("missing declared package accepted")
	}

	writeForeignGraphFixture(
		t,
		stage.modfile,
		"module example.test/main\ngo 1.27.1\nrequire example.test/foreign v1.0.0\nreplace example.test/foreign => ./foreign\n",
	)

	if err := stage.compilerEquivalent(t.Context(), root, []string{compilerFixtureModule}, testOptions{race: true}); err == nil {
		t.Fatal("substituted staged replacement accepted")
	}
}

func TestForeignSkipRejectionIncludesSubtests(t *testing.T) {
	t.Parallel()

	outcome := newTestOutcome()

	outcome.completed["example.test/foreign TestInput/subcase"] = actionSkip
	if err := rejectForeignSkips(outcome, []repopolicy.ForeignSource{{Module: compilerFixtureModule}}); err == nil {
		t.Fatal("foreign skipped subtest accepted")
	}

	outcome.completed = map[string]string{"example.test/owned TestOptional": actionSkip}
	if err := rejectForeignSkips(outcome, []repopolicy.ForeignSource{{Module: compilerFixtureModule}}); err != nil {
		t.Fatal(err)
	}
}

func foreignCompilerFixture(t *testing.T) (string, *foreignTestStage) {
	t.Helper()

	root, resolveErr := filepath.EvalSymlinks(t.TempDir())
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}

	canonical := filepath.Join(root, compilerFixtureDirectory)

	staged := filepath.Join(t.TempDir(), compilerFixtureDirectory)
	for _, directory := range []string{canonical, staged} {
		writeForeignGraphFixture(t, filepath.Join(directory, moduleFileName), "module example.test/foreign\ngo 1.27.1\n")
		writeForeignGraphFixture(
			t,
			filepath.Join(directory, "source.go"),
			"package foreign\nimport \"embed\"\n//go:embed fixtures/*\nvar resource embed.FS\n",
		)
		writeForeignGraphFixture(t, filepath.Join(directory, compilerFixtureEmbeds, "original.raw"), "original canonical embed input")
	}

	writeForeignGraphFixture(
		t,
		filepath.Join(root, moduleFileName),
		"module example.test/main\ngo 1.27.1\nrequire example.test/foreign v1.0.0\nreplace example.test/foreign => ./foreign\n",
	)
	writeForeignGraphFixture(t, filepath.Join(root, "go.sum"), "")

	stage := &foreignTestStage{
		directory: t.TempDir(),
		roots:     map[string]string{canonical: staged},
		sources: []repopolicy.ForeignSource{
			{Module: compilerFixtureModule, Version: fixtureModuleVersion, Root: compilerFixtureDirectory},
		},
	}
	if err := stage.deriveModuleFiles(root); err != nil {
		t.Fatal(err)
	}

	cache, err := goCommand(root, "env", "GOCACHE").output(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	stage.goCache = strings.TrimSpace(cache)

	return root, stage
}

func writeForeignGraphFixture(t *testing.T, name, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(name), foreignStageDirectoryMode); err != nil {
		t.Fatal(err)
	}

	tree, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(tree)

	if writeErr := tree.WriteFile(filepath.Base(name), []byte(text), fileMode); writeErr != nil {
		t.Fatal(writeErr)
	}
}
