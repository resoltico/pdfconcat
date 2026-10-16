// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestLocatedFirstFaults(t *testing.T) {
	cases := []struct {
		input   string
		code    diag.Code
		pointer string
	}{
		{`{"version":3,"unknown":1,"items":[null]}`, "plan_unknown_member", "/unknown"},
		{`{"version":3,"a~/b":1}`, "plan_unknown_member", "/a~0~1b"},
		{`{"version":3,"items":[{"page":{},"count":3.0},null]}`, "plan_value", "/items/0/count"},
		{`{"version":3,"items":[{"page":{},"count":3e0}]}`, "plan_value", "/items/0/count"},
		{`{"version":3,"items":[{"page":{},"count":0}]}`, "plan_value", "/items/0/count"},
		{`{"version":3,"items":[{"page":{},"count":100001}]}`, "plan_limit", "/items/0/count"},
		{`{"version":3,"items":[{"page":{},"count":9223372036854775808}]}`, "plan_limit", "/items/0/count"},
		{`{"version":3,"items":[{"page":{},"count":"3"}]}`, "plan_type", "/items/0/count"},
		{`{"version":3,"items":[{"page":{},"count":null}]}`, "plan_null", "/items/0/count"},
		{`{"version":3,"items":[null],"items":[]}`, "plan_null", "/items/0"},
		{`{"version":3,"version":3,"items":[null]}`, "plan_duplicate_member", "/version"},
		{`{"version":3,"items":[{"page":{"text":{"size":null}}}]}`, "plan_null", "/items/0/page/text/size"},
		{`{"version":3,"items":[{"page":{"text":{"width":0}}}]}`, "plan_value", "/items/0/page/text/width"},
		{`{"version":3,"items":[{"page":{"text":{"font":{}}}}]}`, "plan_type", "/items/0/page/text/font"},
		{`{"version":3,"items":[{"page":{},"dir":"x"}]}`, "plan_item_shape", "/items/0"},
		{`{"version":3,"items":[{"items":[{"source":"a.pdf"}]}]}`, "plan_item_shape", "/items/0"},
		{`{"version":3,"items":[{"dir":"x","items":[]}]}`, "plan_value", "/items/0/items"},
		{`{"version":3,"items":[]}`, "plan_value", "/items"},
		{`{"version":3,"items":"x"}`, "plan_type", "/items"},
		{`{"version":3,"items":[{"source":"\u0000"}]}`, "path_invalid", "/items/0/source"},
		{`{"version":3,"items":[{"page":{"size":"0x10"}}]}`, "plan_value", "/items/0/page/size"},
		{`{"version":3,"items":[{"source":"\ud800"}]}`, "plan_syntax", "/items/0/source"},
		{`{"version":3,"items":[{"source":"a.pdf"}]} null`, "plan_syntax", ""},
		{`{"version":3`, "plan_syntax", ""},
		{`{}`, "plan_version_unsupported", ""},
		{`{"version":3}`, "plan_type", ""},
		{`{"version":4}`, "plan_version_unsupported", "/version"},
		{`{"version":1,"blank":{},"items":[]}`, "plan_version_unsupported", "/version"},
		{`{"blank":{},"items":[],"version":1}`, "plan_unknown_member", "/blank"},
		{`{"items":[null],"version":1}`, "plan_null", "/items/0"},
		{`{"Version":3}`, "plan_unknown_member", "/Version"},
	}
	for _, tc := range cases {
		t.Run(string(tc.code)+tc.pointer+tc.input, func(t *testing.T) {
			_, ds := plan.Decode([]byte(tc.input), "plan.json", t.TempDir())
			if len(ds) != 1 {
				t.Fatalf("expected one error, got %+v", ds)
			}
			d := ds[0]
			if d.Code != tc.code || d.Location == nil || d.Location.Pointer != tc.pointer {
				t.Fatalf("got %+v location %+v; want %s %s", d, d.Location, tc.code, tc.pointer)
			}
			l := d.Location
			before := tc.input[:l.Offset]
			wantLine := strings.Count(before, "\n") + 1
			wantColumn := len(before) - strings.LastIndex(before, "\n")
			if l.Line != wantLine || l.Column != wantColumn || l.File != "plan.json" {
				t.Fatalf("location %+v", l)
			}
		})
	}
}

