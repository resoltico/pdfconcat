// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package result_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	result "github.com/resoltico/pdfconcat/internal/wire"
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

func TestFittingResultSchemaBoundsAndVersion(t *testing.T) {
	type bounds struct {
		Minimum float64 `json:"minimum"`
		Maximum float64 `json:"maximum"`
	}
	type version struct {
		Const int `json:"const"`
	}
	var schema struct {
		Definitions struct {
			Result struct {
				Properties struct {
					Version version `json:"format_version"`
					Fitting struct {
						Variants []struct {
							Properties struct {
								Target struct {
									Properties struct {
										Width  bounds `json:"width"`
										Height bounds `json:"height"`
									} `json:"properties"`
								} `json:"target"`
								Tolerance bounds `json:"tolerance"`
							} `json:"properties"`
						} `json:"anyOf"`
					} `json:"fitting"`
				} `json:"properties"`
			} `json:"result"`
			Version struct {
				Properties struct {
					Version version `json:"format_version"`
				} `json:"properties"`
			} `json:"version"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(result.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Definitions.Result.Properties.Version.Const != result.WireVersion ||
		schema.Definitions.Version.Properties.Version.Const != result.WireVersion {
		t.Fatal("schema and wire version disagree")
	}
	variants := schema.Definitions.Result.Properties.Fitting.Variants
	if len(variants) != 2 {
		t.Fatal("schema does not declare nullable fitting")
	}
	policy := variants[1].Properties
	if policy.Target.Properties.Width != (bounds{1, packet.MaxSide}) ||
		policy.Target.Properties.Height != (bounds{1, packet.MaxSide}) ||
		policy.Tolerance != (bounds{0, packet.MaxSide}) {
		t.Fatalf("schema and fitting bounds disagree: %+v", policy)
	}
}
