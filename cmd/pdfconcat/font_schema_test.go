// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"encoding/json/v2"
	"os/exec"
	"strings"
	"testing"
)

func TestEmittedPlanSchemaDescribesStaticFonts(t *testing.T) {
	binary := buildBinary(t)
	data, err := exec.CommandContext(t.Context(), binary, "schema", "plan").Output()
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Page struct {
				Properties struct {
					Text struct {
						Properties struct {
							Font struct {
								OneOf []struct {
									Properties struct {
										File struct {
											Description string `json:"description"`
										} `json:"file"`
									} `json:"properties"`
								} `json:"oneOf"`
							} `json:"font"`
						} `json:"properties"`
					} `json:"text"`
				} `json:"properties"`
			} `json:"page"`
		} `json:"$defs"`
		Properties struct {
			PageDefaults struct {
				Description string `json:"description"`
			} `json:"page_defaults"`
		} `json:"properties"`
	}
	if err = json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Defs.Page.Properties.Text.Properties.Font.OneOf) != 2 {
		t.Fatal("font schema alternatives changed")
	}
	description := schema.Defs.Page.Properties.Text.Properties.Font.OneOf[1].Properties.File.Description
	for _, want := range []string{"Static TrueType (glyf) or OpenType CFF1", "Variable fonts, CFF2 and collections are unsupported", "base directory for page_defaults"} {
		if !strings.Contains(description, want) {
			t.Fatalf("emitted font schema missing %q: %s", want, description)
		}
	}
	if !strings.Contains(schema.Properties.PageDefaults.Description, "Contents also inherits text.font") {
		t.Fatal(schema.Properties.PageDefaults.Description)
	}
}
