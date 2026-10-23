// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func localLink(destination string, indirect bool) *pdffixture.Doc {
	d := pdffixture.Plain("target")
	target := "[3 0 R " + destination + "]"
	if indirect {
		target = "7 0 R"
	}
	insertEntry(d, 3, "/Annots [6 0 R]")
	d.Objs = append(d.Objs, []byte("<< /Subtype /Link /Rect [10 20 30 40] /Dest "+target+" >>"))
	if indirect {
		d.Objs = append(d.Objs, []byte("[3 0 R "+destination+"]"))
	}
	return d
}

func screenAction(chained bool) *pdffixture.Doc {
	d := localLink("/Fit", false)
	action := "<< /S /GoTo /D [3 0 R /FitH 20] >>"
	if chained {
		action = "<< /S /URI /URI (https://example.invalid) /Next " + action + " >>"
	}
	d.Objs[5] = []byte("<< /Subtype /Screen /Rect [10 20 30 40] /P 3 0 R /A " + action + " >>")
	return d
}

func TestFittingDestinationCoordinates(t *testing.T) {
	for _, tc := range []struct {
		position string
		tail     []any
	}{
		{"/XYZ 0 null null", []any{"/XYZ", float64(0), nil, nil}},
		{"/XYZ null 20 null", []any{"/XYZ", nil, float64(20), nil}},
		{"/FitH 20", []any{"/FitH", float64(20)}},
		{"/FitBH 20", []any{"/FitBH", float64(20)}},
		{"/FitV 20", []any{"/FitV", float64(20)}},
		{"/FitBV 20", []any{"/FitBV", float64(20)}},
		{"/FitR 0 0 40 40", []any{"/FitR", float64(0), float64(0), float64(40), float64(40)}},
	} {
		position := tc.position
		t.Run(position, func(t *testing.T) {
			d := localLink(position, true)
			f, _, ds := pdf.Inspect(t.Context(), d.Bytes(), nil)
			if status, _ := diag.Outcome(ds); status != "ok" || !f.HasCoordinateDestinations {
				t.Fatalf("facts %+v: %+v", f, ds)
			}
			_, _, ds = assembleDocs(t, []*pdffixture.Doc{d}, &packet.Size{Width: 300, Height: 400})
			if !codePresent(ds, diag.FitDestinationUnsupported) {
				t.Fatal(ds)
			}
			path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, nil)
			if status, _ := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			output, err := oracle.Load(oracle.RequireTools(t), path)
			if err != nil {
				t.Fatal(err)
			}
			page, tail, err := output.AnnotationDestination(1, 0)
			if err != nil || page != 1 || !reflect.DeepEqual(tail, tc.tail) {
				t.Fatalf("unfitted destination page=%d tail=%v: %v", page, tail, err)
			}
		})
	}
	for _, position := range []string{"/Fit", "/FitB", "/XYZ null null 2", "/FitH null", "/FitBH null", "/FitV null", "/FitBV null"} {
		t.Run(position, func(t *testing.T) {
			path, _, ds := assembleDocs(t, []*pdffixture.Doc{localLink(position, true)}, &packet.Size{Width: 306, Height: 396})
			if status, _ := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			d, err := oracle.Load(oracle.RequireTools(t), path)
			if err != nil {
				t.Fatal(err)
			}
			targets, err := d.ActionTargets(1)
			if err != nil || len(targets) != 1 || targets[0] != 1 {
				t.Fatalf("targets %v: %v", targets, err)
			}
			rects, err := d.AnnotationRects(1)
			if err != nil || len(rects) != 1 || rects[0] != [4]float64{5, 10, 15, 20} {
				t.Fatalf("rectangles %v: %v", rects, err)
			}
			width, height, err := d.RenderedSize(1)
			if err != nil || width != 306 || height != 396 || d.UserUnit(1) != 1 {
				t.Fatalf("physical sheet %dx%d: %v", width, height, err)
			}
		})
	}
}

