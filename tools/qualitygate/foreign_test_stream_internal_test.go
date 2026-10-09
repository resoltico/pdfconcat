// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"path/filepath"
	"testing"
)

func TestForeignFixtureStreamRejectsUnvalidatedFooterAndConcatenation(t *testing.T) {
	t.Parallel()
	valid := fixtureCompressedBytes(t)
	raw := fixtureDecodedBytes(t, valid)
	corrupt := bytes.Clone(valid)
	corrupt[len(corrupt)-8] ^= 1

	cases := []struct {
		name string
		data []byte
	}{
		{name: "missing footer", data: fixtureGzipBytes(t, raw[:len(raw)-foreignTarFooterBytes])},
		{name: "one footer block", data: fixtureGzipBytes(t, raw[:len(raw)-foreignTarBlockBytes])},
		{name: "truncated last member", data: fixtureGzipBytes(t, raw[:len(raw)-foreignTarFooterBytes-foreignTarBlockBytes])},
		{name: "truncated member padding", data: fixtureGzipBytes(t, raw[:len(raw)-foreignTarFooterBytes-1])},
		{name: "nonzero tail", data: fixtureGzipBytes(t, append(bytes.Clone(raw), 1))},
		{name: "extra empty gzip", data: append(bytes.Clone(valid), fixtureGzipBytes(t, nil)...)},
		{name: "extra content gzip", data: append(bytes.Clone(valid), fixtureGzipBytes(t, []byte("program injection"))...)},
		{name: "CRC failure", data: corrupt},
		{name: "truncated trailer", data: valid[:len(valid)-1]},
		{name: "raw compressed tail", data: append(bytes.Clone(valid), 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, source := fixtureArchiveSource(t)
			artifact := filepath.Join(t.TempDir(), fixtureArchiveName)
			writeForeignGraphFixture(t, artifact, string(test.data))

			destination := t.TempDir()
			if _, err := checkFixtureArchive(t, root, artifact, destination, source); err == nil {
				t.Fatal("malformed verified-pin archive accepted")
			}
			// A valid candidate precedes these late failures. Quarantine must never
			// be mistaken for a sealed stage, even after files have been written.
			if _, err := readInRoot(destination, fixtureCandidateData); err != nil {
				t.Fatal("late-failure control did not reach candidate output")
			}
		})
	}
}

func TestForeignFixtureStreamAcceptsBoundedAuthenticZeroPadding(t *testing.T) {
	t.Parallel()
	root, source := fixtureArchiveSource(t)
	raw := fixtureDecodedBytes(t, fixtureCompressedBytes(t))
	artifact := filepath.Join(t.TempDir(), fixtureArchiveName)
	writeForeignGraphFixture(t, artifact, string(fixtureGzipBytes(t, append(raw, make([]byte, foreignTarBlockBytes)...))))

	if _, err := checkFixtureArchive(t, root, artifact, t.TempDir(), source); err != nil {
		t.Fatal(err)
	}
}

func fixtureCompressedBytes(t *testing.T) []byte {
	t.Helper()
	name := writeFixtureTar(t, authenticFixtureMembers(), fixtureTestRevision)

	data, err := readInRoot(filepath.Dir(name), filepath.Base(name))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func fixtureDecodedBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}

	return decoded
}

func fixtureGzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer

	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func TestForeignZeroMemberDataCannotMasqueradeAsFooter(t *testing.T) {
	t.Parallel()
	root, source := fixtureArchiveSource(t)
	members := authenticFixtureMembers()
	members = append(
		members,
		fixtureTarMember{name: "pkg/testdata/zero.raw", body: string(make([]byte, foreignTarFooterBytes)), kind: tar.TypeReg},
	)
	name := writeFixtureTar(t, members, fixtureTestRevision)

	data, err := readInRoot(filepath.Dir(name), filepath.Base(name))
	if err != nil {
		t.Fatal(err)
	}

	raw := fixtureDecodedBytes(t, data)
	artifact := filepath.Join(t.TempDir(), fixtureArchiveName)
	writeForeignGraphFixture(t, artifact, string(fixtureGzipBytes(t, raw[:len(raw)-foreignTarFooterBytes])))

	if _, archiveErr := checkFixtureArchive(t, root, artifact, t.TempDir(), source); archiveErr == nil {
		t.Fatal("zero member data mistaken for missing footer")
	}
}
