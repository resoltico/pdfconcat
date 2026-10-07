// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package doccontract_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

type (
	// enumNode is a schema node that lists the allowed names.
	enumNode struct {
		Enum []string `json:"enum"`
	}

	// sizeProperty is the schema of a blank's size: a paper-name enum or a free-form string.
	sizeProperty struct {
		OneOf []enumNode `json:"oneof"`
	}

	// blankProperties are the members of a blank object that the test reads.
	blankProperties struct {
		Size sizeProperty `json:"size"`
	}

	// blankDefinition is the schema's blank object.
	blankDefinition struct {
		Properties blankProperties `json:"properties"`
	}

	// textDefinition is the schema's text object, by member name.
	textDefinition struct {
		Properties map[string]enumNode `json:"properties"`
	}

	// schemaDefinitions are the schema's $defs that the test reads.
	schemaDefinitions struct {
		Text  textDefinition  `json:"text"`
		Blank blankDefinition `json:"blank"`
	}

	// planSchema is the plan schema as far as the test reads it.
	planSchema struct {
		Defs schemaDefinitions `json:"$defs"`
	}
)

// TestSchemaEnumsMatchDomainAndDocs fails if the schema's name lists drift from the code or from the docs.
func TestSchemaEnumsMatchDomainAndDocs(t *testing.T) {
	t.Parallel()

	var schema planSchema

	err := json.Unmarshal([]byte(plan.Schema()), &schema)
	if err != nil {
		t.Fatal(err)
	}

	if len(schema.Defs.Blank.Properties.Size.OneOf) == 0 {
		t.Fatal("the schema's blank size has no paper-name enum")
	}

	blankPages := readDocument(t, "docs/BLANK_PAGES.md")
	properties := schema.Defs.Text.Properties

	checkNames(t, "paper size", schema.Defs.Blank.Properties.Size.OneOf[0].Enum,
		slices.Concat([]string{"inherit"}, assembly.PageSizeNames()), blankPages)
	checkNames(t, "anchor", properties["anchor"].Enum, assembly.AnchorNames(), blankPages)
	checkNames(t, "alignment", properties["align"].Enum, assembly.AlignNames(), blankPages)
	checkNames(t, "overflow", properties["overflow"].Enum, assembly.OverflowNames(), blankPages)
}

// checkNames requires the schema's enum to equal the domain's names and every name to appear in the document.
func checkNames(t *testing.T, what string, schemaNames, domainNames []string, document string) {
	t.Helper()

	if !slices.Equal(schemaNames, domainNames) {
		t.Errorf("%s names: schema %v, code %v", what, schemaNames, domainNames)
	}

	for _, name := range domainNames {
		if !strings.Contains(document, "`"+name+"`") {
			t.Errorf("%s %q is not documented in docs/BLANK_PAGES.md", what, name)
		}
	}
}
