// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenFileIdentitySurvivesRenameAndRejectsClosedHandle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "owned")

	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	identity, err := IdentityOfFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// A Windows open handle need not allow rename, so compare the original path while it remains open.
	named, err := IdentityOf(path)
	if err != nil || named != identity {
		t.Fatalf("open/name identity mismatch: %v %v", named, err)
	}

	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if _, closedErr := IdentityOfFile(file); closedErr == nil {
		t.Fatalf("closed handle identified: %v", closedErr)
	}

	requireTestFileClosed(t, file)

	moved := path + "-renamed"
	if renameErr := os.Rename(path, moved); renameErr != nil {
		t.Fatal(renameErr)
	}

	actual, err := IdentityOf(moved)
	if err != nil || actual != identity {
		t.Fatalf("renamed owned identity mismatch: %v %v", actual, err)
	}
}