func TestRetainedDestinationRoles(t *testing.T) {
	cases := []struct {
		name string
		doc  *pdffixture.Doc
		want bool
	}{
		{"named repeated", pdffixture.NamedDests("named"), true},
		{"legacy", pdffixture.LegacyDests("legacy"), true},
		{"outline", pdffixture.Outlined("outline"), true},
		{"chained", localLink("/Fit", false), true},
		{"Screen direct", screenAction(false), true},
		{"Screen chained", screenAction(true), true},
		{"unreferenced named", pdffixture.Plain("named"), true},
		{"removed catalog action", insertEntry(pdffixture.Plain("removed"), 1, "/OpenAction [3 0 R /XYZ 20 30 null]"), false},
		{"private", insertEntry(pdffixture.Plain("private"), 3, "/Private << /Dest [3 0 R /XYZ 20 30 null] /S /GoTo /D [3 0 R /FitH 20] >>"), false},
		{"remote", localLink("/Fit", false), false},
		{"Text private destination", localLink("/XYZ 20 30 null", false), false},
		{"outline root private destination", pdffixture.Outlined("root"), false},
		{"dropped named chain", localLink("/Fit", false), false},
	}
	for i := range cases {
		tc := &cases[i]
		switch tc.name {
		case "named repeated", "legacy", "outline":
			for n, b := range tc.doc.Objs {
				tc.doc.Objs[n] = bytes.ReplaceAll(b, []byte("/Fit]"), []byte("/XYZ 20 30 null]"))
			}
		case "chained":
			tc.doc.Objs[5] = []byte("<< /Subtype /Link /Rect [10 20 30 40] /A << /S /URI /URI (https://example.invalid) /Next [<< /S /GoTo /D [3 0 R /FitH 20] >>] >> >>")
		case "unreferenced named":
			insertEntry(tc.doc, 1, "/Names << /Dests << /Names [(target) [3 0 R /FitV 20]] >> >>")
		case "Text private destination":
			tc.doc.Objs[5] = bytes.ReplaceAll(tc.doc.Objs[5], []byte("/Subtype /Link"), []byte("/Subtype /Text"))
		case "outline root private destination":
			for n, b := range tc.doc.Objs {
				if bytes.Contains(b, []byte("/Type /Outlines")) {
					insertEntry(tc.doc, n+1, "/Dest [3 0 R /XYZ 20 30 null]")
				}
			}
		case "remote":
			tc.doc.Objs[5] = []byte("<< /Subtype /Link /Rect [10 20 30 40] /A << /S /GoToR /F (remote.pdf) /D [0 /XYZ 20 30 null] >> >>")
		case "dropped named chain":
			insertEntry(tc.doc, 1, "/Names << /Dests << /Names [(outside) [20 0 R /XYZ 20 30 null]] >> >>")
			tc.doc.Objs[5] = []byte("<< /Subtype /Link /Rect [10 20 30 40] /A << /S /GoTo /D (outside) /Next << /S /GoTo /D [3 0 R /FitH 20] >> >> >>")
		}
		t.Run(tc.name, func(t *testing.T) {
			f, _, ds := pdf.Inspect(t.Context(), tc.doc.Bytes(), nil)
			if status, _ := diag.Outcome(ds); status != "ok" || f.HasCoordinateDestinations != tc.want {
				t.Fatalf("want coordinates=%v: facts %+v ds %+v", tc.want, f, ds)
			}
			docs := []*pdffixture.Doc{tc.doc}
			if tc.name == "named repeated" {
				docs = append(docs, tc.doc)
			}
			_, _, ds = assembleDocs(t, docs, &packet.Size{Width: 300, Height: 400})
			if codePresent(ds, diag.FitDestinationUnsupported) != tc.want {
				t.Fatal(ds)
			}
		})
	}
}

