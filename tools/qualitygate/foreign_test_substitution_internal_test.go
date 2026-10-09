// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestForeignObservedStreamRejectsInPlaceChangeAfterOpening(t *testing.T) {
	t.Parallel()

	const randomBytes = 32768

	data := make([]byte, randomBytes)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	members := authenticFixtureMembers()
	members = append(members, fixtureTarMember{name: "pkg/testdata/unprefetched.raw", body: string(data), kind: tar.TypeReg})
	artifact := writeFixtureTar(t, members, fixtureTestRevision)

	compressed, err := readInRoot(filepath.Dir(artifact), filepath.Base(artifact))
	if err != nil {
		t.Fatal(err)
	}

	pin := fmt.Sprintf("%x", sha256.Sum256(compressed))

	err = validatedForeignStream(t.Context(), artifact, pin, func(reader *tar.Reader, stream *foreignDecodedStream) error {
		root, openErr := os.OpenRoot(filepath.Dir(artifact))
		if openErr != nil {
			return fmt.Errorf("open alteration directory: %w", openErr)
		}
		defer closeLogged(root)

		file, openErr := root.OpenFile(filepath.Base(artifact), os.O_WRONLY, 0)
		if openErr != nil {
			return fmt.Errorf("open in-place alteration: %w", openErr)
		}

		compressed[len(compressed)-8] ^= 1
		_, writeErr := file.WriteAt(compressed[len(compressed)-8:], int64(len(compressed)-8))

		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return fmt.Errorf("alter observed archive: %w", errors.Join(writeErr, closeErr))
		}

		boundary := foreignTarBoundary{prefix: "fixture-" + fixtureTestRevision, revision: fixtureTestRevision, seen: map[string]byte{}}

		return boundary.consume(reader, stream, discardFixtureMember)
	})
	if err == nil {
		t.Fatal("opened archive changed in-place without invalidating observed pin/trailer")
	}
}
