// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMutationCaptureReaderPreservesBytesAndRejectsOtherObjects(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "capture.json"), []byte("retained bytes\n"), fileMode); err != nil {
		t.Fatal(err)
	}

	open := mutationCaptureReader(directory)

	reader, err := open("capture.json")
	if err != nil {
		t.Fatal(err)
	}

	data, readErr := io.ReadAll(reader)

	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(data) != "retained bytes\n" {
		t.Fatalf("capture changed: %q %v %v", data, readErr, closeErr)
	}

	for _, name := range []string{"../capture.json", "/capture.json", "nested/capture.json", ".", "missing.json"} {
		if file, openErr := open(name); openErr == nil {
			if unexpectedCloseErr := file.Close(); unexpectedCloseErr != nil {
				t.Error(unexpectedCloseErr)
			}

			t.Errorf("invalid capture accepted: %q", name)
		}
	}
}