func TestCompatibilityRefusesBeforePublication(t *testing.T) {
	for _, kind := range []string{"coordinates", "Screen direct", "Screen chained", "appearance"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%v", kind, reverse), func(t *testing.T) {
				dir := t.TempDir()
				docs := []*pdffixture.Doc{localLink("/FitH 20", false)}
				code := diag.FitDestinationUnsupported
				fit := "A4"
				if strings.HasPrefix(kind, "Screen") {
					docs = []*pdffixture.Doc{screenAction(kind == "Screen chained")}
				}
				if kind == "appearance" {
					docs = []*pdffixture.Doc{defaultForm("FORM", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), pdffixture.Versioned("plain", "2.0", "")}
					code = diag.SourceOutputIncompatible
					fit = ""
				}
				if reverse && len(docs) == 2 {
					docs[0], docs[1] = docs[1], docs[0]
				}
				operands := []core.Item{}
				for i, d := range docs {
					name := fmt.Sprintf("source-%d.pdf", i)
					if err := d.WriteFile(filepath.Join(dir, name)); err != nil {
						t.Fatal(err)
					}
					operands = append(operands, core.SourceItem{Path: name, Ref: fmt.Sprintf("argv:%d", i+1)})
				}
				output := filepath.Join(dir, "packet.pdf")
				before := []byte("preserve existing output")
				if err := os.WriteFile(output, before, 0600); err != nil {
					t.Fatal(err)
				}
				for _, command := range []string{"check", "build"} {
					options := core.Request{WorkingDir: dir, Output: output, Overwrite: true}
					if fit != "" {
						sheet, _ := core.ParseSheet(fit)
						options.FitTo = &sheet
					}
					r := core.Execute(t.Context(), core.Request{Operation: core.Operation(command), Input: core.Items(operands), WorkingDir: options.WorkingDir, Output: options.Output, Report: options.Report, FitTo: options.FitTo, Overwrite: options.Overwrite})
					if r.Status != "invalid" || !codePresent(r.Diagnostics, code) || r.Output.Published {
						t.Fatalf("%s: %+v", command, r)
					}
					after, err := os.ReadFile(output)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatalf("published on refusal: %q %v", after, err)
					}
					entries, err := os.ReadDir(dir)
					if err != nil || len(entries) != len(docs)+1 {
						t.Fatalf("stage created on refusal: %+v %v", entries, err)
					}
				}
			})
		}
	}
}

func TestFlatOutlineSiblingsRemainSupported(t *testing.T) {
	d := pdffixture.Plain("outlines")
	const count = 160
	insertEntry(d, 1, "/Outlines 6 0 R")
	d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /Outlines /First 7 0 R /Last %d 0 R /Count %d >>", 6+count, count)))
	for i := range count {
		links := ""
		if i > 0 {
			links += fmt.Sprintf(" /Prev %d 0 R", 6+i)
		}
		if i+1 < count {
			links += fmt.Sprintf(" /Next %d 0 R", 8+i)
		}
		d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Title (bookmark %d) /Parent 6 0 R /Dest [3 0 R /Fit] %s >>", i, links)))
	}
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, &packet.Size{Width: 306, Height: 396})
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := output.OutlineTargets()
	if err != nil || len(targets) != count {
		t.Fatalf("outlines %v: %v", targets, err)
	}
	for _, page := range targets {
		if page != 1 {
			t.Fatalf("wrong bookmark page %d", page)
		}
	}
}

// actionChain varies only the PDF spelling of Next, not its semantic nesting.
func actionChain(count int, arrayNext bool) *pdffixture.Doc {
	d := localLink("/Fit", false)
	d.Objs[5] = []byte("<< /Subtype /Link /Rect [10 20 30 40] /A 7 0 R >>")
	for i := range count {
		next := ""
		if i+1 < count {
			ref := fmt.Sprintf("%d 0 R", 8+i)
			if arrayNext {
				ref = "[" + ref + "]"
			}
			next = " /Next " + ref
		}
		d.Objs = append(d.Objs, []byte("<< /S /URI /URI (https://example.invalid)"+next+" >>"))
	}
	return d
}

func pageTreeWithLink(depth int) *pdffixture.Doc {
	d := localLink("/Fit", false)
	if depth == 1 {
		return d
	}
	// The ordinary fixture's Pages root is depth zero and its Page is depth one.
	d.Objs[1] = []byte("<< /Type /Pages /Kids [7 0 R] /Count 1 >>")
	for i := range depth - 1 {
		parent := 2
		if i > 0 {
			parent = 6 + i
		}
		child := 3
		if i+1 < depth-1 {
			child = 8 + i
		}
		d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /Pages /Parent %d 0 R /Kids [%d 0 R] /Count 1 >>", parent, child)))
	}
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Parent 2 0 R"), []byte(fmt.Sprintf("/Parent %d 0 R", 5+depth)))
	return d
}

