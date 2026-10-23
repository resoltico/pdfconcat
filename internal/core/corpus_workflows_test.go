// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func corpusGenerated(count int, size, text string) map[string]any {
	return map[string]any{"count": count, "page": map[string]any{"size": size, "text": map[string]any{"value": text}}}
}
func corpusSection(title string, items ...any) map[string]any {
	return map[string]any{"title": title, "items": items}
}
func corpusRecipe(t *testing.T, dir string, plan map[string]any) (core.Result, core.Result) {
	t.Helper()
	plan["version"] = 3
	plan["output"] = "packet.pdf"
	path := filepath.Join(dir, "plan.json")
	corpusJSON(t, path, plan)
	check := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromPlan(core.FilePlan(path)), core.Options{WorkingDir: dir, Report: filepath.Join(dir, "check.json")}))
	if check.Status != "ok" || check.Output.Published {
		t.Fatal(check)
	}
	build := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromPlan(core.FilePlan(path)), core.Options{WorkingDir: dir, Report: filepath.Join(dir, "build.json")}))
	if build.Status != "ok" {
		t.Fatal(build)
	}
	corpusArtifact(t, build)
	if !reflect.DeepEqual(check.Pages, build.Pages) || !reflect.DeepEqual(check.Sections, build.Sections) {
		t.Fatal("check/build physical facts disagree")
	}
	for n, p := range check.Parts {
		if p.Kind != build.Parts[n].Kind || !reflect.DeepEqual(p.OutputPages, build.Parts[n].OutputPages) || !reflect.DeepEqual(p.Spans, build.Parts[n].Spans) {
			t.Fatal("check/build occurrence mapping disagrees")
		}
	}
	return check, build
}

type corpusWorkflow struct {
	ID      string
	Plan    map[string]any
	Pages   int
	Markers []string
	Targets []int64
}

