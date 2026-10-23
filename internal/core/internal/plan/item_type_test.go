// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
)

func TestInvalidItemTypeExplainsShapesAtExactLocation(t *testing.T) {
	for _, container := range []struct {
		name, prefix, suffix, pointer string
		column                        int
	}{
		{"root", "{\"version\":3,\"items\":[\n  ", "]}", "/items/0", 3},
		{"nested", "{\"version\":3,\"items\":[{\"title\":\"Section\",\"items\":[\n\t", "]}]}", "/items/0/items/0", 2},
	} {
		for _, value := range []string{`"source.pdf"`, `7`, `true`, `false`, `[]`} {
			for _, bom := range []string{"", "\xef\xbb\xbf"} {
				t.Run(container.name+value+bom, func(t *testing.T) {
					prefix := bom + container.prefix
					_, ds := plan.Decode([]byte(prefix+value+container.suffix), "packet.json", t.TempDir())
					if len(ds) != 1 || ds[0].Code != "plan_type" {
						t.Fatalf("wrong item type: %+v", ds)
					}
					d := ds[0]
					if d.Location == nil || d.Location.Pointer != container.pointer || d.Location.Offset != int64(len(prefix)) || d.Location.Line != 2 || d.Location.Column != container.column || d.File != "packet.json" || len(d.Items) != 1 || d.Items[0] != container.pointer {
						t.Fatalf("item location changed: %+v", d)
					}
					for _, want := range []string{"source object", "generated page object", "contents object", "container object", `{"source":"source.pdf"}`} {
						if !strings.Contains(d.Message, want) {
							t.Fatalf("missing %q in item guidance: %q", want, d.Message)
						}
					}
				})
			}
		}
	}
}

func TestItemBoundaryRetainsNullSyntaxAndMemberErrors(t *testing.T) {
	for _, tc := range []struct {
		input   string
		code    diag.Code
		pointer string
	}{
		{`{"version":3,"items":[null]}`, "plan_null", "/items/0"},
		{`{"version":3,"items":[{"title":"Section","items":[null]}]}`, "plan_null", "/items/0/items/0"},
		{`{"version":3,"items":["unterminated]}`, "plan_syntax", "/items/0"},
		{`{"version":3,"items":[tru]}`, "plan_syntax", "/items/0"},
		{`{"version":3,"items":[nul]}`, "plan_syntax", "/items/0"},
		{`{"version":3,"items":[1e]}`, "plan_syntax", "/items/0"},
		{`{"version":3,"items":[{"source":7}]}`, "plan_type", "/items/0/source"},
		{`{"version":3,"items":[{"page":false}]}`, "plan_type", "/items/0/page"},
		{`{"version":3,"items":[{}]}`, "plan_item_shape", "/items/0"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			_, ds := plan.Decode([]byte(tc.input), "packet.json", t.TempDir())
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Location.Pointer != tc.pointer {
				t.Fatalf("fault semantics changed: %+v", ds)
			}
			if strings.Contains(ds[0].Message, `{"source":"source.pdf"}`) {
				t.Fatalf("item-type advice obscured a different fault: %+v", ds)
			}
		})
	}
}

func TestCurrentItemShapesRemainAccepted(t *testing.T) {
	input := `{"version":3,"items":[{"source":"source.pdf"},{"page":{"size":"A4"}},{"contents":{}},{"title":"Section","items":[{"source":"appendix.pdf"}]}]}`
	j, ds := plan.Decode([]byte(input), "packet.json", t.TempDir())
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	leaves, sections := packet.Declarations(j)
	if len(j.Parts) != 4 || len(leaves) != 4 || len(sections) != 1 || leaves[0].Kind != "source" || leaves[1].Kind != "page" || leaves[2].Kind != "contents" || j.Parts[3].Title != "Section" {
		t.Fatalf("current item shapes changed: %+v", j)
	}
	var instance any
	if err := json.Unmarshal([]byte(input), &instance); err != nil {
		t.Fatal(err)
	}
	if err := compileSchema(t).Validate(instance); err != nil {
		t.Fatal(err)
	}
}
