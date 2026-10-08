// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

// zeroArchivePadding streams highly compressed padding without allocating its decoded size.
type zeroArchivePadding struct{}

const gzipTrailerBytes = 8

func TestArchiveRejectsCorruptCompressedFooterAndTrailingData(t *testing.T) {
	t.Parallel()

	original := compressionFixture(t, nil)
	if _, err := readTarGz("valid.tar.gz", original); err != nil {
		t.Fatal(err)
	}

	corrupt := bytes.Clone(original)

	corrupt[len(corrupt)-gzipTrailerBytes] ^= 1

	invalidArchives := [][]byte{
		corrupt, append(bytes.Clone(original), 1), append(bytes.Clone(original), original...),
		compressionFixture(t, []byte("another decoded archive")),
	}
	for _, data := range invalidArchives {
		if _, err := readTarGz("corrupt.tar.gz", data); err == nil {
			t.Fatal("invalid compression envelope accepted")
		}
	}
}

func compressionFixture(t *testing.T, tail []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)

	archive := tar.NewWriter(writer)
	if err := archive.WriteHeader(&tar.Header{Name: archiveExecutableName, Mode: 0o700, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Write(tail); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return compressed.Bytes()
}

func (zeroArchivePadding) Read(buffer []byte) (int, error) { clear(buffer); return len(buffer), nil }

func TestArchiveDecodedBudgetRejectsCompressedPaddingBomb(t *testing.T) {
	t.Parallel()

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)

	archive := tar.NewWriter(writer)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := io.Copy(writer, io.LimitReader(zeroArchivePadding{}, maxArchiveDecoded+1)); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := readTarGz("padding-bomb.tar.gz", compressed.Bytes()); err == nil || !strings.Contains(err.Error(), "aggregate decoded") {
		t.Fatalf("decoded padding budget not enforced: %v", err)
	}
}