func corpusWorkflows(t *testing.T, cli string) {
	title := "Ā́ Pārskats — Ελληνικά Кириллица"
	odd := corpusSection("Child", corpusSource("book.pdf", [2]int{2, 2}))
	odd["start_on"] = "odd"
	duplicateA, duplicateB := corpusSection("Evidence", corpusSource("book.pdf", [2]int{1, 1})), corpusSection("Evidence", corpusSource("book.pdf", [2]int{12, 12}))
	nested := corpusSection("Parent", corpusSection("Evidence", corpusSource("outlined.pdf")), corpusSection("Appendix", corpusSource("book.pdf", [2]int{12, 12})))
	nested["divider"] = map[string]any{}
	painted := corpusSection(title, corpusSource("book.pdf", [2]int{6, 6}))
	painted["divider"] = map[string]any{}
	childOdd := corpusSection("Inner", corpusSource("book.pdf", [2]int{1, 1}))
	childOdd["start_on"] = "odd"
	parentOdd := corpusSection("Outer", childOdd)
	parentOdd["start_on"] = "odd"
	redBody := corpusSection("Printed section", corpusSource("book.pdf", [2]int{1, 2}))
	redBody["start_on"] = "odd"
	redBody["divider"] = map[string]any{}
	physicalBody := corpusSection("Odd body", corpusSource("book.pdf", [2]int{2, 2}))
	physicalBody["start_on"] = "odd"
	cases := []corpusWorkflow{
		{"coalesced-odd-boundaries", map[string]any{"items": []any{corpusSource("plain.pdf"), map[string]any{"start_on": "odd", "items": []any{parentOdd}}, corpusGenerated(1, "105x148mm", "")}}, 4, nil, []int64{3, 3}},
		{"padding-clears-style", map[string]any{"page": map[string]any{"size": "A4", "background": "#FF0000", "text": map[string]any{"value": "DEFAULT"}}, "page_numbers": map[string]any{}, "items": []any{corpusSource("plain.pdf"), redBody, map[string]any{"page": map[string]any{}}}}, 6, nil, []int64{3}},
		{"padding-physical-versus-source", map[string]any{"items": []any{corpusSource("book.pdf", [2]int{1, 1}), corpusGenerated(1, "210x100pt", ""), corpusGenerated(1, "150x250pt", ""), physicalBody, map[string]any{"page": map[string]any{}}}}, 6, nil, []int64{5}},
		{"selected-disjoint", map[string]any{"items": []any{corpusSource("book.pdf", [2]int{2, 4}, [2]int{7, 7}, [2]int{12, 12})}}, 5, []string{"BOOK p2", "BOOK p3", "BOOK p4", "BOOK p7", "BOOK p12"}, nil},
		{"selected-reordered-occurrences", map[string]any{"items": []any{corpusSource("book.pdf", [2]int{10, 12}), corpusSource("book.pdf", [2]int{1, 1}), corpusSource("book.pdf", [2]int{5, 6})}}, 6, []string{"BOOK p10", "BOOK p11", "BOOK p12", "BOOK p1", "BOOK p5", "BOOK p6"}, nil},
		{"selected-adjacency", map[string]any{"items": []any{corpusSource("book.pdf", [2]int{1, 2}, [2]int{3, 3}, [2]int{4, 5})}}, 5, nil, nil},
		{"nested-native-outlines", map[string]any{"page": map[string]any{"size": "A4"}, "items": []any{corpusSource("plain.pdf"), nested, map[string]any{"contents": map[string]any{}}}}, 6, []string{"PLAIN", "Parent", "OUTLINED p1", "OUTLINED p2", "BOOK p12", "Contents"}, []int64{2, 3, 5}},
		{"duplicate-section-titles", map[string]any{"items": []any{map[string]any{"contents": map[string]any{}}, duplicateA, duplicateB}}, 3, nil, []int64{2, 3}},
		{"parent-range-before-target", map[string]any{"page_numbers": map[string]any{}, "items": []any{corpusSource("plain.pdf"), corpusSection("Parent", odd), map[string]any{"contents": map[string]any{}}}}, 4, nil, []int64{3, 3}},
		{"contents-inside-own-section", map[string]any{"items": []any{corpusSection("Volume", map[string]any{"contents": map[string]any{}}, corpusSource("book.pdf", [2]int{5, 5}), corpusSection("Child", corpusSource("book.pdf", [2]int{6, 6})))}}, 3, nil, []int64{1, 3}},
		{"contents-middle", map[string]any{"items": []any{corpusSection("Before", corpusSource("book.pdf", [2]int{1, 1})), map[string]any{"contents": map[string]any{}}, corpusSection("After", corpusSource("book.pdf", [2]int{2, 2}))}}, 3, nil, []int64{1, 3}},
		{"contents-end", map[string]any{"items": []any{corpusSection("Body", corpusSource("book.pdf", [2]int{8, 9})), map[string]any{"contents": map[string]any{"title": "Rear index"}}}}, 3, nil, []int64{1}},
		{"contents-absent", map[string]any{"items": []any{corpusSource("book.pdf", [2]int{3, 3})}}, 1, []string{"BOOK p3"}, []int64{}},
		{"contents-digits9", map[string]any{"page_numbers": map[string]any{}, "items": []any{corpusSource("book.pdf", [2]int{1, 8}), map[string]any{"contents": map[string]any{}}, corpusSection("First double-digit target", corpusSource("book.pdf", [2]int{9, 9})), corpusSection("Next target", corpusSource("book.pdf", [2]int{10, 10}))}}, 11, nil, []int64{10, 11}},
		{"contents-digits99", map[string]any{"page_numbers": map[string]any{}, "items": []any{corpusGenerated(98, "A4", ""), map[string]any{"contents": map[string]any{}}, corpusSection("One hundred", corpusSource("book.pdf", [2]int{1, 1})), corpusSection("One hundred one", corpusSource("book.pdf", [2]int{12, 12}))}}, 101, nil, []int64{100, 101}},
		{"title-unpainted-script", map[string]any{"items": []any{corpusSection("資料 📎", corpusSource("book.pdf", [2]int{4, 4}))}}, 1, []string{"BOOK p4"}, []int64{1}},
		{"title-painted-decomposed", map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"size": 18}}, "page_numbers": map[string]any{}, "items": []any{map[string]any{"contents": map[string]any{}}, painted}}, 3, nil, []int64{2}},
		{"generated-only", map[string]any{"page": map[string]any{"size": "A4"}, "page_numbers": map[string]any{}, "items": []any{corpusGenerated(1, "200x300pt", "COVER"), map[string]any{"contents": map[string]any{}}, map[string]any{"title": "Cards", "start_on": "odd", "divider": map[string]any{}, "items": []any{corpusGenerated(2, "150x250pt", ""), map[string]any{"title": "End", "start_on": "odd", "items": []any{corpusGenerated(1, "300x200pt", "END")}}}}}}, 7, []string{"COVER", "Contents", "Cards", "", "", "", "END"}, []int64{3, 7}},
		{"native-selected-outlines", map[string]any{"items": []any{corpusSection("One", corpusSource("outlined.pdf", [2]int{1, 1})), corpusSection("Two", corpusSource("outlined.pdf", [2]int{2, 2})), map[string]any{"contents": map[string]any{}}}}, 3, nil, []int64{1, 2}},
		{"named-repeated", map[string]any{"items": []any{corpusSource("named.pdf"), corpusSource("named.pdf")}}, 4, nil, nil},
		{"legacy-selected", map[string]any{"items": []any{corpusSource("legacy.pdf", [2]int{1, 1}), corpusSource("legacy.pdf", [2]int{2, 2})}}, 2, nil, nil},
	}
	var matrix []corpusRecord
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			dir := t.ArtifactDir()
			for name, d := range map[string]*pdffixture.Doc{"book.pdf": pdffixture.Pages("BOOK", 12), "plain.pdf": pdffixture.Plain("PLAIN"), "outlined.pdf": pdffixture.Outlined("OUTLINED"), "named.pdf": pdffixture.NamedDests("NAMED"), "legacy.pdf": pdffixture.LegacyDests("LEGACY")} {
				if err := d.WriteFile(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			check, r := corpusRecipe(t, dir, tc.Plan)
			if tc.ID == "selected-reordered-occurrences" {
				for name, complete := range map[string]core.Result{"typed-check": check, "typed-build": r} {
					if err := corpusReorderedMappingError(complete, filepath.Join(dir, "book.pdf")); err != nil {
						t.Fatalf("%s: %v", name, err)
					}
				}
				for _, name := range []string{"check.json", "build.json"} {
					data, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					var saved core.Result
					if err = json.Unmarshal(data, &saved); err != nil {
						t.Fatal(err)
					}
					if err = corpusReorderedMappingError(saved, filepath.Join(dir, "book.pdf")); err != nil {
						t.Fatalf("actual saved %s: %v", name, err)
					}
				}
				bad := r
				bad.Parts = append([]core.Part(nil), r.Parts...)
				for n := range bad.Parts {
					spans := []core.Span{{Source: [2]int{1, 1}, Output: [2]int64{1, 1}}}
					bad.Parts[n].Spans = &spans
				}
				if err := corpusReorderedMappingError(bad, filepath.Join(dir, "book.pdf")); err == nil {
					t.Fatal("all-wrong occurrence maps satisfied the literal mapping witness")
				}
			}
			if *r.Pages.Total != int64(tc.Pages) {
				t.Fatalf("literal pages %d got %+v", tc.Pages, r.Pages)
			}
			if tc.Targets != nil {
				var targets []int64
				for _, s := range r.Sections {
					targets = append(targets, *s.TargetPage)
				}
				if !slices.Equal(targets, tc.Targets) {
					t.Fatalf("literal targets %v want %v", targets, tc.Targets)
				}
			}
			if tc.ID == "padding-physical-versus-source" {
				if *r.Parts[3].Size != (core.Size{Width: 150, Height: 250}) || *r.Parts[len(r.Parts)-1].Size != (core.Size{Width: 612, Height: 792}) {
					t.Fatal("physical padding/source inheritance conflated", r.Parts)
				}
			}
			if tc.ID == "selected-adjacency" {
				want := []core.Span{{Source: [2]int{1, 5}, Output: [2]int64{1, 5}}}
				if !reflect.DeepEqual(*r.Parts[0].Spans, want) {
					t.Fatal(r.Parts[0].Spans)
				}
			}
			if tc.ID == "parent-range-before-target" {
				if *r.Sections[0].OutputPages != [2]int64{2, 3} || *r.Sections[1].OutputPages != [2]int64{3, 3} {
					t.Fatal("parent range ownership", r.Sections)
				}
			}
			t.Run("oracle", func(t *testing.T) {
				tools := oracle.RequireTools(t)
				doc, err := oracle.Load(tools, r.Output.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err = tools.Check(r.Output.Path); err != nil || doc.PageCount() != tc.Pages {
					t.Fatal(err, doc.PageCount())
				}
				texts, err := doc.RawTexts()
				if err != nil {
					t.Fatal(err)
				}
				for n, marker := range tc.Markers {
					if !strings.Contains(texts[n], marker) {
						t.Fatalf("itinerary %d: %q lacks %q", n+1, texts[n], marker)
					}
				}
				for _, part := range r.Parts {
					if part.Kind == "padding" {
						page := int(part.OutputPages[0])
						if strings.TrimSpace(texts[page-1]) != "" {
							t.Fatal("padding has footer/text")
						}
						pixel, err := doc.PixelColor(page, 2, 2)
						if err != nil || pixel != [3]uint8{255, 255, 255} {
							t.Fatal("padding inherited fill", pixel, err)
						}
					}
					if part.Kind == "contents" {
						var links []int
						for page := part.OutputPages[0]; page <= part.OutputPages[1]; page++ {
							targets, err := doc.ActionTargets(int(page))
							if err != nil {
								t.Fatal(err)
							}
							links = append(links, targets...)
						}
						if len(links) != len(r.Sections) {
							t.Fatal("contents links missing")
						}
						for n, link := range links {
							if int64(link) != tc.Targets[n] {
								t.Fatal("literal contents target", links, tc.Targets)
							}
						}
					}
				}
				if tc.ID == "title-painted-decomposed" && !strings.Contains(texts[1], title) {
					t.Fatal("decomposed title normalized/lost", texts[1])
				}
				if tc.ID == "nested-native-outlines" {
					expected := []oracle.Outline{{Title: "Parent", Targets: []int{2}, Open: true, Children: []oracle.Outline{{Title: "Evidence", Targets: []int{3}, Open: true, Children: []oracle.Outline{{Title: "First", Targets: []int{3}}, {Title: "Second", Targets: []int{4}}}}, {Title: "Appendix", Targets: []int{5}}}}}
					if err = doc.CheckOutlineTree(expected); err != nil {
						t.Fatal(err)
					}
				}
				if tc.ID == "named-repeated" {
					for page, want := range [][]int{{2}, {1}, {4}, {3}} {
						targets, err := doc.ActionTargets(page + 1)
						if err != nil || !slices.Equal(targets, want) {
							t.Fatal(targets, want, err)
						}
					}
					if err = tools.CheckLinks(r.Output.Path); err != nil {
						t.Fatal(err)
					}
				}
				if tc.ID == "legacy-selected" {
					for page := 1; page <= 2; page++ {
						targets, err := doc.ActionTargets(page)
						if err != nil || len(targets) != 0 {
							t.Fatal(targets, err)
						}
					}
					if err = tools.CheckLinks(r.Output.Path); err != nil {
						t.Fatal(err)
					}
				}
				corpusJSON(t, filepath.Join(t.ArtifactDir(), "text.json"), texts)
			})
			matrix = append(matrix, corpusRecord{ID: tc.ID, Boundary: "core check/build + qpdf/Poppler", ArtifactDir: dir})
		})
	}
	t.Run("relocated-project", func(t *testing.T) {
		root := t.ArtifactDir()
		old := filepath.Join(root, "original")
		if err := os.MkdirAll(filepath.Join(old, "inputs/leaf"), 0700); err != nil {
			t.Fatal(err)
		}
		corpusCopy(t, "testdata/corpus/business-report.pdf", filepath.Join(old, "inputs/leaf/report.pdf"))
		corpusCopy(t, "internal/typeset/fontdata/NotoSans-Regular.ttf", filepath.Join(old, "inputs/fonts/title.ttf"))
		plan := map[string]any{"version": 3, "dir": "inputs", "output": "packet.pdf", "items": []any{map[string]any{"title": "Scoped Ā Ελληνικά", "dir": "leaf", "divider": map[string]any{"size": "A4", "text": map[string]any{"font": map[string]any{"file": "fonts/title.ttf"}}}, "items": []any{corpusSource("report.pdf", [2]int{2, 3})}}}}
		corpusJSON(t, filepath.Join(old, "plan.json"), plan)
		moved := filepath.Join(root, "Packet space Ā Ελληνικά")
		if err := os.Rename(old, moved); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("old project still exists")
		}
		cmd := exec.CommandContext(t.Context(), cli, "build", "--plan", filepath.Join(moved, "plan.json"), "--output", filepath.Join(moved, "override.pdf"), "--report", filepath.Join(moved, "report.json"))
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("relocated CLI: %s %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(moved, "packet.pdf")); !os.IsNotExist(err) {
			t.Fatal("saved output ignored CLI override")
		}
		data, err := os.ReadFile(filepath.Join(moved, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		var r core.Result
		if err = json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		corpusArtifact(t, r)
		if r.Parts[1].Path != filepath.Join(moved, "inputs/leaf/report.pdf") || r.Parts[0].Text.Font != filepath.Join(moved, "inputs/fonts/title.ttf") {
			t.Fatal("declaration/source scopes lost", r.Parts)
		}
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			texts, err := doc.RawTexts()
			if err != nil || !strings.Contains(texts[0], "Scoped Ā Ελληνικά") || !strings.Contains(texts[1], "Measured comparison") {
				t.Fatal(texts, err)
			}
		})
	})
	corpusJSON(t, filepath.Join(t.ArtifactDir(), "matrix.json"), matrix)
}

