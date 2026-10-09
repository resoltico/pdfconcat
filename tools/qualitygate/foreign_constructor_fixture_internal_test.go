// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	fixtureCommitKey           = "comment"
	fixtureSamplesModule       = "pkg/samples/go.mod"
	fixtureTestdataModule      = "pkg/testdata/go.mod"
	fixtureCandidateInput      = "pkg/testdata/input.raw"
	constructorChecksumFailure = "CRC"
	constructorLateCommit      = "late commit"
	constructorModule          = "github.com/benoitkugler/pdf"
	constructorRoot            = "third_party/benoitkugler/pdf"
	constructorVersion         = "v0.0.15"
	constructorReader          = "reader/value.go"
	constructorPatch           = "diff --git a/reader/value.go b/reader/value.go\n" +
		"--- a/reader/value.go\n+++ b/reader/value.go\n@@ -1,2 +1,2 @@\n" +
		" package reader\n-func Value() int { return 1 }\n+func Value() int { return 2 }\n"
)

func constructorSourceFiles() map[string]string {
	return map[string]string{
		moduleFileName: "module github.com/benoitkugler/pdf\n", "go.sum": "", "LICENSE": "upstream license\n",
		constructorReader: "package reader\nfunc Value() int { return 1 }\n", "README.md": "upstream readme\n",
		"testdata/asset.bin": string([]byte{0, 255, 10}), ".upstream": "hidden upstream asset\n",
	}
}

func constructorModuleArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer

	archive := zip.NewWriter(&buffer)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		header := &zip.FileHeader{Name: constructorModule + "@" + constructorVersion + "/" + name}
		header.SetMode(0o600)

		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = writer.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func constructorSourceFixture(t *testing.T) (string, repopolicy.ForeignSource) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	files := constructorSourceFiles()
	archive := constructorModuleArchive(t, files)
	source := repopolicy.ForeignSource{
		Root: constructorRoot, Module: constructorModule, Version: constructorVersion, Repository: "https://" + constructorModule,
		Revision: fixtureTestRevision, SourceArtifact: "third_party/benoitkugler/base/pdf-v0.0.15.zip",
		SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(archive)), Patch: "third_party/benoitkugler/patches/pdf-content-limits.patch",
		PatchSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(constructorPatch))), License: constructorRoot + "/LICENSE",
		// Independently computed from these seven upstream file bytes, not the verifier under test.
		ModuleSum: "h1:xvv0XfK/WPoOw0zeZHjTcE6rpa0n/oz9aGRAwaOHCi4=", GoModSum: "h1:c5tfRqVRmHF/S8G6dPCLTySu6QNB9+zoqWYySr5cavI=",
		Completeness: "Go module ZIP plus exact local patch; not a full Git tree claim",
	}
	writeForeignGraphFixture(t, filepath.Join(root, filepath.FromSlash(source.SourceArtifact)), string(archive))
	writeForeignGraphFixture(t, filepath.Join(root, filepath.FromSlash(source.Patch)), constructorPatch)

	for name, data := range files {
		if name == constructorReader {
			data = "package reader\nfunc Value() int { return 2 }\n"
		}

		writeForeignGraphFixture(t, filepath.Join(root, filepath.FromSlash(source.Root), filepath.FromSlash(name)), data)
	}

	writeForeignGraphFixture(
		t,
		filepath.Join(root, moduleFileName),
		"module example.test/constructor\ngo 1.27.1\nrequire "+constructorModule+" "+constructorVersion+"\n"+
			"replace "+constructorModule+" => ./"+constructorRoot+"\n",
	)
	writeForeignGraphFixture(t, filepath.Join(root, foreignModuleSums), "")
	writeConstructorRecord(t, root, source)

	return root, source
}

func writeConstructorRecord(t *testing.T, root string, source repopolicy.ForeignSource) {
	t.Helper()

	data, err := json.Marshal([]repopolicy.ForeignSource{source})
	if err != nil {
		t.Fatal(err)
	}

	writeForeignGraphFixture(t, filepath.Join(root, repopolicy.ForeignSourceRecord), string(data))
}

func seedConstructorArtifact(t *testing.T, data []byte) (string, string) {
	t.Helper()

	pin := fmt.Sprintf("%x", sha256.Sum256(data))

	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}

	cache = filepath.Join(cache, "pdfconcat", "foreign-test-inputs")
	if err = os.MkdirAll(cache, foreignStageDirectoryMode); err != nil {
		t.Fatal(err)
	}

	name := filepath.Clean(filepath.Join(cache, pin+".tar.gz"))

	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if removeErr := os.Remove(name); removeErr != nil {
			t.Error(removeErr)
		}
	})

	return name, pin
}
