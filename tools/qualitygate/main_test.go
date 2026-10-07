// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	main "github.com/resoltico/pdfconcat/tools/qualitygate"
)

// archiveFixture is a dist directory and the repository files it must mirror.
type archiveFixture struct {
	root string
	dist string
}

const (
	eventPackage = "m/p"
	licenseName  = "LICENSE"
	noticesName  = "THIRD_PARTY_NOTICES.md"
)

func TestReplaceAnchoredLineRequiresExactlyOneMatch(t *testing.T) {
	t.Parallel()

	source := []byte("func f() {\n\tif x > 0 {\n\t\treturn\n\t}\n\tif y > 0 {\n\t}\n}\n")

	got, err := main.AnchoredReplacement("a.go", source, "if x > 0 {", "if x >= 0 {")
	if err != nil || !strings.Contains(string(got), "\tif x >= 0 {\n") {
		t.Fatalf("got %q, %v; the indentation must be kept", got, err)
	}

	for name, anchor := range map[string]string{"absent": "if z > 0 {", "ambiguous": "}"} {
		_, err = main.AnchoredReplacement("a.go", source, anchor, "x")
		if err == nil || !strings.Contains(err.Error(), "want exactly 1") {
			t.Fatalf("%s anchor: got %v, want a clear anchor error", name, err)
		}
	}
}

func TestEventsRequirePassingTestsAndDiscovery(t *testing.T) {
	t.Parallel()

	pass := strings.Join([]string{
		`{"Action":"start","Package":"m/p"}`,
		`{"Action":"run","Package":"m/p","Test":"TestA"}`,
		`{"Action":"pass","Package":"m/p","Test":"TestA"}`,
		`{"Action":"pass","Package":"m/p"}`,
	}, "\n") + "\n"

	err := main.ParseEvents(pass, []string{eventPackage}, []string{"TestA"})
	if err != nil {
		t.Fatalf("a passing package with the required test: %v", err)
	}

	if err = main.ParseEvents(pass, []string{eventPackage}, []string{"m/p::TestA"}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		events  string
		require []string
	}{
		"required test missing": {pass, []string{"TestScaleAcceptance"}},
		"no test ran":           {`{"Action":"pass","Package":"m/p"}` + "\n", nil},
		"all skipped": {
			`{"Action":"skip","Package":"m/p","Test":"TestA"}` + "\n" + `{"Action":"pass","Package":"m/p"}` + "\n",
			[]string{"TestA"},
		},
		"failing test": {
			`{"Action":"fail","Package":"m/p","Test":"TestA"}` + "\n" + `{"Action":"fail","Package":"m/p"}` + "\n",
			nil,
		},
		"only final results": {
			`{"Action":"pass","Package":"m/p","Test":"TestA"}` + "\n" + `{"Action":"pass","Package":"m/p"}` + "\n",
			nil,
		},
		"package never reported":   {"", nil},
		"malformed trailing event": {pass + `{ "Action":`, nil},
		"unfinished subtest":       {`{"Action":"run","Package":"m/p","Test":"TestA/sub"}` + "\n" + pass, nil},
	}

	for name, test := range cases {
		err = main.ParseEvents(test.events, []string{eventPackage}, test.require)
		if err == nil {
			t.Fatalf("%s: an empty or failing run was accepted", name)
		}
	}

	err = main.ParseEvents(pass, nil, nil)
	if err == nil {
		t.Fatal("a run with no package that has tests was accepted")
	}
}

func TestOperatorFlagsEnableEveryOperator(t *testing.T) {
	t.Parallel()

	flags := main.MutationOperatorFlags()

	const operators = 11
	if len(flags) != operators || flags[0] != "--arithmetic-base=true" {
		t.Fatalf("flags %v", flags)
	}
}

// fixtureTargets selects nonhost archives from the Darwin and Linux target set, so inspection
// never tries to run the fixture's fake executable.
func fixtureTargets() []string {
	var targets []string

	for _, target := range []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"} {
		if target != runtime.GOOS+"_"+runtime.GOARCH {
			targets = append(targets, target)
		}
	}

	return targets
}

func newArchiveFixture(t *testing.T, tamper func(member string, content []byte) []byte) archiveFixture {
	t.Helper()

	base := t.TempDir()
	root := filepath.Join(base, "repo")
	dist := filepath.Join(base, "dist")

	repoFiles := map[string]string{
		"internal/app/version.txt":       "1.0.0\n",
		licenseName:                      "license",
		"README.md":                      "readme",
		noticesName:                      "notices",
		"third_party/licenses/MIT.txt":   "mit",
		"docs/CLI.md":                    "cli",
		"docs/PLAN.md":                   "plan",
		"docs/BLANK_PAGES.md":            "blanks",
		"internal/plan/plan.schema.json": "{}",
	}

	for name, content := range repoFiles {
		write(t, filepath.Join(root, filepath.FromSlash(name)), []byte(content))
	}

	archiveFiles := map[string]string{
		archiveExecutableName:          "binary",
		licenseName:                    "license",
		"README.md":                    "readme",
		noticesName:                    "notices",
		"third_party/licenses/MIT.txt": "mit",
		"docs/CLI.md":                  "cli",
		"docs/PLAN.md":                 "plan",
		"docs/BLANK_PAGES.md":          "blanks",
		"schemas/plan.schema.json":     "{}",
	}

	var sums strings.Builder

	for _, target := range fixtureTargets() {
		name := "PDFConcat_1.0.0_" + target + ".tar.gz"
		data := tarball(t, archiveFiles, tamper)

		write(t, filepath.Join(dist, name), data)

		sum := sha256.Sum256(data)
		sums.WriteString(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	}

	write(t, filepath.Join(dist, "checksums.txt"), []byte(sums.String()))

	return archiveFixture{root: root, dist: dist}
}

func write(t *testing.T, name string, content []byte) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(name), 0o750)
	if err == nil {
		err = os.WriteFile(name, content, 0o600)
	}

	if err != nil {
		t.Fatal(err)
	}
}