func TestCorpusContentsSpecimens(t *testing.T) {
	for _, tc := range []struct {
		ID, Prefix, Title, Size string
		Count                   int
		Fit                     bool
	}{{"multipage", "Exhibit", "Long packet index", "A4", 60, false}, {"pocket", "Unit", "Index", "220x240pt", 10, false}, {"fit-footer-band", "Entry", "Index", "A3", 60, true}} {
		t.Run(tc.ID, func(t *testing.T) {
			dir := t.ArtifactDir()
			if err := pdffixture.Pages("BOOK", 12).WriteFile(filepath.Join(dir, "book.pdf")); err != nil {
				t.Fatal(err)
			}
			items := []any{map[string]any{"contents": map[string]any{"title": tc.Title, "size": tc.Size}}}
			for n := range tc.Count {
				page := 1
				if tc.ID == "pocket" {
					page = n + 1
				}
				items = append(items, corpusSection(fmt.Sprintf("%s %02d", tc.Prefix, n+1), corpusSource("book.pdf", [2]int{page, page})))
			}
			plan := map[string]any{"items": items}
			if tc.Fit {
				plan["fit_to"] = "A5"
				plan["page_numbers"] = map[string]any{"margin": "50mm"}
			}
			_, r := corpusRecipe(t, dir, plan)
			if *r.Parts[0].Pages != 2 || *r.Pages.Total != int64(tc.Count+2) {
				t.Fatal("reviewed whole-row pagination changed", r.Pages, r.Parts[0])
			}
			for n, s := range r.Sections {
				if *s.TargetPage != int64(n+3) || *s.OutputPages != [2]int64{int64(n + 3), int64(n + 3)} {
					t.Fatal("literal multipage target", s)
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
				var links []int
				for page := 1; page <= 2; page++ {
					targets, err := doc.ActionTargets(page)
					if err != nil {
						t.Fatal(err)
					}
					links = append(links, targets...)
				}
				if len(links) != tc.Count {
					t.Fatal("TOC links lost", links)
				}
				for n, target := range links {
					if target != n+3 {
						t.Fatal("whole row target order", links)
					}
				}
				contents := texts[0] + texts[1]
				for n := range tc.Count {
					title := fmt.Sprintf("%s %02d", tc.Prefix, n+1)
					if strings.Count(contents, title) != 1 {
						t.Fatal("missing/split/duplicated whole row", title)
					}
				}
				boxes := corpusBBoxes(t, tools, r.Output.Path)
				for page := range 2 {
					words := boxes[page]
					if len(words) == 0 {
						t.Fatal("contents painted no words")
					}
					for _, w := range words {
						if w.XMin < 0 || w.YMin < 0 || w.XMax <= w.XMin || w.YMax <= w.YMin {
							t.Fatal("invalid painted row geometry", w)
						}
					}
					if tc.Fit {
						last := words[0]
						for _, w := range words {
							if w.YMax > last.YMax {
								last = w
							}
						}
						if last.Text != strconv.Itoa(page+1) {
							t.Fatal("footer not in reserved band", last)
						}
						for _, w := range words {
							if w.YMax < last.YMax && !nearCorpus(w.YMax, last.YMax, .01) && w.YMax > last.YMin-4 {
								t.Fatal("contents crosses footer exclusion", w, last)
							}
						}
					}
				}
			})
		})
	}
}
func corpusBlankNeighbors(t *testing.T, cli string) {
	dir := corpusStage(t, "business-report.pdf")
	report := filepath.Join(dir, "report.json")
	cmd := exec.CommandContext(t.Context(), cli, "build", "--blank", filepath.Join(dir, "business-report.pdf"), "--blank", "-o", filepath.Join(dir, "packet.pdf"), "--report", report)
	cmd.Dir = dir
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("direct blank neighbors %s %v", raw, err)
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var r core.Result
	if err = json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Parts) != 3 || *r.Pages.Total != 5 || r.Parts[0].Kind != "page" || r.Parts[1].Kind != "source" || r.Parts[2].Kind != "page" {
		t.Fatal(r)
	}
	corpusArtifact(t, r)
	t.Run("oracle", func(t *testing.T) {
		tools := oracle.RequireTools(t)
		doc, err := oracle.Load(tools, r.Output.Path)
		if err != nil {
			t.Fatal(err)
		}
		text, err := doc.RawTexts()
		if err != nil || strings.TrimSpace(text[0]) != "" || strings.TrimSpace(text[4]) != "" || !strings.Contains(text[2], "Measured comparison") {
			t.Fatal(text, err)
		}
		for _, page := range []int{1, 5} {
			w, h, err := doc.RenderedSize(page)
			if err != nil || w != 596 || h != 842 {
				t.Fatal("neighbor inherited physical sheet", page, w, h, err)
			}
		}
	})
}

