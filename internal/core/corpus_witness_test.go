// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestCorpusProfileWitnessRejectsMissingFooterAndTree(t *testing.T) {
	t.Run("missing-footer", func(t *testing.T) {
		p := corpusProfiles[2]
		dir := corpusStage(t, "business-report.pdf", "fonts/Go-Italic.ttf", "fonts/Go-Mono.ttf")
		plan := profilePlan(p)
		delete(plan, "page_numbers")
		_, r := corpusRecipe(t, dir, plan)
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			if err := corpusFooterError(p, corpusTextGeometry(t, tools, r.Output.Path)); err == nil {
				t.Fatal("body/source digits incorrectly satisfied missing physical footers")
			}
		})
	})
	t.Run("removed-tree", func(t *testing.T) {
		p := corpusProfiles[len(corpusProfiles)-1]
		dir := corpusStage(t, "business-report-object-streams.pdf", "fonts/Go-Italic.ttf", "fonts/Go-Mono.ttf")
		_, r := corpusRecipe(t, dir, profilePlan(p))
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			qdf := filepath.Join(t.ArtifactDir(), "with-tree.qdf.pdf")
			if data, err := exec.CommandContext(t.Context(), tools.QPDF, "--qdf", "--object-streams=disable", r.Output.Path, qdf).CombinedOutput(); err != nil {
				t.Fatal(string(data), err)
			}
			data, err := os.ReadFile(qdf)
			if err != nil {
				t.Fatal(err)
			}
			pattern := regexp.MustCompile(`/Outlines [0-9]+ [0-9]+ R`)
			matches := pattern.FindAllIndex(data, -1)
			if len(matches) != 1 {
				t.Fatal("catalog outline reference witness", len(matches))
			}
			for _, span := range matches {
				copy(data[span[0]:span[1]], bytes.Repeat([]byte(" "), span[1]-span[0]))
			}
			removed := filepath.Join(t.ArtifactDir(), "without-tree.pdf")
			if err = os.WriteFile(removed, data, 0600); err != nil {
				t.Fatal(err)
			}
			doc, err := oracle.Load(tools, removed)
			if err != nil {
				t.Fatal(err)
			}
			if err = doc.CheckOutlineTree(p.Outlines); err == nil {
				t.Fatal("valid output with removed catalog tree satisfied literal navigation")
			}
			wrong := append([]oracle.Outline(nil), p.Outlines...)
			wrong[0].Title = "Wrong title"
			actual, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err = actual.CheckOutlineTree(wrong); err == nil {
				t.Fatal("wrong expected tree accepted")
			}
		})
	})
}
func TestCorpusProducerWitnessRejectsHeaderAndCornerOnly(t *testing.T) {
	for _, name := range []string{"technical-note.pdf", "scan-invoice.pdf"} {
		t.Run(name, func(t *testing.T) {
			dir := corpusStage(t, name)
			source := filepath.Join(dir, name)
			var wrong *pdffixture.Doc
			if name == "technical-note.pdf" {
				wrong = pdffixture.WithBoxes("Calibration note", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 595.2756 841.8898]"})
			} else {
				wrong = pdffixture.WithBoxes("ignored", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 600 800]"}, pdffixture.PageBoxes{Media: "[0 0 800 600]"})
				for n, program := range []string{"0.862745098 0.117647059 0.117647059 rg 0 720 80 80 re f\n", "0.078431373 g 0 520 80 80 re f\n"} {
					wrong.Objs[4+2*n] = []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(program), program))
				}
			}
			output := filepath.Join(dir, "wrong.pdf")
			if err := wrong.WriteFile(output); err != nil {
				t.Fatal(err)
			}
			t.Run("oracle", func(t *testing.T) {
				tools := oracle.RequireTools(t)
				if err := tools.Check(output); err != nil {
					t.Fatal("negative control must itself be a valid PDF", err)
				}
				actual, err := oracle.Load(tools, output)
				if err != nil {
					t.Fatal(err)
				}
				original, err := oracle.Load(tools, source)
				if err != nil {
					t.Fatal(err)
				}
				if err = corpusVisibleFidelity(actual, original, 1); err == nil || !strings.Contains(err.Error(), "producer") {
					t.Fatal("valid header/corner-only output satisfied full producer fidelity", err)
				}
				if name == "technical-note.pdf" && len(corpusCFFPrograms(t, tools, output)) != 0 {
					t.Fatal("header-only fixture unexpectedly has CFF")
				}
			})
		})
	}
}
