// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

type producerCase struct {
	Name    string
	Pages   int
	Markers []string
}

var producerCases = []producerCase{
	{"business-report.pdf", 3, []string{"Project evidence report", "Measured comparison", "Supporting notes"}},
	{"approval-form.pdf", 2, []string{"Review approval", "FINAL REVISION"}},
	{"office-report.pdf", 2, []string{"Office summary", "Office appendix"}},
	{"technical-note.pdf", 1, []string{"Calibration note"}},
	{"scan-invoice.pdf", 2, []string{"", ""}},
	{"business-report-object-streams.pdf", 3, []string{"Project evidence report", "Measured comparison", "Supporting notes"}},
}

func corpusProducers(t *testing.T) {
	var records []corpusRecord
	for _, tc := range producerCases {
		t.Run(tc.Name, func(t *testing.T) {
			dir := corpusStage(t, tc.Name)
			source := filepath.Join(dir, tc.Name)
			r := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromItems(core.Source(source, "producer")), core.Options{WorkingDir: dir, Output: filepath.Join(dir, "packet.pdf"), Report: filepath.Join(dir, "report.json")}))
			if r.Status != "ok" || r.Pages == nil || r.Pages.Total == nil || *r.Pages.Total != int64(tc.Pages) {
				t.Fatalf("producer outcome %+v", r)
			}
			corpusArtifact(t, r)
			schemaValid(t, r)
			corpusJSON(t, filepath.Join(dir, "complete.json"), r)
			records = append(records, corpusRecord{ID: tc.Name, Boundary: "core direct source", ArtifactDir: dir})
			t.Run("oracle", func(t *testing.T) {
				tools := oracle.RequireTools(t)
				if err := tools.Check(r.Output.Path); err != nil {
					t.Fatal(err)
				}
				doc, err := oracle.Load(tools, r.Output.Path)
				if err != nil {
					t.Fatal(err)
				}
				texts, err := doc.RawTexts()
				if err != nil || len(texts) != tc.Pages {
					t.Fatalf("source text/count %v %v", texts, err)
				}
				for n, marker := range tc.Markers {
					if marker == "" {
						if strings.TrimSpace(texts[n]) != "" {
							t.Fatalf("scan has unexpected text: %q", texts[n])
						}
					} else if !strings.Contains(texts[n], marker) {
						t.Fatalf("page %d missing %q: %q", n+1, marker, texts[n])
					}
				}
				if tc.Name == "approval-form.pdf" && strings.Contains(strings.Join(texts, " "), "Supporting page without widgets") {
					t.Fatal("superseded supporting text survived")
				}
				if tc.Name == "scan-invoice.pdf" {
					for page := 1; page <= 2; page++ {
						pixel, err := doc.PixelColor(page, 20, 20)
						want := [3]uint8{220, 30, 30}
						if page == 2 {
							want = [3]uint8{20, 20, 20}
						}
						if err != nil || !nearCorpus(float64(pixel[0]), float64(want[0]), 3) || !nearCorpus(float64(pixel[1]), float64(want[1]), 3) || !nearCorpus(float64(pixel[2]), float64(want[2]), 3) {
							t.Fatalf("image-only page was blank/unpainted: %v %v", pixel, err)
						}
					}
				}

				corpusPassThroughFidelity(t, tools, r.Output.Path, source, 1)
				corpusProducerResources(t, tools, r.Output.Path, source)
				corpusJSON(t, filepath.Join(t.ArtifactDir(), "source-facts.json"), map[string]any{"texts": texts, "pages": doc.PageCount(), "version": doc.EffectiveVersion()})
			})
		})
	}
	t.Run("mixed-office-scan-technical", func(t *testing.T) {
		dir := corpusStage(t, "office-report.pdf", "scan-invoice.pdf", "technical-note.pdf")
		r := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromItems(core.Source("office-report.pdf", "office"), core.Source("scan-invoice.pdf", "scan"), core.Source("technical-note.pdf", "note")), core.Options{WorkingDir: dir, Output: filepath.Join(dir, "packet.pdf")}))
		if r.Status != "ok" || *r.Pages.Total != 5 {
			t.Fatal(r)
		}
		corpusArtifact(t, r)
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			texts, err := doc.RawTexts()
			if err != nil || len(texts) != 5 || !strings.Contains(texts[0], "Office summary") || strings.TrimSpace(texts[2]) != "" || !strings.Contains(texts[4], "Calibration note") {
				t.Fatalf("mixed source order %q %v", texts, err)
			}
			corpusPassThroughFidelity(t, tools, r.Output.Path, filepath.Join(dir, "office-report.pdf"), 1)
			corpusPassThroughFidelity(t, tools, r.Output.Path, filepath.Join(dir, "scan-invoice.pdf"), 3)
			corpusPassThroughFidelity(t, tools, r.Output.Path, filepath.Join(dir, "technical-note.pdf"), 5)
			if want := corpusCFFPrograms(t, tools, filepath.Join(dir, "technical-note.pdf")); len(want) == 0 || !slices.Equal(corpusCFFPrograms(t, tools, r.Output.Path), want) {
				t.Fatal("mixed output lost imported CFF programs")
			}

		})
	})
	corpusJSON(t, filepath.Join(t.ArtifactDir(), "matrix.json"), records)
}
func TestCorpusAssetIdentity(t *testing.T) {
	var manifest struct {
		Assets []struct {
			Path, SHA256 string
			Bytes        int64
		}
	}
	raw, err := os.ReadFile("testdata/corpus/provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Assets) != 8 {
		t.Fatal("six PDF/two-font asset bill missing")
	}
	seen := map[string]bool{}
	for _, asset := range manifest.Assets {
		if seen[asset.Path] {
			t.Fatal("duplicate asset", asset.Path)
		}
		seen[asset.Path] = true
		data, err := os.ReadFile(filepath.Join("testdata/corpus", asset.Path))
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(data)) != asset.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != asset.SHA256 {
			t.Fatal("corrupt/missing reviewed asset", asset.Path)
		}
	}
}

