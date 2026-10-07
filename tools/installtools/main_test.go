// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	main "github.com/resoltico/pdfconcat/tools/installtools"
)

const toolArchiveName = "tool.tar.gz"

func digestOf(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

func TestVerifyChecksumAcceptsMatchingDigest(t *testing.T) {
	t.Parallel()

	content := []byte("archive bytes")
	sums := "deadbeef  other.tar.gz\n" + digestOf(content) + "  tool.tar.gz\n"

	err := main.ChecksumCheck(content, sums, toolArchiveName)
	if err != nil {
		t.Fatal(err)
	}
}

// TestVerifyChecksumRejects is the negative control: a tampered archive, an unlisted file and a
// malformed manifest all fail.
func TestVerifyChecksumRejects(t *testing.T) {
	t.Parallel()

	content := []byte("archive bytes")
	good := digestOf(content) + "  tool.tar.gz\n"

	cases := map[string]struct {
		sums    string
		want    string
		content []byte
	}{
		"tampered":   {good, "release checksums say", []byte("tampered")},
		"unlisted":   {good, "do not list other.tar.gz", content},
		"empty list": {"", "do not list", content},
	}

	for name, test := range cases {
		asset := toolArchiveName
		if name == "unlisted" {
			asset = "other.tar.gz"
		}

		err := main.ChecksumCheck(test.content, test.sums, asset)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: got %v, want an error mentioning %q", name, err, test.want)
		}
	}
}

func TestExtractMemberFromTarAndZip(t *testing.T) {
	t.Parallel()

	var tarball bytes.Buffer

	gzipWriter := gzip.NewWriter(&tarball)
	tarWriter := tar.NewWriter(gzipWriter)

	body := []byte("binary")

	err := tarWriter.WriteHeader(&tar.Header{Name: "dir/tool", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	if err == nil {
		_, err = tarWriter.Write(body)
	}

	if err != nil || tarWriter.Close() != nil || gzipWriter.Close() != nil {
		t.Fatal("cannot build the tar fixture", err)
	}

	got, err := main.MemberOf(tarball.Bytes(), toolArchiveName, "dir/tool")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("tar: got %q, %v", got, err)
	}

	_, err = main.MemberOf(tarball.Bytes(), toolArchiveName, "dir/missing")
	if err == nil {
		t.Fatal("a missing member was accepted")
	}
}

func TestExtractMemberFromZip(t *testing.T) {
	t.Parallel()

	body := []byte("binary")

	var archive bytes.Buffer

	zipWriter := zip.NewWriter(&archive)

	file, err := zipWriter.Create("tool.exe")
	if err == nil {
		_, err = file.Write(body)
	}

	if err != nil || zipWriter.Close() != nil {
		t.Fatal("cannot build the zip fixture", err)
	}

	got, err := main.MemberOf(archive.Bytes(), "tool.zip", "tool.exe")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("zip: got %q, %v", got, err)
	}
}

func TestSelectToolsRejectsUnknownName(t *testing.T) {
	t.Parallel()

	all, err := main.ToolSelection(nil)
	if err != nil || !all["golangci-lint"] || !all["gremlins"] {
		t.Fatalf("default selection %v, %v", all, err)
	}

	_, err = main.ToolSelection([]string{"golangci-lint", "nonesuch"})
	if err == nil {
		t.Fatal("unknown tool accepted")
	}
}
