// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestLocatedFirstFaults(t *testing.T) {
	cases := []struct {
		input   string
		code    diag.Code
		pointer string
	}{
		{`{"version":2,"unknown":1,"items":[null]}`, "plan_unknown_member", "/unknown"},
		{`{"version":2,"a~/b":1}`, "plan_unknown_member", "/a~0~1b"},
		{`{"version":2,"items":[{"page":{},"count":3.0},null]}`, "plan_value", "/items/0/count"},
		{`{"version":2,"items":[{"page":{},"count":3e0}]}`, "plan_value", "/items/0/count"},
		{`{"version":2,"items":[{"page":{},"count":0}]}`, "plan_value", "/items/0/count"},
		{`{"version":2,"items":[{"page":{},"count":100001}]}`, "plan_limit", "/items/0/count"},
		{`{"version":2,"items":[{"page":{},"count":9223372036854775808}]}`, "plan_limit", "/items/0/count"},
		{`{"version":2,"items":[{"page":{},"count":"3"}]}`, "plan_type", "/items/0/count"},
		{`{"version":2,"items":[{"page":{},"count":null}]}`, "plan_null", "/items/0/count"},
		{`{"version":2,"items":[null],"items":[]}`, "plan_null", "/items/0"},
		{`{"version":2,"version":2,"items":[null]}`, "plan_duplicate_member", "/version"},
		{`{"version":2,"items":[{"page":{"text":{"size":null}}}]}`, "plan_null", "/items/0/page/text/size"},
		{`{"version":2,"items":[{"page":{"text":{"width":0}}}]}`, "plan_value", "/items/0/page/text/width"},
		{`{"version":2,"items":[{"page":{"text":{"font":{}}}}]}`, "plan_type", "/items/0/page/text/font"},
		{`{"version":2,"items":[{"page":{},"dir":"x"}]}`, "plan_item_shape", "/items/0"},
		{`{"version":2,"items":[{"items":["a.pdf"]}]}`, "plan_item_shape", "/items/0"},
		{`{"version":2,"items":[{"dir":"x","items":[]}]}`, "plan_value", "/items/0/items"},
		{`{"version":2,"items":[]}`, "plan_value", "/items"},
		{`{"version":2,"items":"x"}`, "plan_type", "/items"},
		{`{"version":2,"items":["\u0000"]}`, "path_invalid", "/items/0"},
		{`{"version":2,"items":[{"page":{"size":"0x10"}}]}`, "plan_value", "/items/0/page/size"},
		{`{"version":2,"items":["\ud800"]}`, "plan_syntax", "/items/0"},
		{`{"version":2,"items":["a.pdf"]} null`, "plan_syntax", ""},
		{`{"version":2`, "plan_syntax", ""},
		{`{}`, "plan_version_unsupported", ""},
		{`{"version":2}`, "plan_type", ""},
		{`{"version":3}`, "plan_version_unsupported", "/version"},
		{`{"Version":2}`, "plan_unknown_member", "/Version"},
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

func TestBOMOffsetsAndV1(t *testing.T) {
	plain := "{\n  \"version\":2,\n  \"items\":[null]}"
	_, a := plan.Decode([]byte(plain), "p", ".")
	_, b := plan.Decode([]byte("\xef\xbb\xbf"+plain), "p", ".")
	if a[0].Location.Offset+3 != b[0].Location.Offset || a[0].Location.Line != b[0].Location.Line || a[0].Location.Column != b[0].Location.Column {
		t.Fatalf("BOM offsets: %+v %+v", a, b)
	}
	for _, input := range []string{`{"blank":{},"items":[],"version":1}`, `{"version":1,"blank":{},"items":[]}`} {
		_, ds := plan.Decode([]byte(input), "p", ".")
		if ds[0].Code != "plan_version_unsupported" || !strings.Contains(ds[0].Message, "blank") || !strings.Contains(ds[0].Message, "page") || !strings.Contains(ds[0].Message, "2") {
			t.Fatalf("v1 migration: %+v", ds)
		}
	}
}

func TestLayeringOriginsAndPaths(t *testing.T) {
	base := t.TempDir()
	input := `{"version":2,"output":"packet.pdf","dir":"sources","page":{"text":{"value":"default","font":{"file":"fonts/top.ttf"},"width":"50mm"},"background":"#abc"},"items":[{"dir":"chapters","items":["a.pdf",{"page":{"background":"none","text":{"font":{"file":"local.ttf"},"value":""}}}]},{"page":{"text":{"font":"default"}}}]}`
	j, ds := plan.Decode([]byte(input), "p", base)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if len(j.Parts) != 3 || j.Output != filepath.Join(base, "packet.pdf") || j.Parts[0].Source != filepath.Join(base, "sources/chapters/a.pdf") {
		t.Fatalf("job %+v", j)
	}
	a, b := j.Parts[1].Page, j.Parts[2].Page
	if a.Background != nil || a.Text.Value != "" || a.Text.Font != filepath.Join(base, "sources/chapters/local.ttf") || b.Text.Font != "default" || b.Text.Value != "default" || b.Background == nil {
		t.Fatalf("layered: %+v %+v", a, b)
	}
	if a.Origins["font"] != "/items/0/items/1/page/text/font" || a.Origins["width"] != "/page/text/width" || b.Origins["background"] != "/page/background" {
		t.Fatalf("origins %+v %+v", a.Origins, b.Origins)
	}
	top, ds := plan.Decode([]byte(`{"version":2,"page":{"text":{"font":{"file":"top.ttf"}}},"items":[{"dir":"elsewhere","items":[{"page":{}}]}]}`), "p", base)
	if len(ds) > 0 || top.Parts[0].Page.Text.Font != filepath.Join(base, "top.ttf") {
		t.Fatalf("top font scope %+v %+v", top, ds)
	}
}

func TestLimits(t *testing.T) {
	for _, n := range []int{100000, 100001} {
		input := `{"version":2,"items":[` + strings.Repeat(`"a",`, n-1) + `"a"]}`
		j, ds := plan.Decode([]byte(input), "p", ".")
		if n == 100000 && (len(ds) > 0 || len(j.Parts) != n) || n == 100001 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("entry boundary %d: %d %+v", n, len(j.Parts), ds)
		}
	}
	for _, count := range []string{"100000", "100001"} {
		_, ds := plan.Decode([]byte(`{"version":2,"items":[{"page":{},"count":`+count+`}]}`), "p", ".")
		if count == "100000" && len(ds) > 0 || count == "100001" && len(ds) == 0 {
			t.Fatalf("count %s: %+v", count, ds)
		}
	}
	_, ds := plan.Decode([]byte(`{"version":2,"items":[{"page":{},"count":100000},{"page":{}}]}`), "p", ".")
	if len(ds) != 1 || ds[0].Code != "plan_limit" {
		t.Fatal(ds)
	}
	for _, n := range []int{64, 65} {
		input := `"a"`
		for range n {
			input = `{"dir":"x","items":[` + input + `]}`
		}
		_, ds := plan.Decode([]byte(`{"version":2,"items":[`+input+`]}`), "p", ".")
		if n == 64 && len(ds) > 0 || n == 65 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("depth %d: %+v", n, ds)
		}
	}
	for _, n := range []int{10000, 10001} {
		_, ds := plan.Decode([]byte(`{"version":2,"items":[{"page":{"text":{"value":"`+strings.Repeat("a", n)+`"}}}]}`), "p", ".")
		if n == 10000 && len(ds) > 0 || n == 10001 && (len(ds) != 1 || ds[0].Code != "plan_limit") {
			t.Fatalf("text %d: %+v", n, ds)
		}
	}
	input := append([]byte(`{"version":2,"items":["a"]}`), []byte(strings.Repeat(" ", plan.MaxBytes))...)
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
		`{"version":2,"items":[]}`, `{"version":2,"items":[{"page":{},"count":0}]}`,
		`{"version":2,"items":[{"page":{"text":{"size":-1}}}]}`, `{"version":2,"items":[{"page":{"text":{"width":0}}}]}`,
		`{"version":2,"items":[{"items":["a"]}]}`, `{"version":2,"items":[null]}`,
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
		input := []byte(`{"version":2,"items":[{"page":{},"count":` + count + `}]}`)
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
	for _, seed := range []string{`{"version":2,"items":["a.pdf"]}`, `{"version":2,"items":[{"page":{}}]}`, `{"version":2,"items":[null]}`, `{"version":1,"blank":{}}`, `{`, "\xef\xbb\xbf{}"} {
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
		{`{"version":2.0,"items":["a"]}`, "plan_version_unsupported", "/version"},
		{`{"version":999999999999999999999999,"items":["a"]}`, "plan_version_unsupported", "/version"},
		{`{"version":2,"items":[{"page":{"text":{"size":1e999}}}]}`, "plan_value", "/items/0/page/text/size"},
		{`{"version":2,"items":[{"page":{"text":{"leading":1e999}}}]}`, "plan_value", "/items/0/page/text/leading"},
	} {
		_, ds := plan.Decode([]byte(tc.input), "p", ".")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Location.Pointer != tc.pointer {
			t.Fatalf("numeric fault %s: %+v", tc.input, ds)
		}
	}
	input := `{"blank":{},"items":[],"version":1}`
	_, ds := plan.Decode([]byte(input), "p", ".")
	if ds[0].Location.Offset != int64(strings.LastIndex(input, "1")) {
		t.Fatalf("v1 location points away from its value: %+v", ds[0].Location)
	}
}
