// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"testing"
)

const gremlinsArchiveSourceTestVersion = "v0.6.0"

func TestModuleSourceIdentityRejectsEveryChangedPinnedInput(t *testing.T) {
	t.Parallel()

	content := []byte("verified module archive")
	digest := sha256.Sum256(content)
	versions := map[string]string{
		"GREMLINS_VERSION":           gremlinsArchiveSourceTestVersion,
		"GREMLINS_SOURCE_SUM":        "h1:source",
		"GREMLINS_SOURCE_MOD_SUM":    "h1:module",
		"GREMLINS_SOURCE_ZIP_SHA256": hex.EncodeToString(digest[:]),
	}

	source := moduleSource{Path: gremlinsSourceModule, Version: gremlinsArchiveSourceTestVersion, Sum: "h1:source", GoModSum: "h1:module"}
	if err := verifyModuleSource(&source, content, versions, gremlinsSourceModule, gremlinsSourcePrefix); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"GREMLINS_VERSION", "GREMLINS_SOURCE_SUM",
		"GREMLINS_SOURCE_MOD_SUM", "GREMLINS_SOURCE_ZIP_SHA256",
	} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			altered := maps.Clone(versions)

			altered[key] = "wrong"
			if err := verifyModuleSource(&source, content, altered, gremlinsSourceModule, gremlinsSourcePrefix); err == nil {
				t.Fatal("changed source pin accepted")
			}
		})
	}

	wrongSource := source

	wrongSource.Path = "github.com/untrusted/tool"
	if err := verifyModuleSource(&wrongSource, content, versions, gremlinsSourceModule, gremlinsSourcePrefix); err == nil {
		t.Fatal("wrong source module accepted")
	}
}

func TestModuleSourceExtractionPreservesFilesAndRejectsAliases(t *testing.T) {
	t.Parallel()

	prefix := gremlinsSourceModule + "@" + gremlinsArchiveSourceTestVersion + "/"
	positive := sourceArchiveFixture(t, []string{prefix + sourceModuleFile, prefix + "pkg/file.go"})

	dir := t.TempDir()
	if err := extractModuleSource(positive, gremlinsSourceModule, gremlinsArchiveSourceTestVersion, dir); err != nil {
		t.Fatal(err)
	}

	data, readErr := readFile(dir, "pkg/file.go")
	if readErr != nil || string(data) != "source bytes" {
		t.Fatalf("source bytes changed: %s %v", data, readErr)
	}

	for _, names := range [][]string{
		{prefix + "../escape"},
		{"wrong/module.go"},
		{prefix + "same.go", prefix + "same.go"},
		{prefix + "nested\\escape.go"},
		{},
	} {
		archive := sourceArchiveFixture(t, names)
		if err := extractModuleSource(archive, gremlinsSourceModule, gremlinsArchiveSourceTestVersion, t.TempDir()); err == nil {
			t.Fatalf("unsafe/empty source archive accepted: %v", names)
		}
	}
}

func sourceArchiveFixture(t *testing.T, names []string) []byte {
	t.Helper()

	var buffer bytes.Buffer

	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		file, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}

		if _, writeErr := file.Write([]byte("source bytes")); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}
