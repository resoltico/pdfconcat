// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
	for _, tc := range []struct{ path, current, stale string }{
		{"docs/CLI.md", "| `request_invalid` | error", "| `request_invalid` | warning"},
		{"internal/plan/plan.schema.json", `"default": 0.01`, `"default": 0.1`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			path := filepath.Join(root, tc.path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			altered := bytes.Replace(data, []byte(tc.current), []byte(tc.stale), 1)
			if bytes.Equal(data, altered) {
				t.Fatal("negative control did not change a generated value")
			}
			if err = os.WriteFile(path, altered, 0600); err != nil {
				t.Fatal(err)
			}
			if err = run(root, true); err == nil || !strings.Contains(err.Error(), "stale generated artifact: "+tc.path) {
				t.Fatalf("stale generated value: %v", err)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		})
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