func tarball(t *testing.T, files map[string]string, tamper func(string, []byte) []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer

	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, content := range files {
		data := []byte(content)
		if tamper != nil {
			data = tamper(name, data)
		}

		if data == nil {
			continue
		}

		err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		if err == nil {
			_, err = tarWriter.Write(data)
		}

		if err != nil {
			t.Fatal(err)
		}
	}

	if tarWriter.Close() != nil || gzipWriter.Close() != nil {
		t.Fatal("cannot close the tar fixture")
	}

	return buffer.Bytes()
}

// TestArchiveProblemsRejectsTextExecutables proves foreign target names cannot bless text as binaries.
func TestArchiveProblemsRejectsTextExecutables(t *testing.T) {
	t.Parallel()

	fixture := newArchiveFixture(t, nil)

	problems, err := main.ArchiveInspection(fixture.root, fixture.dist)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "no archive for windows/amd64") || !strings.Contains(joined, "no archive for windows/arm64") {
		t.Fatalf("missing targets not reported: %q", problems)
	}

	const targetsInRelease = 6
	if want := targetsInRelease - len(fixtureTargets()) + 2*len(fixtureTargets()); len(problems) != want {
		t.Fatalf("got %d problems, want exactly the %d missing targets: %q", len(problems), want, problems)
	}
}

// TestArchiveProblemsRejectsTamperedArchives is the negative control for archive inspection.
func TestArchiveProblemsRejectsTamperedArchives(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tamper func(member string, content []byte) []byte
		want   string
	}{
		"license differs": {func(member string, content []byte) []byte {
			if member == licenseName {
				return []byte("other")
			}

			return content
		}, "does not equal the repository's LICENSE"},
		"notices missing": {func(member string, content []byte) []byte {
			if member == noticesName {
				return nil
			}

			return content
		}, "missing THIRD_PARTY_NOTICES.md"},
		"schema differs": {func(member string, content []byte) []byte {
			if member == "schemas/plan.schema.json" {
				return []byte(`{"x":1}`)
			}

			return content
		}, "schemas/plan.schema.json is missing or differs"},
	}

	for name, test := range cases {
		fixture := newArchiveFixture(t, test.tamper)

		problems, err := main.ArchiveInspection(fixture.root, fixture.dist)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(strings.Join(problems, "\n"), test.want) {
			t.Fatalf("%s: problems %q do not mention %q", name, problems, test.want)
		}
	}
}

func TestArchiveProblemsRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	fixture := newArchiveFixture(t, nil)

	archive := filepath.Join(fixture.dist, "PDFConcat_1.0.0_"+fixtureTargets()[0]+".tar.gz")
	if _, err := os.Stat(archive); err != nil {
		t.Fatal(err)
	}

	write(
		t,
		archive,
		tarball(t, map[string]string{archiveExecutableName: "x"}, nil),
	)

	problems, err := main.ArchiveInspection(fixture.root, fixture.dist)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(strings.Join(problems, "\n"), "does not match its checksums.txt digest") {
		t.Fatalf("problems %q", problems)
	}
}

// TestVersionOutputRequiresRealCommitAndDate is the negative control for version metadata: a build
// without VCS information, or without the linker-set version, is rejected.
func TestVersionOutputRequiresRealCommitAndDate(t *testing.T) {
	t.Parallel()

	good := `{"version":"1.2.3","commit":"899db07216b1","date":"2026-10-06T05:09:28Z"}`
	if problems := main.VersionOutputCheck("1.2.3", good); len(problems) != 0 {
		t.Fatalf("unexpected problems: %q", problems)
	}

	cases := map[string]string{
		"version prefix": `{"version":"1.2.3-forged","commit":"899db07216b1","date":"2026-10-06T05:09:28Z"}`,
		"no version":     `{"version":"dev","commit":"899db07216b1","date":"2026-10-06T05:09:28Z"}`,
		"no commit":      `{"version":"1.2.3","commit":"none","date":"2026-10-06T05:09:28Z"}`,
		"no date":        `{"version":"1.2.3","commit":"899db07216b1","date":"unknown"}`,
		"fields gone":    `{"version":"1.2.3"}`,
	}

	for name, output := range cases {
		if len(main.VersionOutputCheck("1.2.3", output)) == 0 {
			t.Fatalf("%s: accepted", name)
		}
	}
}
