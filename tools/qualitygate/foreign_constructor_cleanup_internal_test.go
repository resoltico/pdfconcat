// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func constructorGitArchive(t *testing.T, trailingMetadata *tar.Header) []byte {
	t.Helper()

	var data bytes.Buffer

	compressed := gzip.NewWriter(&data)
	archive := tar.NewWriter(compressed)

	commit := &tar.Header{
		Name:       fixtureGlobalPAXName,
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{fixtureCommitKey: fixtureTestRevision},
	}
	if err := archive.WriteHeader(commit); err != nil {
		t.Fatal(err)
	}

	for name, body := range constructorSourceFiles() {
		writeConstructorTarMember(t, archive, fixtureTarMember{name: name, body: body, kind: tar.TypeReg})
	}

	for _, member := range []fixtureTarMember{
		{name: fixtureSamplesModule, body: "module github.com/benoitkugler/pdf/pkg/samples\n", kind: tar.TypeReg},
		{name: fixtureTestdataModule, body: "module github.com/benoitkugler/pdf/pkg/testdata\n", kind: tar.TypeReg},
		{name: fixtureCandidateInput, body: "candidate-written-before-footer-" + filepath.Base(t.TempDir()), kind: tar.TypeReg},
	} {
		writeConstructorTarMember(t, archive, member)
	}

	if trailingMetadata != nil {
		if err := archive.WriteHeader(trailingMetadata); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}

	return data.Bytes()
}

func writeConstructorTarMember(t *testing.T, archive *tar.Writer, member fixtureTarMember) {
	t.Helper()

	header := &tar.Header{
		Name:     "pdf-" + fixtureTestRevision + "/" + member.name,
		Mode:     0o644,
		Typeflag: tar.TypeReg,
		Size:     int64(len(member.body)),
	}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Write([]byte(member.body)); err != nil {
		t.Fatal(err)
	}
}

func TestForeignConstructorLateFailureNeverHandsOffOwnedStage(t *testing.T) {
	// Acquiring the actual owned stage uses process-wide platform temp environment.
	// Serial execution preserves ownership observation; no production failure hook is used.
	temporary := t.TempDir()
	for _, variable := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(variable, temporary)
	}

	for _, kind := range []string{constructorChecksumFailure, constructorLateCommit} {
		checkConstructorLateFailure(t, temporary, kind)
	}
}

func checkConstructorLateFailure(t *testing.T, temporary, kind string) {
	t.Helper()
	root, source := constructorSourceFixture(t)

	var lateMetadata *tar.Header
	if kind == constructorLateCommit {
		lateMetadata = &tar.Header{
			Name:       fixtureGlobalPAXName,
			Typeflag:   tar.TypeXGlobalHeader,
			PAXRecords: map[string]string{fixtureCommitKey: "wrong late revision"},
		}
	}

	data := constructorGitArchive(t, lateMetadata)
	if kind == constructorChecksumFailure {
		data[len(data)-8] ^= 1
	}

	cache, pin := seedConstructorArtifact(t, data)
	source.TestInputs = repopolicy.ForeignTestInputs{
		URL:    "https://codeload.github.com/benoitkugler/pdf/tar.gz/" + fixtureTestRevision,
		SHA256: pin,
	}
	writeConstructorRecord(t, root, source)

	before, err := foreignStageIdentity(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = repopolicy.ForeignSourcesContext(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	if err = verifyForeignTestArtifact(cache, pin); err != nil {
		t.Fatal(err)
	}

	proveLateFixtureReachedCandidates(t, data)

	stage, err := prepareForeignTests(t.Context(), root)
	if stage != nil || err == nil {
		t.Fatalf("%s admitted constructor/compiler handoff: stage=%v err=%v", kind, stage, err)
	}

	if kind == constructorChecksumFailure && !errors.Is(err, gzip.ErrChecksum) {
		t.Fatalf("CRC failed before late footer: %v", err)
	}

	if kind == constructorLateCommit && !strings.Contains(err.Error(), "commit mismatch") {
		t.Fatalf("late metadata failed for unrelated reason: %v", err)
	}

	assertConstructorCleaned(t, temporary, root, cache, pin, before)

	checkConstructorEarlyIdentityAndPinFailures(t, root, cache, pin)
	assertConstructorCleaned(t, temporary, root, cache, pin, before)
}

func proveLateFixtureReachedCandidates(t *testing.T, data []byte) {
	t.Helper()

	decoded, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(decoded)

	reader := tar.NewReader(decoded)
	found := false

	for {
		header, readErr := reader.Next()
		if readErr != nil {
			break
		}

		if strings.HasSuffix(header.Name, "/"+fixtureCandidateInput) {
			body, bodyErr := io.ReadAll(reader)
			if bodyErr != nil {
				t.Fatal(bodyErr)
			}

			found = strings.HasPrefix(string(body), "candidate-written-before-footer-")
		}
	}

	if !found {
		t.Fatal("malformed archive failed before actual fixture candidate")
	}
}

func assertConstructorCleaned(t *testing.T, temporary, root, cache, pin string, before map[string]foreignFixture) {
	t.Helper()

	left, err := filepath.Glob(filepath.Join(temporary, "pdfconcat-foreign-tests-*"))
	if err != nil || len(left) != 0 {
		t.Fatalf("constructor left ownedstage/modfile/compilerinput: %v %v", left, err)
	}

	after, err := foreignStageIdentity(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	if !maps.Equal(before, after) {
		t.Fatal("failed constructor changed canonical base/patch/source authority")
	}

	if err = verifyForeignTestArtifact(cache, pin); err != nil {
		t.Fatalf("failed constructor changed pinned cache: %v", err)
	}
}

func checkConstructorEarlyIdentityAndPinFailures(t *testing.T, root, cache, pin string) {
	t.Helper()

	canonical := filepath.Join(root, filepath.FromSlash(constructorRoot), constructorReader)

	original, err := os.ReadFile(filepath.Clean(canonical))
	if err != nil {
		t.Fatal(err)
	}

	writeForeignGraphFixture(t, canonical, "package reader\nfunc Value() int{return 3}\n")

	stage, err := prepareForeignTests(t.Context(), root)
	if stage != nil || err == nil || !strings.Contains(err.Error(), "foreign source identity") {
		t.Fatalf("early canonical drift bypassed: stage=%v err=%v", stage, err)
	}

	writeForeignGraphFixture(t, canonical, string(original))

	cached, err := os.ReadFile(filepath.Clean(cache))
	if err != nil {
		t.Fatal(err)
	}

	changed := bytes.Clone(cached)
	changed[0] ^= 1
	writeForeignGraphFixture(t, cache, string(changed))

	stage, err = prepareForeignTests(t.Context(), root)
	if stage != nil || err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("early cache pin drift bypassed: stage=%v err=%v", stage, err)
	}

	writeForeignGraphFixture(t, cache, string(cached))

	if err = verifyForeignTestArtifact(cache, pin); err != nil {
		t.Fatal(err)
	}
}
