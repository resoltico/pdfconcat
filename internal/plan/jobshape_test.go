// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestDecodedJobHeader(t *testing.T) {
	t.Parallel()

	job := mustDecode(t, `{"$schema":"x","version":1.0,"output":"out/o.pdf","dir":"src","items":["a.pdf"]}`)

	if job.Base != baseDir || job.Output.Value != "out/o.pdf" || job.Dir.Value != "src" || job.ItemsBase() != "/base/src" {
		t.Fatalf("job header: %+v", job)
	}
}

func TestDecodedDefaultsResolveToTheWrittenValues(t *testing.T) {
	t.Parallel()

	job := mustDecode(t, `{"version":1,"blank":{"size":"100x200","background":"#abc",`+
		`"text":{"value":"","font":"default","size":"14pt","color":"#102030","anchor":"top-left","x":"10in","y":-30,"width":300,`+
		`"align":"justify","leading":1.5,"overflow":"allow"}},"items":["a.pdf"]}`)

	spec, err := job.Defaults.Resolve(assembly.PageDim{})
	if err != nil {
		t.Fatal(err)
	}

	want := assembly.BlankSpec{
		Dim:        assembly.PageDim{Width: 100, Height: 200},
		Background: assembly.FillColor(assembly.Color{R: 0xAA, G: 0xBB, B: 0xCC}),
		Text: assembly.TextSpec{
			Font: assembly.Font{}, Size: 14, X: 720, Y: -30, Width: 300, Leading: 1.5, Color: assembly.Color{R: 0x10, G: 0x20, B: 0x30},
			Anchor: assembly.AnchorTopLeft, Align: assembly.AlignJustify, Overflow: assembly.OverflowAllow,
		},
	}
	if spec != want {
		t.Fatalf("resolved defaults:\n got %+v\nwant %+v", spec, want)
	}

	if !job.Defaults.Text.Value.IsSet() || !job.Defaults.Text.Font.IsSet() {
		t.Fatal("an explicit empty text and an explicit built-in font are values, not absences")
	}
}

func kindsOf(items []assembly.Item) []assembly.ItemKind {
	kinds := make([]assembly.ItemKind, 0, len(items))
	for index := range items {
		kinds = append(kinds, items[index].Kind)
	}

	return kinds
}

func TestDecodedItems(t *testing.T) {
	t.Parallel()

	job := mustDecode(t, `{"version":1,"items":["a.pdf",{"blank":{"background":"none"},"count":2},`+
		`{"dir":"g","items":["b.pdf",{"blank":{}}]}]}`)

	if got := kindsOf(job.Items); !slices.Equal(got, []assembly.ItemKind{assembly.ItemPDF, assembly.ItemBlank, assembly.ItemGroup}) {
		t.Fatalf("kinds: %v", got)
	}

	blank := job.Items[1].Blank
	if blank.Count != 2 || !blank.Style.Background.IsSet() || blank.Style.Background.Value.Painted {
		t.Fatalf("blank item: %+v", blank)
	}

	group := job.Items[2]
	if group.Dir.Value != "g" || !slices.Equal(kindsOf(group.Items), []assembly.ItemKind{assembly.ItemPDF, assembly.ItemBlank}) {
		t.Fatalf("group: %+v", group)
	}

	if group.Items[1].Blank.Count != 1 {
		t.Fatalf("an omitted count is 1, got %d", group.Items[1].Blank.Count)
	}

	err := job.Validate()
	if err != nil {
		t.Fatal(err)
	}
}

func TestFontBasesFollowTheDeclaringScope(t *testing.T) {
	t.Parallel()

	// The "dir" members come after the members that use them: bases are fixed once the document is read.
	job := mustDecode(t, `{"version":1,
	  "blank":{"text":{"font":{"file":"fonts/Root.ttf"}}},
	  "items":[
	    {"blank":{"text":{"font":{"file":"item.ttf"}}}},
	    {"blank":{"text":{"font":"default"}}},
	    {"items":[
	       {"blank":{"text":{"font":{"file":"inner.ttf"}}}},
	       {"dir":"deep","items":[{"blank":{"text":{"font":{"file":"/abs/Deep.ttf"}}}},{"blank":{"text":{"font":{"file":"deep.ttf"}}}}]}
	    ],"dir":"grp"}
	  ],
	  "dir":"src"}`)

	group := job.Items[2]
	deep := group.Items[1].Items
	fontOf := func(item *assembly.Item) assembly.Font { return item.Blank.Style.Text.Font.Value }

	for name, row := range map[string]struct{ got, want assembly.Font }{
		"default font (initial base, not dir)": {job.Defaults.Text.Font.Value, assembly.Font{File: "fonts/Root.ttf", Base: baseDir}},
		"root item font":                       {fontOf(&job.Items[0]), assembly.Font{File: "item.ttf", Base: "/base/src"}},
		"explicit default":                     {fontOf(&job.Items[1]), assembly.Font{}},
		"group font":                           {fontOf(&group.Items[0]), assembly.Font{File: "inner.ttf", Base: "/base/src/grp"}},
		"nested absolute":                      {fontOf(&deep[0]), assembly.Font{File: "/abs/Deep.ttf", Base: "/base/src/grp/deep"}},
		"nested group font":                    {fontOf(&deep[1]), assembly.Font{File: "deep.ttf", Base: "/base/src/grp/deep"}},
	} {
		if row.got != row.want {
			t.Errorf("%s: %+v, want %+v", name, row.got, row.want)
		}
	}
}

func TestFieldOriginsPointAtTheWinningValue(t *testing.T) {
	t.Parallel()

	document := `{"version":1,"blank":{"text":{"size":10}},"items":[{"blank":{"text":{"size":20}}},{"blank":{}}]}`
	job := mustDecode(t, document)

	own := job.Items[0].Blank.Style.Over(&job.Defaults)
	inherited := job.Items[1].Blank.Style.Over(&job.Defaults)

	for style, want := range map[*assembly.BlankStyle]string{&own: "20", &inherited: "10"} {
		origin := style.Text.Size.Origin
		if !strings.HasPrefix(document[origin.Offset:], want) {
			t.Errorf("origin offset %d is not at %s", origin.Offset, want)
		}
	}

	if got := assembly.Locate(job.Source, inherited.Text.Size.Origin, textSizePointer).Pointer; got != textSizePointer {
		t.Errorf("defaults pointer %q", got)
	}

	if got := assembly.Locate(job.Source, own.Text.Size.Origin, textSizePointer).Pointer; got != "/items/0/blank/text/size" {
		t.Errorf("item pointer %q", got)
	}
}
