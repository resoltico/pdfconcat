// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/xml"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

type corpusWord struct {
	Text string  `xml:",chardata"`
	XMin float64 `xml:"xMin,attr"`
	YMin float64 `xml:"yMin,attr"`
	XMax float64 `xml:"xMax,attr"`
	YMax float64 `xml:"yMax,attr"`
}

type corpusBBoxPage struct {
	Width  float64      `xml:"width,attr"`
	Height float64      `xml:"height,attr"`
	Words  []corpusWord `xml:"word"`
}

func corpusTextGeometry(t *testing.T, tools oracle.Tools, path string) []corpusBBoxPage {
	t.Helper()
	raw, err := exec.CommandContext(t.Context(), tools.PDFToText, "-bbox", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	var html struct {
		Body struct {
			Doc struct {
				Pages []corpusBBoxPage `xml:"page"`
			} `xml:"doc"`
		} `xml:"body"`
	}
	if err = xml.Unmarshal(raw, &html); err != nil {
		t.Fatal(err)
	}
	if len(html.Body.Doc.Pages) == 0 {
		t.Fatal("bbox page discovery empty")
	}
	return html.Body.Doc.Pages
}
func corpusBBoxes(t *testing.T, tools oracle.Tools, path string) [][]corpusWord {
	t.Helper()
	var pages [][]corpusWord
	for _, page := range corpusTextGeometry(t, tools, path) {
		pages = append(pages, page.Words)
	}
	return pages
}
func corpusLines(words []corpusWord) [][]corpusWord {
	var lines [][]corpusWord
	for _, word := range words {
		if len(lines) == 0 || math.Abs(lines[len(lines)-1][0].YMin-word.YMin) > .1 {
			lines = append(lines, []corpusWord{word})
		} else {
			lines[len(lines)-1] = append(lines[len(lines)-1], word)
		}
	}
	return lines
}
func corpusStyledPage(value, canvas string, opts map[string]any) map[string]any {
	text := map[string]any{"value": value}
	for k, v := range opts {
		text[k] = v
	}
	return map[string]any{"page": map[string]any{"size": canvas, "text": text}}
}
func nearCorpus(a, b, tolerance float64) bool { return math.Abs(a-b) <= tolerance }
func TestCorpusTypography(t *testing.T) {
	t.Run("nine-anchors", func(t *testing.T) {
		anchors := []string{"top-left", "top", "top-right", "left", "center", "right", "bottom-left", "bottom", "bottom-right"}
		bounds := [][4]float64{{24, 240.132, 174, 276}, {125, 240.132, 275, 276}, {226, 240.132, 376, 276}, {24, 132.066, 174, 167.934}, {125, 132.066, 275, 167.934}, {226, 132.066, 376, 167.934}, {24, 24, 174, 59.868}, {125, 24, 275, 59.868}, {226, 24, 376, 59.868}}
		var items []any
		for n, anchor := range anchors {
			item := corpusStyledPage("Anchor: "+anchor+"\nĀ ļ ķ Ελληνικά", "400x300pt", map[string]any{"anchor": anchor, "x": []int{24, 0, -24}[n%3], "y": []int{-24, 0, 24}[n/3], "width": 150, "align": "left", "size": 14, "color": "#17324D"})
			page, ok := item["page"].(map[string]any)
			if !ok {
				t.Fatal("generated specimen lacks page object")
			}
			page["background"] = "#F4EFE6"
			items = append(items, item)
		}
		_, r := corpusRecipe(t, t.ArtifactDir(), map[string]any{"items": items})
		for n, p := range r.Parts {
			if p.Text == nil {
				t.Fatal("missing placement")
			}
			for k, want := range bounds[n] {
				if !nearCorpus(p.Text.Bounds[k], want, .002) {
					t.Fatal("reviewed anchor bound", n, p.Text.Bounds, bounds[n])
				}
			}
		}
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			texts, err := doc.RawTexts()
			if err != nil {
				t.Fatal(err)
			}
			for n, anchor := range anchors {
				if !strings.Contains(texts[n], "Anchor: "+anchor) {
					t.Fatal(texts[n])
				}
				pixel, err := doc.PixelColor(n+1, 2, 2)
				if err != nil || pixel != [3]uint8{244, 239, 230} {
					t.Fatal(pixel, err)
				}
			}
		})
	})
	t.Run("units-offsets-alignment", func(t *testing.T) {
		items := []any{corpusStyledPage("Moving block", "400x300pt", map[string]any{"size": 16, "width": 200}), corpusStyledPage("Moving block", "400x300pt", map[string]any{"size": 16, "width": 200, "x": "12mm", "y": "-.5in"})}
		for _, align := range []string{"left", "center", "right"} {
			items = append(items, corpusStyledPage("Short\nA much longer line", "400x300pt", map[string]any{"width": 220, "size": 14, "align": align}))
		}
		for _, size := range []string{"210x148mm", "8.5x5.5in", "20x15cm", "400x300pt"} {
			items = append(items, corpusStyledPage("UNITS", size, map[string]any{"width": 200}))
		}
		_, r := corpusRecipe(t, t.ArtifactDir(), map[string]any{"items": items})
		a, b := r.Parts[0].Text.Bounds, r.Parts[1].Text.Bounds
		if !nearCorpus(b[0]-a[0], 12*72/25.4, .002) || !nearCorpus(b[1]-a[1], -36, .002) {
			t.Fatal("physical offsets", a, b)
		}
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			boxes := corpusBBoxes(t, tools, r.Output.Path)
			if !nearCorpus(boxes[1][0].XMin-boxes[0][0].XMin, 12*72/25.4, .1) || !nearCorpus(boxes[1][0].YMin-boxes[0][0].YMin, 36, .1) {
				t.Fatal("painted physical offset")
			}
			for index, align := range []string{"left", "center", "right"} {
				lines := corpusLines(boxes[index+2])
				if len(lines) != 2 {
					t.Fatal(lines)
				}
				first, last := lines[0], lines[1]
				x, y := first[0].XMin, last[0].XMin
				if align == "right" {
					x = first[len(first)-1].XMax
					y = last[len(last)-1].XMax
				}
				if align == "center" {
					x = (first[0].XMin + first[len(first)-1].XMax) / 2
					y = (last[0].XMin + last[len(last)-1].XMax) / 2
				}
				if !nearCorpus(x, y, 1.5) {
					t.Fatal("painted fixed block alignment", align, x, y)
				}
			}
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			for n, size := range [][2]int{{596, 420}, {612, 396}, {567, 426}, {400, 300}} {
				w, h, err := doc.RenderedSize(n + 6)
				if err != nil || math.Abs(float64(w-size[0])) > 1 || math.Abs(float64(h-size[1])) > 1 {
					t.Fatal("unit canvas", n, w, h, err)
				}
			}
		})
	})
	t.Run("lines-wrap-leading-newlines-whitespace", func(t *testing.T) {
		words := "One two three four five six seven eight nine ten eleven twelve thirteen fourteen."
		items := []any{corpusStyledPage(words, "400x300pt", map[string]any{"size": 12, "width": 100, "anchor": "top-left", "x": 24, "y": -24}), corpusStyledPage(words, "400x300pt", map[string]any{"size": 12, "width": 250, "anchor": "top-left", "x": 24, "y": -24})}
		for _, leading := range []int{1, 2} {
			items = append(items, corpusStyledPage("Line one\nLine two\nLine three", "400x300pt", map[string]any{"size": 14, "width": 220, "leading": leading}))
		}
		for _, value := range []string{"First\r\n\r\nSecond\rThird\n", "First\r\n\r\nSecond\rThird"} {
			items = append(items, corpusStyledPage(value, "400x300pt", map[string]any{"size": 14, "width": 250, "leading": 1.3}))
		}
		for n, value := range []string{"Token", "  Token", "Tail", "Tail    ", "First\nLast", "First\n   \nLast"} {
			align := "left"
			if n == 2 || n == 3 {
				align = "right"
			}
			items = append(items, corpusStyledPage(value, "400x300pt", map[string]any{"size": 12, "width": 250, "anchor": "top-left", "x": 24, "y": -24, "leading": 1.4, "align": align}))
		}
		_, r := corpusRecipe(t, t.ArtifactDir(), map[string]any{"items": items})
		if r.Parts[4].Text.Bounds != r.Parts[5].Text.Bounds {
			t.Fatal("final newline added paragraph")
		}
		height := func(n int) float64 { return r.Parts[n].Text.Bounds[3] - r.Parts[n].Text.Bounds[1] }
		if !nearCorpus(height(11)-height(10), 16.8, .05) {
			t.Fatal("blank middle line lost")
		}
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			pages := corpusBBoxes(t, tools, r.Output.Path)
			if len(corpusLines(pages[0])) <= len(corpusLines(pages[1])) {
				t.Fatal("wrap-width did not affect painted lines")
			}
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			texts, err := doc.RawTexts()
			if err != nil {
				t.Fatal(err)
			}
			for n := range 2 {
				if strings.Join(strings.Fields(texts[n]), " ") != words {
					t.Fatal("word order changed", texts[n])
				}
			}
			for n, want := range []float64{14, 28} {
				lines := corpusLines(pages[n+2])
				if len(lines) != 3 {
					t.Fatal(lines)
				}
				for k := 1; k < 3; k++ {
					if !nearCorpus(lines[k][0].YMin-lines[k-1][0].YMin, want, .05) {
						t.Fatal("painted leading")
					}
				}
			}
			lines := corpusLines(pages[4])
			if len(lines) != 3 || !nearCorpus(lines[1][0].YMin-lines[0][0].YMin, 36.4, .1) || !nearCorpus(lines[2][0].YMin-lines[1][0].YMin, 18.2, .1) {
				t.Fatal("CR/blank-line geometry", lines)
			}
			if pages[7][0].XMin <= pages[6][0].XMin+3 || !nearCorpus(pages[8][0].XMin, pages[9][0].XMin, .05) {
				t.Fatal("leading/trailing space placement")
			}
		})
	})
	t.Run("layering-palette-decomposition", func(t *testing.T) {
		value := "ā ļ Ā ļ ķ Ελληνικά Кириллица"
		plan := map[string]any{"page": map[string]any{"size": "400x300pt", "background": "#112233", "text": map[string]any{"value": "INHERITED", "size": 18, "color": "#FFFFFF", "width": 250}}, "items": []any{map[string]any{"page": map[string]any{}}, map[string]any{"page": map[string]any{"background": "none", "text": map[string]any{"value": "", "font": "default"}}}, map[string]any{"page": map[string]any{"background": "none", "text": map[string]any{"value": "OVERRIDE", "size": 12, "color": "#000000"}}}, corpusStyledPage(value, "400x300pt", map[string]any{"width": 330, "size": 20})}}
		_, r := corpusRecipe(t, t.ArtifactDir(), plan)
		if r.Parts[1].Text != nil || r.Parts[0].Text.FontSize != 18 || r.Parts[2].Text.FontSize != 12 {
			t.Fatal("style clearing facts")
		}
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			texts, err := doc.RawTexts()
			if err != nil || strings.TrimSpace(texts[0]) != "INHERITED" || strings.TrimSpace(texts[1]) != "" || strings.TrimSpace(texts[2]) != "OVERRIDE" || strings.Trim(texts[3], "\n\f") != value {
				t.Fatal(texts, err)
			}
			for n, want := range [][3]uint8{{17, 34, 51}, {255, 255, 255}, {255, 255, 255}} {
				pixel, err := doc.PixelColor(n+1, 2, 2)
				if err != nil || pixel != want {
					t.Fatal(pixel, want, err)
				}
			}
			colors, err := doc.ColorCounts(1, [][3]uint8{{255, 255, 255}})
			if err != nil || colors[0] < 20 {
				t.Fatal("foreground not painted", colors, err)
			}
		})
	})
	t.Run("overflow-and-authoring-before-fit", func(t *testing.T) {
		for _, tc := range []struct {
			id, value, size string
			width           int
			allow, fit      bool
			want            string
		}{{"offpage-error", "OVERFLOW", "100x100pt", 80, false, false, "text_overflow"}, {"offpage-allow", "OVERFLOW", "100x100pt", 80, true, false, "text_overflow_allowed"}, {"nbsp-error", "Non\u00a0breaking", "400x300pt", 60, false, false, "text_overflow"}, {"nbsp-allow", "Non\u00a0breaking", "400x300pt", 60, true, false, "text_overflow_allowed"}, {"fit-does-not-hide-overflow", "UnbreakableWordThatCannotFit", "100x100pt", 70, false, true, "text_overflow"}} {
			t.Run(tc.id, func(t *testing.T) {
				dir := t.ArtifactDir()
				text := map[string]any{"size": 12, "width": tc.width}
				if strings.HasPrefix(tc.id, "offpage") {
					text["x"] = 40
				}
				if tc.fit {
					text["size"] = 24
				}
				if tc.allow {
					text["overflow"] = "allow"
				}
				plan := map[string]any{"version": 3, "output": "packet.pdf", "items": []any{corpusStyledPage(tc.value, tc.size, text)}}
				if tc.fit {
					plan["fit_to"] = "A4"
				}
				path := filepath.Join(dir, "plan.json")
				corpusJSON(t, path, plan)
				for _, op := range []core.Operation{core.Check, core.Build} {
					r := core.Execute(t.Context(), core.NewRequest(op, core.FromPlan(core.FilePlan(path)), core.Options{WorkingDir: dir}))
					if !code(r, tc.want) {
						t.Fatal(r)
					}
					if tc.allow {
						if r.Status != "ok" || !r.Parts[0].Text.Overflow {
							t.Fatal(r)
						}
						if op == core.Build {
							corpusArtifact(t, r)
							t.Run("oracle", func(t *testing.T) {
								tools := oracle.RequireTools(t)
								doc, err := oracle.Load(tools, r.Output.Path)
								if err != nil {
									t.Fatal(err)
								}
								words, err := doc.RawTexts()
								want := strings.Fields(tc.value)[0]
								if strings.HasPrefix(tc.id, "offpage") {
									want = "OVER"
								}
								if err != nil || !strings.Contains(words[0], want) {
									t.Fatal(words, err)
								}
							})
						}
					} else if r.Status != "invalid" || r.Output.Published {
						t.Fatal(r)
					}
				}
			})
		}
	})
}

