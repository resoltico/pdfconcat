// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package result_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

func TestPartKindSchemaMatchesPacketVocabulary(t *testing.T) {
	var schema struct {
		Definitions struct {
			Part struct {
				Properties struct {
					Kind struct {
						Values []string `json:"enum"`
					} `json:"kind"`
				} `json:"properties"`
			} `json:"part"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(result.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	actual := schema.Definitions.Part.Properties.Kind.Values
	expected := packet.PartKind("").Values()
	if !slices.Equal(actual, expected) {
		t.Fatalf("schema part kinds %v; packet kinds %v", actual, expected)
	}
}