func TestBOMOffsets(t *testing.T) {
	plain := "{\n  \"version\":3,\n  \"items\":[null]}"
	_, a := plan.Decode([]byte(plain), "p", ".")
	_, b := plan.Decode([]byte("\xef\xbb\xbf"+plain), "p", ".")
	if a[0].Location.Offset+3 != b[0].Location.Offset || a[0].Location.Line != b[0].Location.Line || a[0].Location.Column != b[0].Location.Column {
		t.Fatalf("BOM offsets: %+v %+v", a, b)
	}
}

func TestLayeringOriginsAndPaths(t *testing.T) {
	base := t.TempDir()
	input := `{"version":3,"output":"packet.pdf","dir":"sources","page":{"text":{"value":"default","font":{"file":"fonts/top.ttf"},"width":"50mm"},"background":"#abc"},"items":[{"dir":"chapters","items":[{"source":"a.pdf"},{"page":{"background":"none","text":{"font":{"file":"local.ttf"},"value":""}}}]},{"page":{"text":{"font":"default"}}}]}`
	j, ds := plan.Decode([]byte(input), "p", base)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(j.Parts) != 2 || j.Output != filepath.Join(base, "packet.pdf") || j.Parts[0].Items[0].Source != filepath.Join(base, "sources/chapters/a.pdf") {
		t.Fatalf("job %+v", j)
	}
	a, b := j.Parts[0].Items[1].Page, j.Parts[1].Page
	if a.Background != nil || a.Text.Value != "" || a.Text.Font != filepath.Join(base, "sources/chapters/local.ttf") || b.Text.Font != "default" || b.Text.Value != "default" || b.Background == nil {
		t.Fatalf("layered: %+v %+v", a, b)
	}
	if a.Origins["font"] != "/items/0/items/1/page/text/font" || a.Origins["width"] != "/page/text/width" || b.Origins["background"] != "/page/background" {
		t.Fatalf("origins %+v %+v", a.Origins, b.Origins)
	}
	top, ds := plan.Decode([]byte(`{"version":3,"page":{"text":{"font":{"file":"top.ttf"}}},"items":[{"dir":"elsewhere","items":[{"page":{}}]}]}`), "p", base)
	if len(ds) > 0 || top.Parts[0].Items[0].Page.Text.Font != filepath.Join(base, "top.ttf") {
		t.Fatalf("top font scope %+v %+v", top, ds)
	}
}

