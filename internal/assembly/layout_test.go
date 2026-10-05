// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

var (
	portrait  = assembly.PageDim{Width: 595, Height: 842}
	landscape = assembly.PageDim{Width: 842, Height: 595}
)

func documents() map[string]assembly.DocumentInfo {
	return map[string]assembly.DocumentInfo{
		"a.pdf": {Pages: 2, FirstPage: portrait, LastPage: portrait},
		"b.pdf": {Pages: 3, FirstPage: landscape, LastPage: landscape},
		"c.pdf": {Pages: 1, FirstPage: portrait, LastPage: portrait},
	}
}

func TestBuildLayoutAccountsForPagesAndCounts(t *testing.T) {
	t.Parallel()

	sequence := assembly.Sequence{Items: []assembly.Item{
		assembly.PDFItem("a.pdf"),
		assembly.BlankItem(assembly.BlankStyle{}, 1),
		assembly.PDFItem("b.pdf"),
		assembly.BlankItem(assembly.BlankStyle{}, 2),
		assembly.PDFItem("c.pdf"),
	}}

	layout, err := assembly.BuildLayout(sequence, assembly.BlankStyle{}, documents())
	if err != nil {
		t.Fatalf("BuildLayout() error = %v", err)
	}

	if layout.SourcePages != 6 || layout.BlankPages != 3 || layout.TotalPages != 9 {
		t.Fatalf("totals = %d source, %d blank, %d total", layout.SourcePages, layout.BlankPages, layout.TotalPages)
	}

	wantFirstPages := []int{1, 3, 4, 7, 9}
	for index, part := range layout.Parts {
		if part.FirstPage != wantFirstPages[index] {
			t.Errorf("part %d FirstPage = %d, want %d", index, part.FirstPage, wantFirstPages[index])
		}
	}
}

func TestBuildLayoutInheritsNeighboringPageSize(t *testing.T) {
	t.Parallel()

	sequence := assembly.Sequence{Items: []assembly.Item{
		assembly.BlankItem(assembly.BlankStyle{}, 1), // Leading: follows b.pdf's first page.
		assembly.PDFItem("b.pdf"),
		assembly.BlankItem(assembly.BlankStyle{}, 1), // Follows b.pdf's last page.
		assembly.PDFItem("a.pdf"),
		assembly.BlankItem(assembly.BlankStyle{Size: assembly.Some(assembly.PageSize{Dim: landscape})}, 1),
		assembly.BlankItem(assembly.BlankStyle{}, 1), // Still follows a.pdf, not the explicit blank.
	}}

	layout, err := assembly.BuildLayout(sequence, assembly.BlankStyle{}, documents())
	if err != nil {
		t.Fatalf("BuildLayout() error = %v", err)
	}

	want := map[int]assembly.PageDim{0: landscape, 2: landscape, 4: landscape, 5: portrait}
	for index, dim := range want {
		if got := layout.Parts[index].Blank.Dim; got != dim {
			t.Errorf("part %d blank size = %+v, want %+v", index, got, dim)
		}
	}
}

func TestBuildLayoutLayersItemStyleOverDefaults(t *testing.T) {
	t.Parallel()

	defaults := assembly.BlankStyle{
		Background: assembly.Some(assembly.Color{R: 1}),
		Text:       assembly.TextStyle{Value: assembly.Some("default"), Size: assembly.Some(assembly.Length(20))},
	}
	override := assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Some("")}}
	sequence := assembly.Sequence{Items: []assembly.Item{
		assembly.PDFItem("a.pdf"),
		assembly.BlankItem(assembly.BlankStyle{}, 1),
		assembly.BlankItem(override, 1),
	}}

	layout, err := assembly.BuildLayout(sequence, defaults, documents())
	if err != nil {
		t.Fatalf("BuildLayout() error = %v", err)
	}

	plain, cleared := layout.Parts[1].Blank, layout.Parts[2].Blank
	if plain.Text.Value != "default" || plain.Text.Size != 20 || !plain.Background.IsSet() {
		t.Errorf("default blank = %+v", plain)
	}

	if cleared.Text.Value != "" || cleared.Text.Size != 20 || !cleared.Background.IsSet() {
		t.Errorf("overridden blank = %+v", cleared)
	}

	if plain == cleared {
		t.Error("blanks with different text compare equal; they would be rendered once")
	}
}

func TestBuildLayoutRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	blank := assembly.BlankItem(assembly.BlankStyle{}, 1)

	tests := []struct {
		name     string
		sequence assembly.Sequence
		docs     map[string]assembly.DocumentInfo
		want     string
	}{
		{"empty", assembly.Sequence{}, documents(), "empty"},
		{"blank only", assembly.Sequence{Items: []assembly.Item{blank}}, documents(), "no PDF"},
		{"empty path", assembly.Sequence{Items: []assembly.Item{assembly.PDFItem("")}}, documents(), "empty PDF path"},
		{
			"zero count",
			assembly.Sequence{Items: []assembly.Item{assembly.PDFItem("a.pdf"), assembly.BlankItem(assembly.BlankStyle{}, 0)}},
			documents(),
			"count 0",
		},
		{"unknown kind", assembly.Sequence{Items: []assembly.Item{{}}}, documents(), "unknown kind"},
		{"missing info", assembly.Sequence{Items: []assembly.Item{assembly.PDFItem("x.pdf")}}, documents(), "missing document"},
		{
			"no pages",
			assembly.Sequence{Items: []assembly.Item{assembly.PDFItem("z.pdf")}},
			map[string]assembly.DocumentInfo{"z.pdf": {}},
			"no pages",
		},
		{
			"bad style",
			assembly.Sequence{
				Items: []assembly.Item{
					assembly.PDFItem("a.pdf"),
					assembly.BlankItem(assembly.BlankStyle{Text: assembly.TextStyle{Size: assembly.Some(assembly.Length(0))}}, 1),
				},
			},
			documents(),
			"text size",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := assembly.BuildLayout(test.sequence, assembly.BlankStyle{}, test.docs)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildLayout() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestSequenceHelpers(t *testing.T) {
	t.Parallel()

	sequence := assembly.Sequence{Items: []assembly.Item{
		assembly.PDFItem("a.pdf"),
		assembly.BlankItem(assembly.BlankStyle{}, 1),
		assembly.PDFItem("b.pdf"),
		assembly.PDFItem("a.pdf"),
	}}
	if got := strings.Join(sequence.DistinctPDFPaths(), ","); got != "a.pdf,b.pdf" {
		t.Errorf("DistinctPDFPaths() = %q", got)
	}

	if !sequence.HasBlank() || (assembly.Sequence{Items: []assembly.Item{assembly.PDFItem("a.pdf")}}).HasBlank() {
		t.Error("HasBlank() is wrong")
	}
}

func TestBlankStyleOverAndIsZero(t *testing.T) {
	t.Parallel()

	if !(assembly.BlankStyle{}).IsZero() {
		t.Error("empty style is not zero")
	}

	over := assembly.BlankStyle{Text: assembly.TextStyle{X: assembly.Some(assembly.Length(0))}}
	if over.IsZero() {
		t.Error("an explicitly set zero value must count as set")
	}

	base := assembly.BlankStyle{Text: assembly.TextStyle{X: assembly.Some(assembly.Length(5)), Y: assembly.Some(assembly.Length(7))}}

	merged := over.Over(base)
	if x := merged.Text.X.OrElse(-1); x != 0 {
		t.Errorf("explicit zero x lost: %v", x)
	}

	if y := merged.Text.Y.OrElse(-1); y != 7 {
		t.Errorf("base y lost: %v", y)
	}
}
