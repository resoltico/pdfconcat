// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"path/filepath"
	"testing"
)

const privateFixturePattern = "private-*"

func TestPrivateCreatorsRejectUnownedNamePatterns(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"*", "plain", "nested/*", "nested\\*", "multiple**", "middle*tail"} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			parent := t.TempDir()

			if file, err := CreatePrivateTemp(parent, pattern); !errors.Is(err, errPrivatePattern) || file != nil {
				t.Fatalf("private file accepted invalid pattern: %v", err)
			}

			if path, err := createWorkspaceDirectory(parent, pattern); !errors.Is(err, errPrivatePattern) || path != "" {
				t.Fatalf("private workspace accepted invalid pattern: %v", err)
			}
		})
	}
}

func TestPrivateCreatorsRejectMissingParent(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "missing", "parent")

	if file, err := CreatePrivateTemp(parent, privateFixturePattern); err == nil || file != nil {
		t.Fatal("private file accepted missing parent")
	}

	if path, err := createWorkspaceDirectory(parent, privateFixturePattern); err == nil || path != "" {
		t.Fatal("private workspace accepted missing parent")
	}
}