func TestCorpusJustificationPaletteAndFit(t *testing.T) {
	t.Run("justification", func(t *testing.T) {
		text := "One two three four five six seven eight nine ten eleven twelve thirteen fourteen."
		left := corpusStyledPage(text, "400x300pt", map[string]any{"width": 180, "size": 14, "align": "left", "anchor": "top-left", "x": 24, "y": -24})
		justified := corpusStyledPage(text, "400x300pt", map[string]any{"width": 180, "size": 14, "align": "justify", "anchor": "top-left", "x": 24, "y": -24})
		_, r := corpusRecipe(t, t.ArtifactDir(), map[string]any{"items": []any{left, justified}})
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			pages := corpusBBoxes(t, tools, r.Output.Path)
			a, b := corpusLines(pages[0]), corpusLines(pages[1])
			if len(a) != len(b) || len(a) < 2 {
				t.Fatal("line-break order changed", a, b)
			}
			for n, line := range b {
				if n == len(b)-1 {
					if !nearCorpus(line[len(line)-1].XMax, a[n][len(a[n])-1].XMax, .2) {
						t.Fatal("final line incorrectly justified")
					}
				} else if !nearCorpus(line[len(line)-1].XMax, 204, 1.5) {
					t.Fatal("nonfinal justification edge", line)
				}
			}
		})
	})
	t.Run("palette", func(t *testing.T) {
		var items []any
		for _, colors := range [][2]string{{"#FFFFFF", "#000000"}, {"#112233", "#FFFFFF"}, {"#FFEEAA", "#CC0033"}, {"#0A0", "#003366"}} {
			p := corpusStyledPage("COLOR", "300x200pt", map[string]any{"width": 200, "size": 32, "color": colors[1]})
			page, ok := p["page"].(map[string]any)
			if !ok {
				t.Fatal("palette specimen lacks page object")
			}
			page["background"] = colors[0]
			items = append(items, p)
		}
		_, r := corpusRecipe(t, t.ArtifactDir(), map[string]any{"items": items})
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			for n, pair := range [][2][3]uint8{{{255, 255, 255}, {0, 0, 0}}, {{17, 34, 51}, {255, 255, 255}}, {{255, 238, 170}, {204, 0, 51}}, {{0, 170, 0}, {0, 51, 102}}} {
				pixel, err := doc.PixelColor(n+1, 2, 2)
				if err != nil || pixel != pair[0] {
					t.Fatal("palette corner", pixel, pair, err)
				}
				count, err := doc.ColorCounts(n+1, [][3]uint8{pair[1]})
				if err != nil || count[0] <= 20 {
					t.Fatal("palette glyph paint", count, err)
				}
			}
		})
	})
	t.Run("explicit-inherited-fit", func(t *testing.T) {
		dir := t.ArtifactDir()
		if err := pdffixture.Plain("SOURCE").WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
			t.Fatal(err)
		}
		_, r := corpusRecipe(t, dir, map[string]any{"fit_to": "A4", "items": []any{corpusSource("source.pdf"), map[string]any{"page": map[string]any{"text": map[string]any{"value": "INHERITED", "size": 18}}}, corpusStyledPage("EXPLICIT", "400x300pt", map[string]any{"width": 250})}})
		if !nearCorpus(r.Parts[1].Size.Width, 595.27559, .002) || !nearCorpus(r.Parts[1].Size.Height, 841.88976, .002) || *r.Parts[2].Size != (core.Size{Width: 400, Height: 300}) {
			t.Fatal("authored versus inherited canvas", r.Parts)
		}
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			for page := 1; page <= 3; page++ {
				w, h, err := doc.RenderedSize(page)
				if err != nil || w != 596 || h != 842 {
					t.Fatal("final fitted sheet", w, h, err)
				}
			}
		})
	})
	t.Run("ordinary-space-wrap", func(t *testing.T) {
		_, r := corpusRecipe(t, t.ArtifactDir(), map[string]any{"items": []any{corpusStyledPage("Non breaking", "400x300pt", map[string]any{"width": 60, "size": 12})}})
		if r.Parts[0].Text.Overflow {
			t.Fatal("ordinary spaces failed to wrap")
		}
	})
}
