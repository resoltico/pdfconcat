// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type canceledFixtureReader struct {
	source io.Reader
	cancel context.CancelFunc
}

func (reader *canceledFixtureReader) Read(data []byte) (int, error) {
	n, err := reader.source.Read(data[:min(len(data), foreignTarBlockBytes)])
	reader.cancel()

	if err != nil {
		err = fmt.Errorf("read cancellation fixture: %w", err)
	}

	return n, err
}

func TestForeignQuarantineRejectsLateIncompleteMembership(t *testing.T) {
	t.Parallel()

	for _, missing := range []string{moduleFileName, fixtureSampleMarker, fixtureTestdataMarker} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			root, source := fixtureArchiveSource(t)

			members := []fixtureTarMember{{name: fixtureCandidateData, body: "valid candidate before late failure", kind: tar.TypeReg}}
			for _, member := range authenticFixtureMembers() {
				if member.name != missing && member.name != fixtureCandidateData {
					members = append(members, member)
				}
			}

			artifact := writeFixtureTar(t, members, fixtureTestRevision)

			destination := t.TempDir()
			if _, err := checkFixtureArchive(t, root, artifact, destination, source); err == nil {
				t.Fatal("missing late marker/shared input accepted")
			}

			if _, err := readInRoot(destination, fixtureCandidateData); err != nil {
				t.Fatal("membership negative never wrote private candidate")
			}
		})
	}
}

func TestForeignQuarantineCancellationInterruptsMemberCopy(t *testing.T) {
	t.Parallel()
	destination := t.TempDir()

	root, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	data := bytes.Repeat([]byte("fixture"), foreignTarFooterBytes)
	body := &canceledFixtureReader{source: bytes.NewReader(data), cancel: cancel}

	_, err = quarantineForeignMember(
		ctx,
		root,
		"pkg/testdata/cancel.raw",
		&tar.Header{Size: int64(len(data))},
		body,
		map[string]foreignFixture{},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("long member copy ignored cancellation: %v", err)
	}

	info, err := root.Lstat("pkg/testdata/cancel.raw")
	if err != nil {
		t.Fatal(err)
	}

	if info.Size() >= int64(len(data)) {
		t.Fatal("canceled member copied entire input")
	}
}

func TestForeignObservedDescriptorIgnoresPathSubstitution(t *testing.T) {
	t.Parallel()
	artifact := writeFixtureTar(t, authenticFixtureMembers(), fixtureTestRevision)

	data, err := readInRoot(filepath.Dir(artifact), filepath.Base(artifact))
	if err != nil {
		t.Fatal(err)
	}

	pin := fmt.Sprintf("%x", sha256.Sum256(data))

	err = validatedForeignStream(t.Context(), artifact, pin, func(reader *tar.Reader, stream *foreignDecodedStream) error {
		if renameErr := os.Rename(artifact, artifact+".opened"); renameErr != nil {
			return fmt.Errorf("substitute archive path: %w", renameErr)
		}

		writeForeignGraphFixture(t, artifact, "untrusted replacement")

		boundary := foreignTarBoundary{prefix: "fixture-" + fixtureTestRevision, revision: fixtureTestRevision, seen: map[string]byte{}}

		return boundary.consume(
			reader,
			stream,
			discardFixtureMember,
		)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = verifyForeignTestArtifact(artifact, pin); err == nil {
		t.Fatal("pathname substitution control did not replace bytes")
	}
}

func TestForeignDecodedLimitBoundsReadsAndSparseMetadata(t *testing.T) {
	t.Parallel()

	stream := &foreignDecodedStream{cancellation: t.Context().Err, source: bytes.NewReader([]byte{0, 0}), consumed: maxForeignTestDecoded}
	if _, err := stream.Read(make([]byte, foreignTarBlockBytes)); err == nil {
		t.Fatal("decoded limit accepted over-budget bytes")
	}

	boundary := foreignTarBoundary{prefix: "fixture-" + fixtureTestRevision, revision: fixtureTestRevision, seen: map[string]byte{}}

	sparse := &tar.Header{Name: "fixture-" + fixtureTestRevision + "/pkg/testdata/input", Typeflag: tar.TypeGNUSparse}
	if _, err := boundary.member(sparse); err == nil {
		t.Fatal("GNU sparse member accepted")
	}
}

func discardFixtureMember(_ string, _ *tar.Header, body io.Reader) error {
	if _, err := io.Copy(io.Discard, body); err != nil {
		return fmt.Errorf("read control archive member: %w", err)
	}

	return nil
}
