// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	foreignFixtureRoot      = "third_party/benoitkugler/pdf"
	foreignFixtureModule    = "github.com/benoitkugler/pdf"
	foreignFixtureVersion   = "v0.0.15"
	foreignFixtureReader    = "reader/value.go"
	foreignFixtureZipRole   = "ZIP"
	foreignFixturePatchRole = "patch"
	foreignFixturePatch     = "diff --git a/reader/value.go b/reader/value.go\n" +
		"--- a/reader/value.go\n+++ b/reader/value.go\n@@ -1,2 +1,2 @@\n" +
		" package reader\n-func Value() int { return 1 }\n+func Value() int { return 2 }\n"
)

func tinyForeignFiles() map[string][]byte {
	return map[string][]byte{
		goModuleFile:         []byte("module github.com/benoitkugler/pdf\n"),
		"go.sum":             {},
		foreignLicenseFile:   []byte("upstream license\n"),
		foreignFixtureReader: []byte("package reader\nfunc Value() int { return 1 }\n"),
		"README.md":          []byte("upstream readme\n"),
		"testdata/asset.bin": {0, 255, 10},
		".upstream":          []byte("hidden upstream asset\n"),
	}
}

func tinyForeignSource(t *testing.T) (string, ForeignSource) {
	t.Helper()
	root := t.TempDir()
	files := tinyForeignFiles()
	archive := tinyForeignArchive(t, files)
	source := ForeignSource{
		Root:           foreignFixtureRoot,
		Module:         foreignFixtureModule,
		Version:        foreignFixtureVersion,
		Repository:     "https://" + foreignFixtureModule,
		Revision:       "0000000000000000000000000000000000000000",
		SourceArtifact: "third_party/benoitkugler/base/pdf-v0.0.15.zip",
		SourceSHA256:   fmt.Sprintf("%x", sha256.Sum256(archive)),
		// Independently computed from the specified seven file bytes, not the checker under test.
		ModuleSum:    "h1:xvv0XfK/WPoOw0zeZHjTcE6rpa0n/oz9aGRAwaOHCi4=",
		GoModSum:     "h1:c5tfRqVRmHF/S8G6dPCLTySu6QNB9+zoqWYySr5cavI=",
		Patch:        "third_party/benoitkugler/patches/pdf-content-limits.patch",
		PatchSHA256:  fmt.Sprintf("%x", sha256.Sum256([]byte(foreignFixturePatch))),
		License:      foreignFixtureRoot + "/LICENSE",
		Completeness: "Go module ZIP plus exact local patch; not a full Git tree claim",
	}
	writeForeignTestFile(t, root, source.SourceArtifact, archive)
	writeForeignTestFile(t, root, source.Patch, []byte(foreignFixturePatch))

	for name, data := range files {
		if name == foreignFixtureReader {
			data = bytes.Replace(data, []byte("return 1"), []byte("return 2"), 1)
		}

		writeForeignTestFile(t, root, source.Root+"/"+name, data)
	}

	writeForeignTestRecord(t, root, []ForeignSource{source})

	return root, source
}

func tinyForeignArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buffer bytes.Buffer

	archive := zip.NewWriter(&buffer)

	for _, name := range slices.Sorted(maps.Keys(files)) {
		header := &zip.FileHeader{Name: foreignFixtureModule + "@" + foreignFixtureVersion + "/" + name}
		header.SetMode(0o600)

		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = writer.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func writeForeignTestFile(t *testing.T, root, name string, data []byte) {
	t.Helper()

	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeForeignTestRecord(t *testing.T, root string, sources []ForeignSource) {
	t.Helper()

	data, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}

	writeForeignTestFile(t, root, ForeignSourceRecord, data)
}

func TestForeignSourceReconstructsCompleteTreeBeforeExactOwnershipBoundary(t *testing.T) {
	t.Parallel()
	root, source := tinyForeignSource(t)
	writeForeignTestFile(t, root, "application.go", []byte("package application\n"))
	writeForeignTestFile(t, root, "third_party/benoitkugler/pdfSibling/source.go", []byte("package sibling\n"))
	writeForeignTestFile(t, root, "third_party/vendor/source.go", []byte("package vendor\n"))

	sources, err := ForeignSourcesContext(t.Context(), root)
	if err != nil || len(sources) != 1 || sources[0] != source {
		t.Fatalf("valid immutable ZIP plus patch rejected: %v %v", sources, err)
	}

	owned, err := OwnedGoSources(t.Context(), root)
	if err != nil ||
		!slices.Equal(owned, []string{"application.go", "third_party/benoitkugler/pdfSibling/source.go", "third_party/vendor/source.go"}) {
		t.Fatalf("exact foreign boundary masked sibling/application source: %v %v", owned, err)
	}
	// A warm reconstruction cache cannot authorize source bytes observed on an earlier scan.
	writeForeignTestFile(t, root, source.Root+"/reader/value.go", []byte("package reader\nfunc Value() int { return 3 }\n"))

	if _, err = OwnedGoSources(t.Context(), root); err == nil {
		t.Fatal("changed source survived cached expected reconstruction")
	}
}

func TestForeignSourceRejectsChangedMissingAndAdditionalInputs(t *testing.T) {
	t.Parallel()

	for _, name := range []string{foreignFixtureReader, "testdata/asset.bin", foreignLicenseFile, ".upstream", "README.md"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root, source := tinyForeignSource(t)
			if err := os.Remove(filepath.Join(root, source.Root, name)); err != nil {
				t.Fatal(err)
			}

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
				t.Fatalf("omitted source input %s was accepted", name)
			}
		})
	}

	for _, name := range []string{".hidden.go", ".hidden.dat", "extra.go", "reader/extra.asset"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, source := tinyForeignSource(t)
			writeForeignTestFile(t, root, source.Root+"/"+name, []byte("additional input"))

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
				t.Fatalf("additional source input %s was accepted", name)
			}
		})
	}
}

