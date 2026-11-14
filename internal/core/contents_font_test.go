// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestContentsInheritedFontsInSerializedPages(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, tc := range []struct{ name, file, family, xmlFamily, kind string }{
		{"bundled", "", "NotoSans-Regular", "NotoSans", "CID TrueType"},
		{"truetype", "testdata/corpus/fonts/Go-Mono.ttf", "GoMono", "GoMono", "CID TrueType"},
		{"cff", "testdata/fonts/source-serif/SourceSerif4-Regular.otf", "SourceSerif4-Regular", "SourceSerif4", "CID Type 0C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := pdffixture.Plain("Source text").WriteFile(filepath.Join(dir, "nested", "source.pdf")); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"value": "affine body"}
			text := map[string]any{"size": 24, "color": "#bd0000", "leading": 2, "anchor": "center", "value": "NOT CONTENTS"}
			if tc.file != "" {
				data, err := os.ReadFile(tc.file)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(filepath.Join(dir, "fonts"), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "fonts", "chosen.data"), data, 0600); err != nil {
					t.Fatal(err)
				}
				text["font"] = map[string]any{"file": "fonts/chosen.data"}
				if err = os.WriteFile(filepath.Join(dir, "fonts", "alias.data"), data, 0600); err != nil {
					t.Fatal(err)
				}
				body["font"] = map[string]any{"file": "../fonts/alias.data"}
			}
			plan := map[string]any{"version": 5, "page_defaults": map[string]any{"size": "A4", "text": text}, "page_numbers": map[string]any{"mode": "consecutive", "start": 9, "kinds": []string{"page"}}, "items": []any{
				map[string]any{"numbering": false, "page": map[string]any{"text": map[string]any{"value": "office cover"}}},
				map[string]any{"dir": "nested", "items": []any{
					map[string]any{"contents": map[string]any{"title": "Contents office"}, "numbering": false},
					map[string]any{"title": "office annex", "items": []any{map[string]any{"page": map[string]any{"text": body}}}},
					map[string]any{"title": "Source annex", "items": []any{map[string]any{"source": "source.pdf"}}},
				}},
			}}
			data, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanBytes{Data: data}, WorkingDir: dir, Output: "packet.pdf"})
			if r.Status != "ok" {
				t.Fatal(r.Diagnostics)
			}
			path := filepath.Join(dir, "packet.pdf")
			if err = tools.Check(path); err != nil {
				t.Fatal(err)
			}
			fonts, err := exec.CommandContext(t.Context(), tools.PDFFonts, "-f", "2", "-l", "2", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(fonts), tc.family) || !strings.Contains(string(fonts), tc.kind) {
				t.Fatalf("contents font: %s", fonts)
			}
			if tc.file != "" && strings.Contains(string(fonts), "NotoSans") {
				t.Fatalf("contents fell back to Noto: %s", fonts)
			}
			all, err := exec.CommandContext(t.Context(), tools.PDFFonts, path).Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(all), tc.family) != 1 {
				t.Fatalf("font resources not shared across cover/contents/body: %s", all)
			}
			if tc.file != "" {
				body, err := exec.CommandContext(t.Context(), tools.PDFFonts, "-f", "3", "-l", "3", path).Output()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(body), tc.family) || !strings.Contains(string(body), "NotoSans-Regular") {
					t.Fatalf("body/overlay fonts: %s", body)
				}
			}
			raw, err := exec.CommandContext(t.Context(), tools.PDFToHTML, "-xml", "-hidden", "-zoom", "1", "-stdout", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Pages []struct {
					Number int `xml:"number,attr"`
					Specs  []struct {
						ID     string `xml:"id,attr"`
						Family string `xml:"family,attr"`
						Color  string `xml:"color,attr"`
						Size   int    `xml:"size,attr"`
					} `xml:"fontspec"`
					Texts []struct {
						Font  string `xml:"font,attr"`
						Inner string `xml:",innerxml"`
					} `xml:"text"`
				} `xml:"page"`
			}
			if err = xml.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			type spec struct {
				family, color string
				size          int
			}
			specs := map[string]spec{}
			for _, page := range doc.Pages {
				for _, f := range page.Specs {
					specs[f.ID] = spec{f.Family, f.Color, f.Size}
				}
			}
			var rows []string
			for _, page := range doc.Pages {
				if page.Number != 2 {
					continue
				}
				for _, v := range page.Texts {
					decoder := xml.NewDecoder(strings.NewReader("<root>" + v.Inner + "</root>"))
					var label strings.Builder
					for {
						tok, e := decoder.Token()
						if errors.Is(e, io.EOF) {
							break
						}
						if e != nil {
							t.Fatal(e)
						}
						if chars, ok := tok.(xml.CharData); ok {
							label.Write(chars)
						}
					}
					value := strings.ReplaceAll(strings.TrimSpace(label.String()), "ﬃ", "ffi")
					if value == "" {
						continue
					}
					rows = append(rows, value)
					s := specs[v.Font]
					if !strings.Contains(s.family, tc.xmlFamily) || s.color != "#000000" {
						t.Fatalf("text %q uses %+v", value, s)
					}
					size := 12
					if strings.HasPrefix(value, "Contents") {
						size = 18
					}
					if s.size != size {
						t.Fatalf("contents inherited unrelated text size: %+v", s)
					}
				}
			}
			if !slices.Equal(rows, []string{"Contents office", "office annex", "9", "Source annex", "—"}) {
				t.Fatalf("physical link reading order/labels: %q", rows)
			}
			extracted, err := exec.CommandContext(t.Context(), tools.PDFToText, "-f", "2", "-l", "2", path, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(extracted), "office annex") || !strings.Contains(string(extracted), "—") || strings.Contains(string(extracted), "NOT CONTENTS") {
				t.Fatalf("extraction/ActualText: %s", extracted)
			}
		})
	}
}

func TestContentsInheritedFontDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, file, title, code string }{
		{"missing", "absent.data", "Contents", "font_input_invalid"},
		{"invalid", "invalid.data", "Contents", "font_invalid"},
		{"directory", "directory.data", "Contents", "font_input_invalid"},
		{"denied", "denied.data", "Contents", "font_input_invalid"},
		{"glyph", "font.data", "Contents ẞ", "text_glyph_missing"},
		{"glyph-row", "font.data", "Contents", "text_glyph_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			font := filepath.Join(dir, tc.file)
			if tc.name == "invalid" {
				if err := os.WriteFile(font, []byte("not a font"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "directory" {
				if err := os.Mkdir(font, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(tc.name, "glyph") || tc.name == "denied" {
				d, e := os.ReadFile("testdata/corpus/fonts/Go-Mono.ttf")
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(font, d, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if tc.name == "denied" {
				if e := os.Chmod(font, 0); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { _ = os.Chmod(font, 0600) })
				f, e := os.Open(font)
				if e == nil {
					_ = f.Close()
					t.Skip("genuine font permission denial is unavailable")
				}
				if !os.IsPermission(e) {
					t.Fatal(e)
				}
			}
			title := "Annex"
			if tc.name == "glyph-row" {
				title = "Annex ẞ"
			}

			page := map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"font": "default"}}}
			items := []any{map[string]any{"contents": map[string]any{"title": tc.title}}, map[string]any{"title": title, "items": []any{page}}}
			plan := map[string]any{"version": 5, "page_defaults": map[string]any{"text": map[string]any{"font": map[string]any{"file": tc.file}}}, "items": items}

			data, e := json.Marshal(plan)
			if e != nil {
				t.Fatal(e)
			}
			for _, op := range []core.Operation{core.Check, core.Build} {
				r := core.Execute(t.Context(), core.Request{Operation: op, Input: core.PlanBytes{Data: data}, WorkingDir: dir, Output: "out.pdf"})
				if r.Status != "invalid" || len(r.Diagnostics) != 1 {
					t.Fatalf("%v: %+v", op, r.Diagnostics)
				}
				d := r.Diagnostics[0]
				if tc.name == "glyph-row" && !slices.Contains(d.Items, "/items/1/title") {
					t.Fatalf("missing row declaration: %+v", d)
				}
				if d.Code.String() != tc.code || d.File != font || !slices.Contains(d.Items, "/page_defaults/text/font") || !slices.Contains(d.Items, "/items/0") {
					t.Fatalf("font provenance: %+v", d)
				}
				if _, e = os.Stat(filepath.Join(dir, "out.pdf")); !os.IsNotExist(e) {
					t.Fatalf("invalid font output: %v", e)
				}
			}
			plan["items"] = items[1:]
			without, e := json.Marshal(plan)
			if e != nil {
				t.Fatal(e)
			}
			unused := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanBytes{Data: without}, WorkingDir: dir})
			if unused.Status != "ok" {
				t.Fatalf("unused root font was inspected without contents: %+v", unused.Diagnostics)
			}

		})
	}
}