func fieldTreeWithWidget(depth int) *pdffixture.Doc {
	d := defaultForm("field tree", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
	d.Objs[0] = bytes.ReplaceAll(d.Objs[0], []byte("/Fields [6 0 R]"), []byte("/Fields [7 0 R]"))
	for i := range depth {
		child := 6
		if i+1 < depth {
			child = 8 + i
		}
		parent := ""
		if i > 0 {
			parent = fmt.Sprintf(" /Parent %d 0 R", 6+i)
		}
		d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /T (group%d) /Kids [%d 0 R]%s >>", i, child, parent)))
	}
	insertEntry(d, 6, fmt.Sprintf("/Parent %d 0 R /A << /S /URI /URI (https://example.invalid) >>", 6+depth))
	return d
}

func TestInspectionPreservesValidatedGraphBoundaries(t *testing.T) {
	for _, count := range []int{63, 70, 90, 102} {
		for _, arrayNext := range []bool{false, true} {
			t.Run(fmt.Sprintf("actions=%d/array=%v", count, arrayNext), func(t *testing.T) {
				d := actionChain(count, arrayNext)
				path := filepath.Join(t.TempDir(), "source.pdf")
				if err := d.WriteFile(path); err != nil {
					t.Fatal(err)
				}
				if qpdfCheck(t, path) != 1 {
					t.Fatal("action-chain source page count")
				}
				facts, _, ds := pdf.Inspect(t.Context(), d.Bytes(), nil)
				if status, _ := diag.Outcome(ds); status != "ok" || facts.Pages != 1 {
					t.Fatalf("representation changed accepted action chain: %+v", ds)
				}
				// A deep local target must still be observed, not silently truncated.
				d.Objs[len(d.Objs)-1] = []byte("<< /S /GoTo /D [3 0 R /FitH 20] >>")
				facts, _, ds = pdf.Inspect(t.Context(), d.Bytes(), nil)
				if status, _ := diag.Outcome(ds); status != "ok" || !facts.HasCoordinateDestinations {
					t.Fatalf("deep target was not inspected: %+v %+v", facts, ds)
				}
			})
		}
	}
	for _, tc := range []struct {
		name string
		doc  *pdffixture.Doc
		code diag.Code
	}{
		{"page tree at depth 64 with link", pageTreeWithLink(64), diag.Code{}},
		{"page tree over depth 64", pageTreeWithLink(65), diag.SourceDamaged},
		{"field tree at depth 64 with widget", fieldTreeWithWidget(64), diag.Code{}},
		{"field tree over depth 64", fieldTreeWithWidget(65), diag.SourceInvalid},
		{"excessive direct nesting", insertEntry(pdffixture.Plain("nested"), 1, "/Private "+strings.Repeat("<< /Private ", 130)+"null"+strings.Repeat(" >>", 130)), diag.SourceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts, _, ds := pdf.Inspect(t.Context(), tc.doc.Bytes(), nil)
			if tc.code != (diag.Code{}) {
				if !codePresent(ds, tc.code) {
					t.Fatalf("hierarchy guard did not reject: %+v", ds)
				}
			} else if status, _ := diag.Outcome(ds); status != "ok" || facts.Pages != 1 {
				t.Fatalf("observer narrowed page-tree boundary: %+v", ds)
			}
			if tc.code == (diag.Code{}) {
				path := filepath.Join(t.TempDir(), "source.pdf")
				if err := tc.doc.WriteFile(path); err != nil {
					t.Fatal(err)
				}
				if qpdfCheck(t, path) != 1 {
					t.Fatal("tree-boundary source page count")
				}
			}
		})
	}
	cycle := actionChain(1, false)
	cycle.Objs[6] = []byte("<< /S /URI /URI (https://example.invalid) /Next [7 0 R] >>")
	if _, _, ds := pdf.Inspect(t.Context(), cycle.Bytes(), nil); len(ds) != 0 {
		t.Fatalf("observer must terminate on cycles accepted by relaxed validation: %+v", ds)
	}
	pageCycle := pageTreeWithLink(2)
	pageCycle.Objs[6] = []byte("<< /Type /Pages /Parent 2 0 R /Kids [2 0 R] /Count 1 >>")
	if _, _, ds := pdf.Inspect(t.Context(), pageCycle.Bytes(), nil); !codePresent(ds, diag.SourceDamaged) {
		t.Fatalf("cyclic page tree escaped source guard: %+v", ds)
	}
}
