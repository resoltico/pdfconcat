// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	exchange "github.com/resoltico/pdfconcat/internal/wire"
)

func TestFittedNoRotateWarningsInSummaryAndReport(t *testing.T) {
	const code = "fit_annotation_viewer_dependent"
	const annotations = 21
	dir := t.TempDir()
	doc := pdffixture.FitAnnotation("Square", "", true, 90, 16)
	objects := []int{6}
	for i := 1; i < annotations; i++ {
		objects = append(objects, len(doc.Objs)+1)
		annot := doc.Objs[5]
		if i == 1 {
			annot = bytes.Replace(annot, []byte("/Subtype /Square"), []byte("/Subtype /Text"), 1)
			annot = bytes.Replace(annot, []byte("/F 16 "), []byte("/F 0 "), 1)
		}
		doc.Objs = append(doc.Objs, annot)
	}
	var refs []byte
	for _, object := range objects {
		refs = fmt.Appendf(refs, "%d 0 R ", object)
	}
	doc.Objs[2] = bytes.Replace(doc.Objs[2], []byte("/Annots [6 0 R]"), fmt.Appendf(nil, "/Annots [%s]", refs), 1)
	// The first source page retains its native rotation because its displayed
	// size equals the target. It has its own annotation using the same AP.
	keptAnnot := len(doc.Objs) + 1
	keptPage := keptAnnot + 1
	doc.Objs = append(doc.Objs, doc.Objs[5],
		fmt.Appendf(nil, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Rotate 90 /Annots [%d 0 R] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", keptAnnot))
	doc.Objs[1] = fmt.Appendf(nil, "<< /Type /Pages /Kids [%d 0 R 3 0 R] /Count 2 >>", keptPage)
	source := filepath.Join(dir, "annot.pdf")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	const plan = `{"version":11,"structure":"discard","fitting":{"target":"100x200pt"},"items":[{"source":"annot.pdf","pages":[[2,2]]},{"source":"annot.pdf","pages":[[1,1]]},{"source":"annot.pdf"}]}`
	for _, op := range []string{"check", "build"} {
		t.Run(op, func(t *testing.T) {
			report := filepath.Join(dir, op+".json")
			summary, exit, _ := invokeJSON(t, []string{op, "--plan", "-", "--base-dir", dir, "-o", filepath.Join(dir, "out.pdf"), "--report", report}, plan)
			if exit != 0 || summary.FormatVersion != 11 || summary.Output == nil || summary.Output.Published != (op == "build") {
				t.Fatalf("unexpected status or publication: %d %+v", exit, summary)
			}
			raw, err := os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			validateResult(t, raw)
			var full exchange.Result
			if err := json.Unmarshal(raw, &full); err != nil {
				t.Fatal(err)
			}
			for _, view := range []struct {
				name  string
				facts exchange.Result
				count int
			}{{"summary", summary, 20}, {"report", full, annotations}} {
				if len(view.facts.Diagnostics) != view.count || view.facts.DiagnosticsOmitted != annotations-view.count {
					t.Fatalf("%s diagnostic count: %+v", view.name, view.facts.Diagnostics)
				}
				for i, d := range view.facts.Diagnostics {
					subtype := "Square"
					if i == 1 {
						subtype = "Text"
					}
					if d.Code.String() != code || d.Severity != "warning" || d.File != source ||
						d.PDFLocation == nil || d.PDFLocation.Object != objects[i] || d.PDFLocation.Page != 2 ||
						!slices.Equal(d.Items, []string{"/items/0", "/items/2"}) ||
						!strings.Contains(d.Message, subtype) || !strings.Contains(d.Message, "original page sizes") {
						t.Fatalf("%s annotation %d: %+v", view.name, i, d)
					}
				}
			}
			if !slices.EqualFunc(summary.Diagnostics, full.Diagnostics[:len(summary.Diagnostics)], func(a, b exchange.Diagnostic) bool {
				return a.Code == b.Code && a.Message == b.Message && *a.PDFLocation == *b.PDFLocation && slices.Equal(a.Items, b.Items)
			}) {
				t.Fatal("summary and report disagree on included fitting warnings")
			}
		})
	}
}

func TestFittingAdmitsAnnotationReferencesBeforePruning(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(fmt.Sprintf("direct=%t", direct), func(t *testing.T) {
			dir := t.TempDir()
			doc := pdffixture.FitAnnotation("Square", "", true, 90, 16)
			doc.Objs[1] = []byte("<< /Type /Pages /Kids [3 0 R 9 0 R] /Count 2 >>")
			doc.Objs = append(doc.Objs,
				[]byte("<< /Type /Annot /Subtype /Link /Rect [20 25 60 70] /Border [0 0 0] /Dest [9 0 R /Fit] >>"),
				[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"))
			second := []byte("6 0 R")
			if direct {
				second = doc.Objs[5]
				doc.Objs[5] = []byte("null")
			}
			doc.Objs[2] = bytes.Replace(doc.Objs[2], []byte("/Annots [6 0 R]"), fmt.Appendf(nil, "/Annots [8 0 R %s]", second), 1)
			if err := doc.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			const plan = `{"version":11,"structure":"discard","fitting":{"target":"100x200pt"},"items":[{"source":"source.pdf"},{"source":"source.pdf","pages":[[1,1]]}]}`
			output := filepath.Join(dir, "out.pdf")
			for _, op := range []string{"check", "build"} {
				result, exit, _ := invokeJSON(t, []string{op, "--plan", "-", "--base-dir", dir, "-o", output}, plan)
				if direct {
					if exit != 2 || result.Output == nil || result.Output.Published || len(result.Diagnostics) != 1 {
						t.Fatalf("%s expected early source refusal: exit %d, %+v", op, exit, result)
					}
					finding := result.Diagnostics[0]
					want := exchange.PDFLocation{Object: 3, Page: 1, Path: "Annots[1]"}
					if finding.Code.String() != "source_invalid" || finding.PDFLocation == nil || *finding.PDFLocation != want ||
						!slices.Equal(finding.Items, []string{"/items/0", "/items/1"}) {
						t.Fatalf("%s lost the original invalid annotation slot: %+v", op, finding)
					}
					if _, err := os.Stat(output); !os.IsNotExist(err) {
						t.Fatalf("%s published an output on refusal: %v", op, err)
					}
					continue
				}
				if exit != 0 || result.Output == nil || result.Output.Published != (op == "build") {
					t.Fatalf("%s rejected indirect control: exit %d, %+v", op, exit, result)
				}
				var advisory, pruned int
				for _, finding := range result.Diagnostics {
					switch finding.Code.String() {
					case "fit_annotation_viewer_dependent":
						advisory++
						if finding.PDFLocation == nil || finding.PDFLocation.Object != 6 || finding.PDFLocation.Page != 1 ||
							!slices.Equal(finding.Items, []string{"/items/0", "/items/1"}) {
							t.Fatalf("pruning changed annotation attribution: %+v", finding)
						}
					case "source_navigation_removed":
						pruned++
						if !slices.Equal(finding.Items, []string{"/items/1"}) {
							t.Fatalf("unexpected link pruning: %+v", finding)
						}
					}
				}
				if advisory != 1 || pruned != 1 {
					t.Fatalf("wanted one advisory and one pruning warning: %+v", result.Diagnostics)
				}
			}
		})
	}
}