func TestLimits(t *testing.T) {
	for _, n := range []int{100000, 100001} {
		input := `{"version":3,"items":[` + strings.Repeat(`{"source":"a"},`, n-1) + `{"source":"a"}]}`
		j, ds := plan.Decode([]byte(input), "p", ".")
		if n == 100000 && (len(ds) > 0 || len(j.Parts) != n) || n == 100001 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("entry boundary %d: %d %+v", n, len(j.Parts), ds)
		}
	}
	for _, count := range []string{"100000", "100001"} {
		_, ds := plan.Decode([]byte(`{"version":3,"items":[{"page":{},"count":`+count+`}]}`), "p", ".")
		if count == "100000" && len(ds) > 0 || count == "100001" && len(ds) == 0 {
			t.Fatalf("count %s: %+v", count, ds)
		}
	}
	_, ds := plan.Decode([]byte(`{"version":3,"items":[{"page":{},"count":100000},{"page":{}}]}`), "p", ".")
	if len(ds) != 1 || ds[0].Code != "plan_limit" {
		t.Fatal(ds)
	}
	for _, n := range []int{64, 65} {
		input := `{"source":"a"}`
		for range n {
			input = `{"dir":"x","items":[` + input + `]}`
		}
		_, ds := plan.Decode([]byte(`{"version":3,"items":[`+input+`]}`), "p", ".")
		if n == 64 && len(ds) > 0 || n == 65 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("depth %d: %+v", n, ds)
		}
	}
	for _, n := range []int{10000, 10001} {
		_, ds := plan.Decode([]byte(`{"version":3,"items":[{"page":{"text":{"value":"`+strings.Repeat("a", n)+`"}}}]}`), "p", ".")
		if n == 10000 && len(ds) > 0 || n == 10001 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("text %d: %+v", n, ds)
		}
	}
	input := append([]byte(`{"version":3,"items":[{"source":"a"}]}`), []byte(strings.Repeat(" ", plan.MaxBytes))...)
	_, ds = plan.Decode(input, "p", ".")
	if len(ds) != 1 || ds[0].Code != "plan_too_large" {
		t.Fatal(ds)
	}
}

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	var instance any
	if err := json.Unmarshal(plan.Schema, &instance); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("plan.json", instance); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("plan.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestSchemaAndExamples(t *testing.T) {
	schema := compileSchema(t)
	paths, err := filepath.Glob("../../examples/plans/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples not found: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, ds := plan.Decode(data, path, filepath.Dir(path))
		if len(ds) > 0 {
			t.Fatalf("%s: %+v", path, ds)
		}
		var instance any
		if err = json.Unmarshal(data, &instance); err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(instance); err != nil {
			t.Fatalf("%s schema: %v", path, err)
		}
	}
	for _, input := range []string{
		`{"version":3,"items":[]}`, `{"version":3,"items":[{"page":{},"count":0}]}`,
		`{"version":3,"items":[{"page":{"text":{"size":-1}}}]}`, `{"version":3,"items":[{"page":{"text":{"width":0}}}]}`,
		`{"version":3,"items":[{"items":[{"source":"a"}]}]}`, `{"version":3,"items":[null]}`,
	} {
		var instance any
		if err := json.Unmarshal([]byte(input), &instance); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(instance); err == nil {
			t.Fatalf("schema accepted %s", input)
		}
	}
	// Standard schema integer semantics cannot enforce lexical integer tokens.
	for _, count := range []string{"3.0", "3e0"} {
		input := []byte(`{"version":3,"items":[{"page":{},"count":` + count + `}]}`)
		var instance any
		if err := json.Unmarshal(input, &instance); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(instance); err != nil {
			t.Fatal(err)
		}
		if _, ds := plan.Decode(input, "p", "."); len(ds) != 1 || ds[0].Code != "plan_value" {
			t.Fatalf("lexical rejection: %+v", ds)
		}
	}
}

func FuzzPlanDecode(f *testing.F) {
	for _, seed := range []string{`{"version":3,"items":[{"source":"a.pdf"}]}`, `{"version":3,"items":[{"page":{}}]}`, `{"version":3,"items":[null]}`, `{"version":1,"blank":{}}`, `{`, "\xef\xbb\xbf{}"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		_, ds := plan.Decode(input, "p", ".")
		if len(ds) > 1 {
			t.Fatal("decoder returned multiple faults")
		}
	})
}

func TestNumericVersionAndLengthFaultClasses(t *testing.T) {
	for _, tc := range []struct {
		input   string
		code    diag.Code
		pointer string
	}{
		{`{"version":3.0,"items":[{"source":"a"}]}`, "plan_version_unsupported", "/version"},
		{`{"version":999999999999999999999999,"items":[{"source":"a"}]}`, "plan_version_unsupported", "/version"},
		{`{"version":3,"items":[{"page":{"text":{"size":1e999}}}]}`, "plan_value", "/items/0/page/text/size"},
		{`{"version":3,"items":[{"page":{"text":{"leading":1e999}}}]}`, "plan_value", "/items/0/page/text/leading"},
	} {
		_, ds := plan.Decode([]byte(tc.input), "p", ".")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Location.Pointer != tc.pointer {
			t.Fatalf("numeric fault %s: %+v", tc.input, ds)
		}
	}
	input := `{"version":1,"items":[]}`
	_, ds := plan.Decode([]byte(input), "p", ".")
	if ds[0].Location.Offset != int64(strings.LastIndex(input, "1")) {
		t.Fatalf("version location points away from its value: %+v", ds[0].Location)
	}
}

