// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"testing/iotest"
)

func TestForeignQuarantineCopyFailureAndDeclaredSizePreventAuthority(t *testing.T) {
	t.Parallel()

	for _, body := range []io.Reader{iotest.ErrReader(io.ErrUnexpectedEOF), bytes.NewReader([]byte("short"))} {
		destination := t.TempDir()

		root, err := os.OpenRoot(destination)
		if err != nil {
			t.Fatal(err)
		}

		identity, copyErr := quarantineForeignMember(
			t.Context(),
			root,
			fixtureCandidateData,
			&tar.Header{Size: foreignTarBlockBytes},
			body,
			map[string]foreignFixture{},
		)

		closeErr := root.Close()
		if closeErr != nil {
			t.Fatal(closeErr)
		}

		if copyErr == nil || identity != (foreignFixture{}) {
			t.Fatal("failed/short member returned trusted identity")
		}
	}
}

func TestForeignQuarantineClosedOutputCannotPublishMember(t *testing.T) {
	t.Parallel()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err = root.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = quarantineForeignMember(
		t.Context(),
		root,
		fixtureCandidateData,
		&tar.Header{Size: 1},
		bytes.NewReader([]byte{'x'}),
		map[string]foreignFixture{},
	)
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed output accepted: %v", err)
	}
}