func TestCorpusSourceRefresh(t *testing.T) {
	dir := t.ArtifactDir()
	if err := pdffixture.Pages("BOOK", 12).WriteFile(filepath.Join(dir, "book.pdf")); err != nil {
		t.Fatal(err)
	}
	closing := corpusSection("Closing", corpusSource("book.pdf", [2]int{12, 12}))
	closing["start_on"] = "odd"
	path := filepath.Join(dir, "plan.json")
	corpusJSON(t, path, map[string]any{"version": 3, "output": "ignored.pdf", "items": []any{map[string]any{"contents": map[string]any{}}, corpusSection("Opening", corpusSource("evolving.pdf")), closing}})
	document, ds := core.ReadPlan(t.Context(), core.FilePlan(path), dir)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	hashes := map[string]bool{}
	for _, tc := range []struct{ Pages, Total, Padding, Closing int }{{2, 5, 4, 5}, {3, 5, 0, 5}, {4, 7, 6, 7}} {
		t.Run(strconv.Itoa(tc.Pages), func(t *testing.T) {
			if err := pdffixture.Pages("EVOLVING", tc.Pages).WriteFile(filepath.Join(dir, "evolving.pdf")); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, fmt.Sprintf("version-%d.pdf", tc.Pages))
			r := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromDocument(document), core.Options{WorkingDir: dir, Output: output, Report: filepath.Join(dir, fmt.Sprintf("version-%d.json", tc.Pages))}))
			if r.Status != "ok" || *r.Pages.Total != int64(tc.Total) || *r.Sections[0].TargetPage != 2 || *r.Sections[1].TargetPage != int64(tc.Closing) {
				t.Fatal(r)
			}
			corpusArtifact(t, r)
			hashes[r.Parts[1].SHA256] = true
			var padding []int
			for _, part := range r.Parts {
				if part.Kind == "padding" {
					padding = append(padding, int(part.OutputPages[0]))
				}
			}
			if tc.Padding == 0 && len(padding) != 0 || tc.Padding != 0 && !slices.Equal(padding, []int{tc.Padding}) {
				t.Fatal("source refresh parity", padding)
			}
			t.Run("oracle", func(t *testing.T) {
				tools := oracle.RequireTools(t)
				doc, err := oracle.Load(tools, output)
				if err != nil {
					t.Fatal(err)
				}
				texts, err := doc.RawTexts()
				if err != nil || len(texts) != tc.Total || !strings.Contains(texts[tc.Closing-1], "BOOK p12") {
					t.Fatal(texts, err)
				}
				links, err := doc.ActionTargets(1)
				if err != nil || !slices.Equal(links, []int{2, tc.Closing}) {
					t.Fatal(links, err)
				}
			})
		})
	}
	if len(hashes) != 3 {
		t.Fatal("source refresh reused whole-source identity")
	}
}

