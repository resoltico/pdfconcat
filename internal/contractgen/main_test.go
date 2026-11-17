// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDerivedArtifactsCurrentAndStaleControl(t *testing.T) {
	if err := run("../..", true); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"docs/CLI.md", "internal/plan/plan.schema.json"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "docs/CLI.md")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("stale control"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	// Alter a generated value, rather than unrelated prose that generation preserves.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("| `request_invalid` | error"), []byte("| `request_invalid` | warning"), 1)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = run(root, true); err == nil {
		t.Fatal("stale generated value admitted")
	}
}

func TestSchemaRegenerationPreservesExactIntegerTokens(t *testing.T) {
	data := []byte(`{"limit":9223372036854775807,"enum":["stale"]}`)
	generated, err := schema(data, map[string]any{"enum": []string{"current"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(generated, []byte(`9223372036854775807`)) || bytes.Contains(generated, []byte(`9223372036854776000`)) {
		t.Fatalf("semantic regeneration rounded an integer: %s", generated)
	}
}
