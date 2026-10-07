// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"runtime"
	"strings"
	"testing"
)

const (
	duplicateFixture = "duplicate"
)

func TestArchiveHeadersRejectRealBinaryRenamedForWrongTarget(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}

	if !binaryHeaderMatches(runtime.GOOS, runtime.GOARCH, data) {
		t.Fatal("native real executable header rejected")
	}

	otherArch := amd64Arch
	if runtime.GOARCH == amd64Arch {
		otherArch = arm64Arch
	}

	if binaryHeaderMatches(runtime.GOOS, otherArch, data) {
		t.Fatal("real executable renamed for wrong architecture accepted")
	}

	for _, goos := range []string{"linux", "darwin", windowsOS} {
		if goos != runtime.GOOS && binaryHeaderMatches(goos, runtime.GOARCH, data) {
			t.Fatalf("native binary renamed for %s accepted", goos)
		}
	}
}

func TestArchiveMembersRejectDuplicateTraversalAndLinks(t *testing.T) {
	t.Parallel()

	for _, member := range []string{duplicateFixture, "../escape", "/absolute", "a/../alias", "back\\slash", "link"} {
		t.Run(member, func(t *testing.T) {
			t.Parallel()

			data := unsafeTarFixture(t, member)
			if _, err := readTarGz("fixture.tar.gz", data); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func unsafeTarFixture(t *testing.T, member string) []byte {
	t.Helper()

	var buffer bytes.Buffer

	gzipWriter := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gzipWriter)

	second := &tar.Header{Name: member, Mode: fileMode, Typeflag: tar.TypeReg}
	if member == duplicateFixture {
		second.Name = archiveExecutableName
	}

	if member == "link" {
		second.Typeflag = tar.TypeSymlink
		second.Linkname = "../escape"
	}

	headers := []*tar.Header{{Name: archiveExecutableName, Mode: execMode, Typeflag: tar.TypeReg}, second}
	for _, header := range headers {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func TestChecksumManifestRejectsDuplicateAndMalformedDigests(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64) + "  PDFConcat_1.0.0_linux_amd64.tar.gz\n"
	for _, data := range []string{digest + digest, "not-a-digest  archive.tar.gz\n", strings.Repeat("a", 64) + "  ../escape\n"} {
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/checksums.txt", []byte(data), fileMode); err != nil {
			t.Fatal(err)
		}

		if _, err := readChecksums(dir); err == nil {
			t.Fatal("invalid checksum manifest accepted")
		}
	}
}