// These are authored occurrence expectations, not an expansion of reported spans
// or an equality check between phases. Physical marker order is checked separately.
func corpusReorderedMappingError(result core.Result, source string) error {
	expected := []struct {
		Item   string
		Pages  int64
		Output [2]int64
		Span   core.Span
	}{
		{"/items/0", 3, [2]int64{1, 3}, core.Span{Source: [2]int{10, 12}, Output: [2]int64{1, 3}}},
		{"/items/1", 1, [2]int64{4, 4}, core.Span{Source: [2]int{1, 1}, Output: [2]int64{4, 4}}},
		{"/items/2", 2, [2]int64{5, 6}, core.Span{Source: [2]int{5, 6}, Output: [2]int64{5, 6}}},
	}
	if len(result.Parts) != len(expected) {
		return fmt.Errorf("literal occurrence count: got %d", len(result.Parts))
	}
	for n, want := range expected {
		part := result.Parts[n]
		if part.Item != want.Item || part.Kind != "source" || part.Path != source || part.Pages == nil || *part.Pages != want.Pages || part.OutputPages == nil || *part.OutputPages != want.Output || part.Spans == nil || len(*part.Spans) != 1 || (*part.Spans)[0] != want.Span {
			return fmt.Errorf("literal occurrence mapping %d: got %+v want %+v from %s", n, part, want, source)
		}
	}
	return nil
}
