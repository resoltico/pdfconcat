// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/plan"
)

func decode(t *testing.T, text string) (plan.Document, error) {
	t.Helper()
	return plan.Decode(strings.NewReader(text), filepath.Join(string(filepath.Separator), "base"))
}

func abs(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator), "base"}, parts...)...)
}

func TestDecodeFlattensGroupsAndResolvesPaths(t *testing.T) {
	t.Parallel()

	document, err := decode(t, `{
		"version": 1,
		"output": "out/book.pdf",
		"dir": "src",
		"items": [
			"cover.pdf",
			{"dir": "part1", "items": ["a.pdf", {"dir": "deep", "items": ["b.pdf"]}]},
			{"blank": {}, "count": 3},
			"/abs/c.pdf"
		]
	}`)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if document.Output != abs("out", "book.pdf") {
		t.Errorf("Output = %q", document.Output)
	}

	wantPaths := []string{
		abs("src", "cover.pdf"),
		abs("src", "part1", "a.pdf"),
		abs("src", "part1", "deep", "b.pdf"),
		"",
		filepath.Clean("/abs/c.pdf"),
	}

	items := document.Sequence.Items
	if len(items) != len(wantPaths) {
		t.Fatalf("items = %d, want %d", len(items), len(wantPaths))
	}

	for index, want := range wantPaths {
		if items[index].Path != want {
			t.Errorf("item %d path = %q, want %q", index, items[index].Path, want)
		}
	}

	if items[3].Kind != assembly.Blank || items[3].Count != 3 {
		t.Errorf("blank item = %+v", items[3])
	}
}

func TestDecodeReadsBlankStyles(t *testing.T) {
	t.Parallel()

	document, err := decode(t, "\xEF\xBB\xBF"+`{
		"$schema": "https://example.invalid/schema.json",
		"version": 1,
		"blank": {
			"size": "Letter",
			"background": "#eee",
			"text": {
				"value": "Hi", "font": "courier", "size": "10mm", "color": "#123456",
				"anchor": "Top-Right", "x": -5, "y": "1in", "width": 200, "align": "right", "leading": 1.5
			}
		},
		"items": ["a.pdf", {"blank": {"text": {"value": ""}}}]
	}`)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	spec, err := document.Blank.Resolve(assembly.PageDim{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	text := spec.Text
	if spec.Dim.Width != 612 || spec.Background.OrElse(assembly.Color{}) != (assembly.Color{R: 0xEE, G: 0xEE, B: 0xEE}) {
		t.Errorf("page = %+v", spec)
	}

	if text.Value != "Hi" || text.Font != "Courier" || text.Anchor != assembly.AnchorTopRight || text.Align != assembly.AlignRight ||
		text.X != -5 || text.Y != 72 || text.Width != 200 || text.Leading != 1.5 || text.Color != (assembly.Color{R: 0x12, G: 0x34, B: 0x56}) {
		t.Errorf("text = %+v", text)
	}

	if !document.Sequence.Items[1].Blank.Text.Value.IsSet() {
		t.Error("explicit empty text value must be recorded as set")
	}
}

func TestDecodeRejectsMalformedPlans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{"not json", `{"version": 1,`, "line"},
		{"syntax error position", "{\n  \"version\": 1,\n  items\n}", "line 3"},
		{"trailing data", `{"version":1,"items":["a.pdf"]} {}`, "after the top-level"},
		{"missing version", `{"items":["a.pdf"]}`, "version: required"},
		{"future version", `{"version":2,"items":["a.pdf"]}`, "version: 2 is not supported"},
		{"unknown field", `{"version":1,"itemz":[]}`, "itemz"},
		{"unknown nested field", `{"version":1,"blank":{"text":{"colour":"#000"}},"items":[]}`, "colour"},
		{"item is a number", `{"version":1,"items":[7]}`, "items[0]"},
		{"empty path", `{"version":1,"items":[""]}`, "items[0]: empty PDF path"},
		{"object with both", `{"version":1,"items":[{"blank":{},"dir":"x","items":[]}]}`, "items[0]"},
		{"object with neither", `{"version":1,"items":[{}]}`, "items[0]"},
		{"count without blank", `{"version":1,"items":[{"dir":"x","items":[],"count":2}]}`, "items[0]"},
		{"zero count", `{"version":1,"items":[{"blank":{},"count":0}]}`, "items[0].count"},
		{"absurd count", `{"version":1,"items":[{"blank":{},"count":2000000}]}`, "items[0].count"},
		{"invalid UTF-8", "{\"version\":1,\"items\":[\"a\xff.pdf\"]}", "UTF-8"},
		{"group without items", `{"version":1,"items":[{"dir":"x"}]}`, "needs \"items\""},
		{"bad color path", `{"version":1,"items":["a.pdf",{"blank":{"text":{"color":"red"}}}]}`, "items[1].blank.text.color"},
		{"bad default size", `{"version":1,"blank":{"size":"huge"},"items":[]}`, "blank.size"},
		{
			"bad nested group item",
			`{"version":1,"items":[{"dir":"d","items":["a.pdf",{"blank":{"background":"#12"}}]}]}`,
			"items[0].items[1].blank.background",
		},
		{"length type", `{"version":1,"blank":{"text":{"x":true}},"items":[]}`, "number of points"},
		{"bad length unit", `{"version":1,"blank":{"text":{"x":"3px"}},"items":[]}`, "blank.text.x"},
		{"bad font", `{"version":1,"blank":{"text":{"font":"Papyrus"}},"items":[]}`, "blank.text.font"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := decode(t, test.text)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestDecodeRejectsOversizedPlans(t *testing.T) {
	t.Parallel()

	reader := &endlessReader{}
	if _, err := plan.Decode(reader, abs()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Decode() error = %v, want size limit", err)
	}
}

type endlessReader struct{}

func (*endlessReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = ' '
	}

	return len(buffer), nil
}