func TestForeignSourceRejectsBroaderRootsAndIncompleteRecords(t *testing.T) {
	t.Parallel()

	for _, declaration := range []string{
		"", "/absolute", "../escape", "third_party", "third_party/benoitkugler", "third_party/benoitkugler/pdfSibling",
	} {
		t.Run(declaration, func(t *testing.T) {
			t.Parallel()
			root, source := tinyForeignSource(t)
			source.Root = declaration
			writeForeignTestRecord(t, root, []ForeignSource{source})

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
				t.Fatalf("foreign declaration widened ownership to %q", declaration)
			}
		})
	}

	root, source := tinyForeignSource(t)
	writeForeignTestRecord(t, root, []ForeignSource{source, source})

	if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
		t.Fatal("duplicate foreign declaration passed")
	}

	if err := os.Remove(filepath.Join(root, ForeignSourceRecord)); err != nil {
		t.Fatal(err)
	}

	if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
		t.Fatal("foreign tree without its declaration passed")
	}

	if sources, err := ForeignSourcesContext(t.Context(), t.TempDir()); err != nil || len(sources) != 0 {
		t.Fatalf("ordinary synthetic root acquired a foreign-source requirement: %v %v", sources, err)
	}
}

func TestForeignSourceRejectsPinChangesAndUnapplicablePatches(t *testing.T) {
	t.Parallel()

	for _, field := range []string{foreignFixtureZipRole, foreignFixturePatchRole, "module h1", "go.mod h1", "context"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			root, source := tinyForeignSource(t)

			switch field {
			case foreignFixtureZipRole:
				source.SourceSHA256 = strings.Repeat("0", 64)
			case foreignFixturePatchRole:
				source.PatchSHA256 = strings.Repeat("0", 64)
			case "module h1":
				source.ModuleSum = "h1:wrong"
			case "go.mod h1":
				source.GoModSum = "h1:wrong"
			case "context":
				patch := bytes.ReplaceAll([]byte(foreignFixturePatch), []byte("package reader"), []byte("package absent"))
				writeForeignTestFile(t, root, source.Patch, patch)
				source.PatchSHA256 = fmt.Sprintf("%x", sha256.Sum256(patch))
			default:
				t.Fatal("unknown pin control")
			}

			writeForeignTestRecord(t, root, []ForeignSource{source})

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
				t.Fatalf("changed %s identity/context was accepted", field)
			}
		})
	}
}

func TestForeignSourceRejectsMalformedArchivesAndProtectedInputPatches(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"../outside", "/absolute", "reader/../outside", "new\nline", "C:/outside"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, source := tinyForeignSource(t)
			files := tinyForeignFiles()
			files[name] = []byte("unsafe archive member")
			archive := tinyForeignArchive(t, files)
			writeForeignTestFile(t, root, source.SourceArtifact, archive)
			source.SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(archive))
			writeForeignTestRecord(t, root, []ForeignSource{source})

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
				t.Fatalf("malformed archive path %q was admitted", name)
			}
		})
	}

	for _, name := range []string{goModuleFile, foreignLicenseFile} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, source := tinyForeignSource(t)
			old := strings.TrimSpace(string(tinyForeignFiles()[name]))
			patch := fmt.Sprintf(
				"diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1 +1 @@\n-%s\n+changed upstream input\n",
				name,
				name,
				name,
				name,
				old,
			)
			writeForeignTestFile(t, root, source.Patch, []byte(patch))
			source.PatchSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(patch)))
			writeForeignTestRecord(t, root, []ForeignSource{source})

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil || !strings.Contains(err.Error(), "protected upstream") {
				t.Fatalf("patch changed preserved upstream %s: %v", name, err)
			}
		})
	}
}

func TestForeignSourceRealTreeMatchesCompletePinnedArchiveAndPatch(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	started := time.Now()

	sources, err := ForeignSourcesContext(t.Context(), root)
	if err != nil || len(sources) != 3 {
		t.Fatalf("actual foreign trees do not match all three pinned immutable archives and patches: %v %v", sources, err)
	}

	first := time.Since(started)
	started = time.Now()

	if _, err = ForeignSourcesContext(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"complete actual-source identity scans: first=%s repeat=%s (same-process reconstruction reuse; source bytes rechecked)",
		first,
		time.Since(started),
	)
}
