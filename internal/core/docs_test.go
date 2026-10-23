// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func documentedSchema(t *testing.T, kind core.SchemaKind) *jsonschema.Schema {
	t.Helper()
	var value any
	raw, err := core.Schema(kind)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result.json", value); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("result.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestEveryDocumentedJSONBlock(t *testing.T) {
	docs, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, "../../README.md")
	schema := documentedSchema(t, core.ResultSchema)
	planSchema := documentedSchema(t, core.PlanSchema)
	blocks := 0
	for _, path := range docs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		chunks := strings.Split(string(data), "```json\n")
		for i, chunk := range chunks[1:] {
			block, _, ok := strings.Cut(chunk, "```")
			if !ok {
				t.Fatalf("unclosed JSON block in %s", path)
			}
			blocks++
			var fields map[string]any
			if err = json.Unmarshal([]byte(block), &fields); err != nil {
				t.Fatalf("%s JSON block %d: %v", path, i+1, err)
			}
			if _, isResult := fields["format_version"]; isResult {
				if err = schema.Validate(fields); err != nil {
					t.Fatalf("%s result block %d: %v", path, i+1, err)
				}
			} else {
				if err = planSchema.Validate(fields); err != nil {
					t.Fatalf("%s plan schema block %d: %v", path, i+1, err)
				}
				if _, ds := plan.Decode([]byte(block), path, filepath.Dir(path)); len(ds) > 0 {
					t.Fatalf("%s plan block %d: %+v", path, i+1, ds)
				}
			}
		}
	}
	if blocks < 5 {
		t.Fatalf("unexpectedly few documented JSON examples: %d", blocks)
	}
}
