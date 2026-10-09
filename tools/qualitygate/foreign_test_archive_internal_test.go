// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type fixtureTarMember struct {
	name string
	body string
	kind byte
}

const (
	fixtureTestRevision     = "0123456789abcdef0123456789abcdef01234567"
	fixtureSourceZIP        = "source.zip"
	fixtureArchiveName      = "fixture.tar.gz"
	fixtureModuleVersion    = "v1.0.0"
	fixtureSampleMarker     = "pkg/samples/go.mod"
	fixtureTestdataMarker   = "pkg/testdata/go.mod"
	fixtureCandidateData    = "pkg/testdata/input.raw"
	fixtureArchiveCommitKey = "comment"
	fixtureGlobalPAXName    = "pax_global_header"
)

func TestForeignFixtureArchiveRejectsUnsafeAndInjectedInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		revision string
		extra    fixtureTarMember
	}{
		{name: "traversal", extra: fixtureTarMember{name: "../escaped", body: "x", kind: tar.TypeReg}},
		{name: "absolute residual", extra: fixtureTarMember{name: "/escaped", body: "x", kind: tar.TypeReg}},
		{name: "symlink", extra: fixtureTarMember{name: "pkg/samples/link", kind: tar.TypeSymlink}},
		{name: "program injection", extra: fixtureTarMember{name: "pkg/samples/injected.go", body: "package injected", kind: tar.TypeReg}},
		{
			name:  "native source injection",
			extra: fixtureTarMember{name: "pkg/testdata/injected.c", body: "void injected(){}", kind: tar.TypeReg},
		},
		{name: "source overwrite", extra: fixtureTarMember{name: moduleFileName, body: "changed", kind: tar.TypeReg}},
		{name: "case collision", extra: fixtureTarMember{name: "PKG/SAMPLES/GO.MOD", body: "changed", kind: tar.TypeReg}},
		{name: "file ancestor", extra: fixtureTarMember{name: "pkg/samples", body: "parent-file", kind: tar.TypeReg}},
		{
			name:  "late extra nested marker",
			extra: fixtureTarMember{name: "pkg/samples/nested/go.mod", body: "module extra\n", kind: tar.TypeReg},
		},
		{name: "wrong commit", revision: "bad-revision"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, source := fixtureArchiveSource(t)

			members := authenticFixtureMembers()
			if test.extra.name != "" {
				members = append(members, test.extra)
			}

			revision := fixtureTestRevision
			if test.revision != "" {
				revision = test.revision
			}

			artifact := writeFixtureTar(t, members, revision)
			if _, err := checkFixtureArchive(t, root, artifact, t.TempDir(), source); err == nil {
				t.Fatal("unsafe/injected archive accepted")
			}
		})
	}
}

func TestForeignFixtureArchiveExtractsCompleteDomainsAndDetectsArtifactChanges(t *testing.T) {
	t.Parallel()
	root, source := fixtureArchiveSource(t)
	artifact := writeFixtureTar(t, authenticFixtureMembers(), fixtureTestRevision)

	destination := t.TempDir()

	fixtures, err := checkFixtureArchive(t, root, artifact, destination, source)
	if err != nil {
		t.Fatal(err)
	}

	if len(fixtures) != 3 {
		t.Fatalf("fixture set size %d", len(fixtures))
	}

	if _, err = checkFixtureArchive(t, root, artifact, destination, source); err == nil {
		t.Fatal("overwriting extraction accepted")
	}

	data, err := readInRoot(filepath.Dir(artifact), filepath.Base(artifact))
	if err != nil {
		t.Fatal(err)
	}

	pin := fmt.Sprintf("%x", sha256.Sum256(data))
	if err = verifyForeignTestArtifact(artifact, pin); err != nil {
		t.Fatal(err)
	}

	data[len(data)-1] ^= 1
	writeForeignGraphFixture(t, artifact, string(data))

	if err = verifyForeignTestArtifact(artifact, pin); err == nil {
		t.Fatal("same-size artifact change accepted")
	}
}

func fixtureArchiveSource(t *testing.T) (string, repopolicy.ForeignSource) {
	t.Helper()
	root := t.TempDir()

	tree, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(tree)

	file, err := tree.OpenFile(fixtureSourceZIP, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		t.Fatal(err)
	}

	archive := zip.NewWriter(file)

	member, err := archive.Create("example.test/fixture@v1.0.0/go.mod")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = member.Write([]byte("module example.test/fixture\n")); err != nil {
		t.Fatal(err)
	}

	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	return root, repopolicy.ForeignSource{
		Module:         "example.test/fixture",
		Version:        fixtureModuleVersion,
		Repository:     "https://github.com/example/fixture",
		Revision:       fixtureTestRevision,
		SourceArtifact: fixtureSourceZIP,
	}
}

func authenticFixtureMembers() []fixtureTarMember {
	return []fixtureTarMember{
		{name: moduleFileName, body: "module example.test/fixture\n", kind: tar.TypeReg},
		{name: fixtureSampleMarker, body: "module example.test/fixture/pkg/samples\n", kind: tar.TypeReg},
		{name: fixtureTestdataMarker, body: "module example.test/fixture/pkg/testdata\n", kind: tar.TypeReg},
		{name: fixtureCandidateData, body: "authentic small binary data", kind: tar.TypeReg},
	}
}

func writeFixtureTar(t *testing.T, members []fixtureTarMember, revision string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), fixtureArchiveName)

	tree, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(tree)

	file, err := tree.OpenFile(filepath.Base(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		t.Fatal(err)
	}

	compressed := gzip.NewWriter(file)

	archive := tar.NewWriter(compressed)

	commitHeader := &tar.Header{
		Name:       fixtureGlobalPAXName,
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{fixtureArchiveCommitKey: revision},
	}
	if err = archive.WriteHeader(commitHeader); err != nil {
		t.Fatal(err)
	}

	for _, member := range members {
		writeFixtureTarMember(t, archive, member)
	}

	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}

	if err = compressed.Close(); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	return name
}

func writeFixtureTarMember(t *testing.T, archive *tar.Writer, member fixtureTarMember) {
	t.Helper()

	header := &tar.Header{
		Name:     "fixture-" + fixtureTestRevision + "/" + member.name,
		Mode:     0o644,
		Typeflag: member.kind,
		Size:     int64(len(member.body)),
	}
	if member.kind != tar.TypeReg {
		header.Size = 0
		header.Linkname = "escaped"
	}

	if err := archive.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	if member.kind == tar.TypeReg {
		if _, err := archive.Write([]byte(member.body)); err != nil {
			t.Fatal(err)
		}
	}
}

func checkFixtureArchive(
	t *testing.T,
	root, artifact, destination string,
	source repopolicy.ForeignSource,
) (map[string]foreignFixture, error) {
	t.Helper()

	data, err := readInRoot(filepath.Dir(artifact), filepath.Base(artifact))
	if err != nil {
		t.Fatal(err)
	}

	source.TestInputs.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))

	return stageForeignFixtures(t.Context(), root, artifact, destination, source)
}