func TestPacketShapesScopesAndDefaults(t *testing.T) {
	base := t.TempDir()
	input := `{"version":3,"dir":"inputs","page":{"text":{"value":"inherited","font":{"file":"top.ttf"}},"background":"#abc"},"page_numbers":{},"items":[{"contents":{}},{"title":"  Rīga Ελληνικά Кириллица  ","dir":"chapter","start_on":"odd","divider":{"text":{"font":{"file":"title.ttf"}}},"items":[{"source":"source.pdf","pages":[[2,3],[4,4],[8,8]]},{"page":{"background":"none","text":{"value":""}},"count":2}]},{"title":"漢字","items":[{"source":"last.pdf"}]}]}`
	j, ds := plan.Decode([]byte(input), "packet.json", base)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	leaves, sections := job.Declarations(j)
	if len(j.Parts) != 3 || len(leaves) != 5 || len(sections) != 2 || sections[0].Title != "  Rīga Ελληνικά Кириллица  " || sections[1].Title != "漢字" {
		t.Fatalf("lost declared structure: %+v %+v", j, sections)
	}
	section := j.Parts[1]
	if section.Dir != "chapter" || section.Divider.Text.Font != filepath.Join(base, "inputs/title.ttf") || section.Items[0].Source != filepath.Join(base, "inputs/chapter/source.pdf") || section.Items[1].Page.Text.Font != filepath.Join(base, "top.ttf") {
		t.Fatalf("declaration path scope: %+v", section)
	}
	if len(section.Items[0].Selection) != 2 || section.Items[0].Selection[0] != (job.PageRange{First: 2, Last: 4}) || section.Items[0].Selection[1] != (job.PageRange{First: 8, Last: 8}) {
		t.Fatalf("normalized ranges: %+v", section.Items[0].Selection)
	}
	if leaves[1].Kind != "divider" || leaves[1].Owner != "/items/1" || leaves[1].Page.Text.Value != section.Title || section.Divider.Text.Value != "inherited" || leaves[1].Pointer != "/items/1/divider" {
		t.Fatalf("divider title derivation: %+v", leaves[1])
	}
	if j.PageNumbers == nil || j.PageNumbers.Size != 9 || j.PageNumbers.Margin != 8*72/25.4 || j.Parts[0].Contents.Title != "Contents" || j.Parts[0].Contents.Size != (job.Size{}) {
		t.Fatalf("packet defaults: %+v", j)
	}
	if section.Items[1].Page.Background != nil || section.Items[1].Page.Text.Value != "" || section.Items[1].Page.Origins["value"] != "/items/1/items/1/page/text/value" {
		t.Fatalf("style presence lost: %+v", section.Items[1].Page)
	}
	var instance any
	if err := json.Unmarshal([]byte(input), &instance); err != nil {
		t.Fatal(err)
	}
	if err := compileSchema(t).Validate(instance); err != nil {
		t.Fatal(err)
	}
}

