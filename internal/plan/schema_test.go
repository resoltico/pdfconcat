// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/plan"
)

// schemaProperties returns the sorted property names declared at the given path of the embedded schema.
func schemaProperties(t *testing.T, path ...string) []string {
	t.Helper()

	var node map[string]any

	err := json.Unmarshal([]byte(plan.Schema()), &node)
	if err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	for _, key := range path {
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("schema has no object at %q", strings.Join(path, "/"))
		}

		node = next
	}

	properties, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema node %q has no properties", strings.Join(path, "/"))
	}

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// TestSchemaDeclaresExactlyTheKeysTheDecoderAccepts guards against the schema and decoder drifting apart.
func TestSchemaDeclaresExactlyTheKeysTheDecoderAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path []string
		keys []string
	}{
		{"root", nil, []string{"$schema", "blank", "dir", "items", "output", "version"}},
		{"blank style", []string{"$defs", "blankStyle"}, []string{"background", "size", "text"}},
		{
			"text style",
			[]string{"$defs", "textStyle"},
			[]string{"align", "anchor", "color", "font", "leading", "size", "value", "width", "x", "y"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := schemaProperties(t, test.path...); !slices.Equal(got, test.keys) {
				t.Fatalf("schema properties = %v, want %v", got, test.keys)
			}
		})
	}
}

// TestSchemaAcceptedKeysAreDecodable decodes a plan using every schema key to prove the decoder knows them all.
func TestSchemaAcceptedKeysAreDecodable(t *testing.T) {
	t.Parallel()

	document, err := decode(t, `{
		"$schema": "x", "version": 1, "output": "o.pdf", "dir": "d",
		"blank": {"size": "A4", "background": "#fff", "text": {
			"value": "v", "font": "Courier", "size": 1, "color": "#000", "anchor": "top",
			"x": 1, "y": 2, "width": 3, "align": "left", "leading": 1}},
		"items": ["a.pdf", {"blank": {}, "count": 1}, {"dir": "e", "items": []}]
	}`)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if document.Blank.IsZero() {
		t.Fatal("blank defaults were dropped")
	}
}
