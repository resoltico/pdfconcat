// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
)

const (
	fixtureTarTypeOffset    = 156
	fixtureTarChecksumStart = 148
	fixtureTarChecksumEnd   = 156
)

func TestForeignFixtureRejectsDanglingMetadataAndSparseExpansion(t *testing.T) {
	t.Parallel()

	for _, metadata := range []map[string]string{
		{fixtureArchiveCommitKey: "dangling terminal metadata"},
		{"GNU.sparse.map": "0,1", "GNU.sparse.size": "67108864", "GNU.sparse.numblocks": "1"},
	} {
		t.Run(fmt.Sprint(metadata), func(t *testing.T) {
			t.Parallel()
			root, source := fixtureArchiveSource(t)
			raw := fixtureDecodedBytes(t, fixtureCompressedBytes(t))
			addition := fixtureExtendedMetadata(t, metadata)
			raw = append(raw[:len(raw)-foreignTarFooterBytes], addition...)
			artifact := filepath.Join(t.TempDir(), fixtureArchiveName)
			writeForeignGraphFixture(t, artifact, string(fixtureGzipBytes(t, raw)))

			if _, err := checkFixtureArchive(t, root, artifact, t.TempDir(), source); err == nil {
				t.Fatal("dangling/sparse verified-pin archive accepted")
			}
		})
	}
}

func fixtureExtendedMetadata(t *testing.T, metadata map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer

	writer := tar.NewWriter(&buffer)

	header := &tar.Header{Name: "extended_metadata", Typeflag: tar.TypeXGlobalHeader, PAXRecords: metadata}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	if _, sparse := metadata["GNU.sparse.map"]; sparse {
		header = &tar.Header{
			Name:     "fixture-" + fixtureTestRevision + "/pkg/testdata/sparse.raw",
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     1,
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}

		if _, err := writer.Write([]byte{'x'}); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	data := buffer.Bytes()
	// Go's public writer does not emit per-file PAX on demand. Change only the
	// standard header type/checksum to exercise the maintained reader's sparse
	// expansion and terminal-metadata paths independently of our validator.
	data[fixtureTarTypeOffset] = tar.TypeXHeader
	for index := fixtureTarChecksumStart; index < fixtureTarChecksumEnd; index++ {
		data[index] = ' '
	}

	checksum := 0
	for _, value := range data[:foreignTarBlockBytes] {
		checksum += int(value)
	}

	copy(data[fixtureTarChecksumStart:fixtureTarChecksumEnd], fmt.Sprintf("%06o\x00 ", checksum))

	return data
}
