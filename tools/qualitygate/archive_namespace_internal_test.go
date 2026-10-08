// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"runtime"
	"strings"
	"testing"
)

type archiveNamespaceEntry struct {
	name      string
	directory bool
}

const (
	namespaceDocs           = "docs"
	namespaceCLI            = "docs/CLI.md"
	namespaceZip            = "zip"
	namespaceRegularContent = "regular bytes"
)

func TestArchiveMemberNamespacesRejectFileDirectoryCollisionsInEitherOrder(t *testing.T) {
	t.Parallel()

	executable := executableName(runtime.GOOS)

	fixtures := map[string][]archiveNamespaceEntry{
		"case-aliased-directory":         {{name: strings.ToUpper(executable) + "/", directory: true}, {name: executable}},
		"case-aliased-license-directory": {{name: "license/", directory: true}, {name: "LICENSE"}},
		"unnecessary-empty-directory":    {{name: "unnecessary/", directory: true}, {name: executable}},
		"directory-before-file":          {{name: executable + "/", directory: true}, {name: executable}},
		"file-before-directory":          {{name: executable}, {name: executable + "/", directory: true}},
		"file-before-child":              {{name: namespaceDocs}, {name: namespaceCLI}},
		"child-before-file":              {{name: namespaceCLI}, {name: namespaceDocs}},
		"file-before-nested-directory":   {{name: namespaceDocs}, {name: "docs/nested/", directory: true}},
		"nested-directory-before-file":   {{name: "docs/nested/", directory: true}, {name: namespaceDocs}},
	}
	for name, entries := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, format := range []string{"tar", namespaceZip} {
				if err := readNamespaceFixture(t, format, entries); err == nil {
					t.Fatalf("%s accepted unextractable namespace", format)
				}
			}
		})
	}

	valid := []archiveNamespaceEntry{
		{name: "docs/", directory: true},
		{name: "docs/", directory: true},
		{name: namespaceCLI},
		{name: executable},
	}
	for _, format := range []string{"tar", namespaceZip} {
		if err := readNamespaceFixture(t, format, valid); err != nil {
			t.Fatalf("%s rejected valid members: %v", format, err)
		}
	}
}

func readNamespaceFixture(t *testing.T, format string, entries []archiveNamespaceEntry) error {
	t.Helper()

	if format == namespaceZip {
		return readNamespaceZipFixture(t, entries)
	}

	return readNamespaceTarFixture(t, entries)
}

func readNamespaceZipFixture(t *testing.T, entries []archiveNamespaceEntry) error {
	t.Helper()

	var buffer bytes.Buffer

	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		handle, createErr := writer.Create(entry.name)
		if createErr != nil {
			t.Fatal(createErr)
		}

		if !entry.directory {
			if _, writeErr := handle.Write([]byte(namespaceRegularContent)); writeErr != nil {
				t.Fatal(writeErr)
			}
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := readZip("fixture.zip", buffer.Bytes())

	return err
}

func readNamespaceTarFixture(t *testing.T, entries []archiveNamespaceEntry) error {
	t.Helper()

	var buffer bytes.Buffer

	gzipWriter := gzip.NewWriter(&buffer)

	writer := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		writeNamespaceTarEntry(t, writer, entry)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := readTarGz("fixture.tar.gz", buffer.Bytes())

	return err
}

func writeNamespaceTarEntry(t *testing.T, writer *tar.Writer, entry archiveNamespaceEntry) {
	t.Helper()

	data := []byte(namespaceRegularContent)

	header := &tar.Header{Name: entry.name, Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(len(data))}
	if entry.directory {
		header.Typeflag = tar.TypeDir
		header.Mode = 0o700
		header.Size = 0
	}

	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	if !entry.directory {
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
}