func TestPacketRejectingContracts(t *testing.T) {
	cases := []struct {
		input, pointer string
		code           diag.Code
	}{
		{`{"version":2,"items":[{"source":"x.pdf"}]}`, "/version", "plan_version_unsupported"},
		{`{"version":3,"items":["x.pdf"]}`, "/items/0", "plan_type"},
		{`{"version":3,"items":[{"source":"x","page":{}}]}`, "/items/0", "plan_item_shape"},
		{`{"version":3,"items":[{"source":"x","count":2}]}`, "/items/0", "plan_item_shape"},
		{`{"version":3,"items":[{"items":[{"source":"x"}]}]}`, "/items/0", "plan_item_shape"},
		{`{"version":3,"items":[{"divider":{},"start_on":"odd","items":[{"source":"x"}]}]}`, "/items/0", "plan_item_shape"},
		{`{"version":3,"items":[{"title":"A","divider":{"text":{"value":"B"}},"items":[{"source":"x"}]}]}`, "/items/0/divider/text/value", "plan_value"},
		{`{"version":3,"items":[{"title":"A","divider":{"text":{"value":"B","unknown":0}},"items":[{"source":"x"}]}]}`, "/items/0/divider/text/value", "plan_value"},
		{`{"version":3,"items":[{"contents":{}}]}`, "/items/0/contents", "plan_value"},
		{`{"version":3,"items":[{"title":"A","items":[{"contents":{}},{"contents":{}}]}]}`, "/items/0/items/1/contents", "plan_value"},
		{`{"version":3,"items":[{"title":"A","items":[{"contents":{"size":"inherit"}}]}]}`, "/items/0/items/0/contents/size", "plan_value"},
		{`{"version":3,"page_numbers":{"margin":-1},"items":[{"source":"x"}]}`, "/page_numbers/margin", "plan_value"},
		{`{"version":3,"page_numbers":{"size":0},"items":[{"source":"x"}]}`, "/page_numbers/size", "plan_value"},
		{`{"version":3,"page_numbers":null,"items":[{"source":"x"}]}`, "/page_numbers", "plan_null"},
		{`{"version":3,"page_numbers":{"margin":1,"margin":2},"items":[{"source":"x"}]}`, "/page_numbers/margin", "plan_duplicate_member"},
		{`{"version":3,"items":[{"source":"x","pages":[[1,2],[2,3]]}]}`, "/items/0/pages/1", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[[4,4],[1,2]]}]}`, "/items/0/pages/1", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[[2,1]]}]}`, "/items/0/pages/0", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[]}]}`, "/items/0/pages", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[[0,2]]}]}`, "/items/0/pages/0/0", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[[1.0,2]]}]}`, "/items/0/pages/0/0", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[[1,2e0]]}]}`, "/items/0/pages/0/1", "plan_value"},
		{`{"version":3,"items":[{"source":"x","pages":[[1,null]]}]}`, "/items/0/pages/0/1", "plan_null"},
		{`{"version":3,"items":[{"source":"x","pages":[[1,2,3]]}]}`, "/items/0/pages/0/2", "plan_type"},
		{`{"version":3,"items":[{"source":"x","pages":[[1,9223372036854775808]]}]}`, "/items/0/pages/0/1", "plan_value"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			_, ds := plan.Decode([]byte(tc.input), "packet.json", t.TempDir())
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Location.Pointer != tc.pointer {
				t.Fatalf("want %s %s: %+v", tc.code, tc.pointer, ds)
			}
		})
	}
	for _, title := range []string{"", " ", "\u00a0", "first\nsecond", "bad\x1b", "bad\u200b", "bad\u2028", "bad\u2029"} {
		encoded, err := json.Marshal(title)
		if err != nil {
			t.Fatal(err)
		}
		input := `{"version":3,"items":[{"title":` + string(encoded) + `,"items":[{"source":"x"}]}]}`
		_, ds := plan.Decode([]byte(input), "packet.json", t.TempDir())
		if len(ds) != 1 || ds[0].Code != "plan_value" || ds[0].Location.Pointer != "/items/0/title" {
			t.Fatalf("invalid title %q accepted: %+v", title, ds)
		}
		var instance any
		if err = json.Unmarshal([]byte(input), &instance); err != nil {
			t.Fatal(err)
		}
		if err = compileSchema(t).Validate(instance); err == nil {
			t.Fatalf("schema accepted invalid title %q", title)
		}
	}
}

func TestAggregateContainerAndSelectionBudgets(t *testing.T) {
	for _, count := range []int{99999, 100000} {
		input := `{"version":3,"items":[{"title":"A","items":[` + strings.Repeat(`{"source":"x"},`, count-1) + `{"source":"x"}]}]}`
		_, ds := plan.Decode([]byte(input), "p", ".")
		if count == 99999 && len(ds) > 0 || count == 100000 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("container item budget %d: %+v", count, ds)
		}
	}
	for _, count := range []int{100000, 100001} {
		var pairs strings.Builder
		for i := range count {
			if i > 0 {
				pairs.WriteByte(',')
			}
			fmt.Fprintf(&pairs, "[%d,%d]", i+1, i+1)
		}
		input := `{"version":3,"items":[{"source":"x","pages":[` + pairs.String() + `]}]}`
		j, ds := plan.Decode([]byte(input), "p", ".")
		if count == 100000 && (len(ds) > 0 || len(j.Parts[0].Selection) != 1 || j.Parts[0].Selection[0].Last != count) || count == 100001 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("selection pair budget %d: %+v", count, ds)
		}
	}
}