func TestCorpusReportLabPackets(t *testing.T) {
	for _, name := range []string{"business-report.pdf", "approval-form.pdf"} {
		t.Run(name, func(t *testing.T) {
			dir := corpusStage(t, name)
			var items []any
			pages := 3
			if name == "business-report.pdf" {
				pages = 6
				if err := pdffixture.Plain("SOURCE A").WriteFile(filepath.Join(dir, "plain.pdf")); err != nil {
					t.Fatal(err)
				}
				section := corpusSection("Evidence — Pielikums Ā Ελληνικά", corpusSource(name))
				section["start_on"] = "odd"
				section["divider"] = map[string]any{}
				items = []any{corpusSource("plain.pdf"), map[string]any{"contents": map[string]any{}}, section}
			} else {
				section := corpusSection("Approval", corpusSource(name))
				section["divider"] = map[string]any{}
				items = []any{section}
			}
			_, r := corpusRecipe(t, dir, map[string]any{"page_defaults": map[string]any{"size": "A4"}, "page_numbers": map[string]any{}, "items": items})
			if *r.Pages.Total != int64(pages) {
				t.Fatal(r.Pages)
			}
			wantTarget := int64(1)
			if name == "business-report.pdf" {
				wantTarget = 3
			}
			if *r.Sections[0].TargetPage != wantTarget {
				t.Fatal("business packet target", r.Sections)
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
				if name == "business-report.pdf" {
					joined := strings.Join(texts, " ")
					for _, marker := range []string{"Project evidence report", "Pielikums Ā ļ ķ", "Measured comparison", "Supporting notes"} {
						if !strings.Contains(joined, marker) {
							t.Fatal(marker, joined)
						}
					}
				} else {
					raw, err := oracle.QPDFJSON(tools, r.Output.Path)
					if err != nil {
						t.Fatal(err)
					}
					var q struct {
						Acroform struct {
							Fields []struct {
								Fullname   string `json:"fullname"`
								Value      string `json:"value"`
								Annotation struct {
									AppearanceState string `json:"appearancestate"`
								} `json:"annotation"`
							} `json:"fields"`
						} `json:"acroform"`
					}

					if err = json.Unmarshal(raw, &q); err != nil {
						t.Fatal(err)
					}
					if len(q.Acroform.Fields) != 2 {
						t.Fatal("canonical live fields lost")
					}
					seen := map[string]string{}
					for _, field := range q.Acroform.Fields {
						seen[field.Fullname] = field.Value
						if field.Fullname == "approved" && field.Annotation.AppearanceState != "/Yes" {
							t.Fatal("checkbox appearance stale")
						}
					}
					if seen["reviewer"] != "u:Field tester" || seen["approved"] != "/Yes" {
						t.Fatal("canonical values changed", seen)
					}
				}
			})
		})
	}
}
