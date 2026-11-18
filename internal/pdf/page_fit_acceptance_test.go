// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	exchange "github.com/resoltico/pdfconcat/internal/wire"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// These observations use qpdf's independent parser, including effective parent
// values. They do not call the production geometry reader or fit classifier.
func qpdfPages(t *testing.T, path string) []map[string]any {
	t.Helper()
	var dump struct {
		QPDF  []map[string]json.RawMessage `json:"qpdf"`
		Pages []struct {
			Object string `json:"object"`
		} `json:"pages"`
	}
	if e := json.Unmarshal(run(t, qpdfTool, "--json", path), &dump); e != nil {
		t.Fatal(e)
	}
	var deref func(any) any
	deref = func(v any) any {
		if ref, ok := v.(string); ok {
			if raw, exists := dump.QPDF[1]["obj:"+ref]; exists {
				var item struct {
					Value any `json:"value"`
				}
				if e := json.Unmarshal(raw, &item); e != nil {
					t.Fatal(e)
				}
				return deref(item.Value)
			}
		}
		return v
	}
	var pages []map[string]any
	for _, p := range dump.Pages {
		original, ok := deref(p.Object).(map[string]any)
		if !ok {
			t.Fatal("qpdf page is not a dictionary")
		}
		page := map[string]any{}
		for k, v := range original {
			page[k] = deref(v)
		}
		for _, k := range []string{"/MediaBox", "/CropBox", "/Rotate"} {
			for parent := original; parent != nil; {
				if v := deref(parent[k]); v != nil {
					page[k] = v
					break
				}
				parent, _ = deref(parent["/Parent"]).(map[string]any)
			}
		}
		pages = append(pages, page)
	}
	return pages
}
func nativePageFacts(p map[string]any) map[string]any {
	m := map[string]any{"/MediaBox": p["/MediaBox"], "/CropBox": p["/CropBox"], "/Rotate": p["/Rotate"], "/UserUnit": p["/UserUnit"]}
	if m["/CropBox"] == nil {
		m["/CropBox"] = m["/MediaBox"]
	}
	if m["/Rotate"] == nil {
		m["/Rotate"] = float64(0)
	}
	if m["/UserUnit"] == nil {
		m["/UserUnit"] = float64(1)
	}
	return m
}
func selectiveRequest(t *testing.T, data []byte, target string, operation app.Operation) (app.Result, string, string) {
	t.Helper()
	var policy *packet.FitPolicy
	if target != "" {
		sheet, err := packet.ParseSize(target, false)
		if err != nil {
			t.Fatal(err)
		}
		policy = packet.DefaultFitPolicy(sheet)
	}
	return selectivePolicyRequest(t, data, policy, operation)
}

func selectivePolicyRequest(t *testing.T, data []byte, policy *packet.FitPolicy, operation app.Operation) (app.Result, string, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	request := app.Request{Operation: operation, InspectionWorkers: 1, WorkingDir: dir, Input: app.Items{app.SourceItem{Ref: "source", Path: source}}, Output: output, Fitting: policy}
	result := app.Execute(t.Context(), request)
	if !bytes.Equal(data, readPDF(t, source)) {
		t.Fatal("source bytes changed")
	}
	return result, source, output
}
func TestSelectiveFitIndependentNativeAndRasterWitnesses(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		keep         bool
		refuse       bool
	}{
		{"exact-letter", "Letter", true, false}, {"crop-letter", "Letter", true, false}, {"rounded-a4", "A4", true, false},
		{"integer-a4", "A4", false, true}, {"rotated-letter", "Letter", true, false}, {"unit-letter", "Letter", true, false},
		{"mixed-to-kept", "100x100", false, false}, {"mixed-to-transform", "100x100", false, true},
		{"tiny-raw-large-unit", "", true, false}, {"large-origin", "A4", false, true}, {"large-origin", "600x800", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := readPDF(t, filepath.Join("testdata/page-fit", tc.name+".pdf"))
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, source, output := selectiveRequest(t, input, tc.target, op)
				if tc.name == "large-origin" && tc.target == "A4" {
					if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitGeometryUnsupported) || result.Output.Published {
						t.Fatal("unsupported accuracy was published", result.Diagnostics)
					}
					if len(result.Parts) != 1 || result.Parts[0].PageSizes != nil {
						t.Fatal("failed fitting admission invented complete expected geometry")
					}
					continue
				}
				if tc.refuse {
					if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitDestinationUnsupported) {
						t.Fatal(result.Diagnostics)
					}
				} else if result.Status != "ok" {
					t.Fatal(result.Diagnostics)
				}
				if len(result.Parts) != 1 || result.Parts[0].PageSizes == nil || len(*result.Parts[0].PageSizes) == 0 {
					t.Fatal("complete known metrics were lost", result)
				}
				if tc.refuse || op == app.Check {
					continue
				}
				qpdfCheck(t, output)
				before, after := qpdfPages(t, source), qpdfPages(t, output)
				if tc.keep {
					if !reflect.DeepEqual(nativePageFacts(before[0]), nativePageFacts(after[0])) {
						t.Fatal("keep rewrote native geometry", before, after)
					}
					for _, dict := range qpdfDictionaries(t, output) {
						if dict["/Subtype"] == "/Form" {
							t.Fatal("keep introduced a fitting Form")
						}
					}
					if tc.name != "tiny-raw-large-unit" {
						a, b := fitRaster(t, source, 1, 72), fitRaster(t, output, 1, 72)
						if !reflect.DeepEqual(a, b) {
							t.Fatal("kept viewport raster changed")
						}
					}
				}

				if tc.name == "mixed-to-kept" {
					if !reflect.DeepEqual(nativePageFacts(before[0]), nativePageFacts(after[0])) {
						t.Fatal("mixed kept geometry changed")
					}
					if !reflect.DeepEqual(after[1]["/MediaBox"], []any{float64(0), float64(0), float64(100), float64(100)}) {
						t.Fatal("mixed transform missing")
					}
				}
			}
		})
	}
}
func TestIntegerA4PositionedLinkUsesExplicitTolerance(t *testing.T) {
	target, err := packet.ParseSize("A4", false)
	if err != nil {
		t.Fatal(err)
	}
	input := readPDF(t, filepath.Join("testdata/page-fit", "integer-a4.pdf"))
	fit := &packet.FitPolicy{Target: target, Tolerance: 1}
	result, source, output := selectivePolicyRequest(t, input, fit, app.Build)
	if result.Status != "ok" || !result.Output.Published || result.Fitting == nil || *result.Fitting != *fit {
		t.Fatal(result)
	}
	qpdfCheck(t, output)
	before, after := qpdfPages(t, source), qpdfPages(t, output)
	if !reflect.DeepEqual(nativePageFacts(before[0]), nativePageFacts(after[0])) {
		t.Fatal("one-point tolerance changed the linked sheet's native frame")
	}
	for _, dict := range qpdfDictionaries(t, output) {
		if dict["/Subtype"] == "/Form" {
			t.Fatal("the kept sheet acquired a fitting wrapper")
		}
	}
}

func sharedLinkDoc(duplicate bool) *pdffixture.Doc {
	d := pdffixture.FitAnnotation("Link", "", false, 0, 4)
	d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 7 0 R] >>")
	d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [6 0 R] >>"))
	if duplicate {
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("[6 0 R]"), []byte("[6 0 R 6 0 R]"))
		d.Objs[6] = bytes.ReplaceAll(d.Objs[6], []byte(" /Annots [6 0 R]"), nil)
	}
	return d
}
func TestSharedAnnotationBindingsRefuseBeforeFitting(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		for _, target := range []string{"100x100", "50x50"} {
			result, _, output := selectiveRequest(t, sharedLinkDoc(duplicate).Bytes(), target, app.Build)
			// With a same-page duplicate, target 100x100 keeps its owner. Unrelated
			// transforms cannot mutate it, so the duplicate is allowed in that case.
			refuse := !duplicate || target == "50x50"
			if refuse {
				if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitAnnotationOwnershipUnsupported) || result.Output.Published {
					t.Fatal(result.Diagnostics)
				}
			} else {
				if result.Status != "ok" {
					t.Fatal(result.Diagnostics)
				}
				// The existing duplicate Annots binding itself attracts qpdf's
				// duplicate-entry warning; this keep control preserves that
				// input property and must not introduce fitting geometry changes.
				tools := oracle.RequireTools(t)
				raw, e := exec.CommandContext(t.Context(), tools.QPDF, "--json", output).Output()
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != 3 {
					t.Fatalf("expected retained duplicate warning, got %v", e)
				}
				found := false
				visitQPDFValues(t, raw, func(v any) {
					if d, ok := v.(map[string]any); ok && d["/Subtype"] == "/Link" {
						found = true
						if !reflect.DeepEqual(d["/Rect"], []any{float64(10), float64(10), float64(90), float64(90)}) {
							t.Fatal("kept duplicate annotation changed", d)
						}
					}
				})
				if !found {
					t.Fatal("kept annotation disappeared")
				}

			}
		}
	}
}
func TestOptionalPrintingBoxesAreHonestAndSharedRectanglesSurvive(t *testing.T) {
	for _, key := range []string{"BleedBox", "TrimBox", "ArtBox"} {
		d := pdffixture.FitAnnotation("Link", "", true, 0, 4)
		// Distinct annotations are unnecessary here; remove them while retaining
		// a rectangle shared between the kept and transformed page print entries.
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte(" /Annots [6 0 R]"), []byte(" /"+key+" 9 0 R"))
		d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 8 0 R] >>")
		d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /"+key+" 9 0 R >>"), []byte("[10 10 90 90]"))
		result, _, output := selectiveRequest(t, d.Bytes(), "100x100", app.Build)
		if result.Status != "ok" {
			t.Fatal(key, result.Diagnostics)
		}
		warnings := 0
		for _, finding := range result.Diagnostics {
			if finding.Code == diag.FitPrintBoxOmitted {
				warnings++
				if finding.PDFLocation == nil || finding.PDFLocation.Page != 2 || finding.PDFLocation.Path != key {
					t.Fatal(finding)
				}
			}
		}
		if warnings != 1 {
			t.Fatal(key, "wrong omission count", result.Diagnostics)
		}
		pages := qpdfPages(t, output)
		if !reflect.DeepEqual(pages[0]["/"+key], []any{float64(10), float64(10), float64(90), float64(90)}) || pages[1]["/"+key] != nil {
			t.Fatal("printing box sharing changed", pages)
		}
		qpdfCheck(t, output)
	}
}
func TestSourceSizeSchemaDomainIncludesPositiveSubpointAndLargeDimensions(t *testing.T) {
	for _, size := range []float64{.25, 20000} {
		d := pdffixture.WithBoxes("physical", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: fmtBox(size)})
		result, _, _ := selectiveRequest(t, d.Bytes(), "", app.Check)
		if result.Status != "ok" || result.Parts[0].PageSizes == nil {
			t.Fatal(result.Diagnostics)
		}
		validatePhysicalReport(t, result)
		actual := (*result.Parts[0].PageSizes)[0].SourceSize
		if actual.Width != size || math.IsInf(actual.Height, 0) {
			t.Fatal(actual)
		}
	}
}

func fmtBox(side float64) string { return fmt.Sprintf("[0 0 %.17g %.17g]", side, side) }

func TestNumericDestinationGuardFollowsRetainedTargetAcrossTopologies(t *testing.T) {
	for _, topology := range []string{"direct", "indirect", "named", "legacy", "goto", "next", "outline", "unused-name"} {
		for _, targetKept := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/kept=%v", topology, targetKept), func(t *testing.T) {
				d := sharedLinkDoc(false)
				// One link origin, on the opposite page from its destination.
				target, origin := 7, 0
				if targetKept {
					target, origin = 3, 1
					d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte(" /Annots [6 0 R]"), nil)
				} else {
					d.Objs[6] = bytes.ReplaceAll(d.Objs[6], []byte(" /Annots [6 0 R]"), nil)
				}
				dest := fmt.Sprintf("[%d 0 R /XYZ 20 30 null]", target)
				entry := "/Dest " + dest
				switch topology {
				case "indirect":
					d.Objs = append(d.Objs, []byte(dest))
					entry = "/Dest 8 0 R"
				case "named", "unused-name":
					insertEntry(d, 1, "/Names << /Dests << /Names [(dest) "+dest+"] >> >>")
					entry = "/Dest (dest)"
					if topology == "unused-name" {
						entry = ""
					}
				case "legacy":
					insertEntry(d, 1, "/Dests << /dest "+dest+" >>")
					entry = "/Dest /dest"
				case "goto":
					entry = "/A << /S /GoTo /D " + dest + " >>"
				case "next":
					entry = "/A << /S /URI /URI (https://example.invalid/) /Next << /S /GoTo /D " + dest + " >> >>"
				case "outline":
					insertEntry(d, 1, "/Outlines 8 0 R")
					d.Objs = append(d.Objs, []byte("<< /Type /Outlines /First 9 0 R /Last 9 0 R /Count 1 >>"), []byte("<< /Title (target) /Parent 8 0 R /Dest "+dest+" >>"))
					entry = ""
				}
				insertEntry(d, 6, entry)
				result, _, output := selectiveRequest(t, d.Bytes(), "100x100", app.Build)
				if !targetKept {
					if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitDestinationUnsupported) {
						t.Fatal(result.Diagnostics)
					}
					return
				}
				if result.Status != "ok" {
					t.Fatal(result.Diagnostics)
				}
				qpdfCheck(t, output)
				if topology != "unused-name" && topology != "outline" {
					tools := oracle.RequireTools(t)
					doc, e := oracle.Load(tools, output)
					if e != nil {
						t.Fatal(e)
					}
					targets, e := doc.ActionTargets(origin + 1)
					if e != nil || !reflect.DeepEqual(targets, []int{1}) {
						t.Fatalf("wrong physical target %v %v", targets, e)
					}
				}
				// Every explicit XYZ tail remains unmodified while references are rebound.
				tails := 0
				visitQPDFValues(t, run(t, qpdfTool, "--json", output), func(v any) {
					if a, ok := v.([]any); ok && len(a) == 5 && a[1] == "/XYZ" {
						tails++
						if a[2] != float64(20) || a[3] != float64(30) || a[4] != nil {
							t.Fatal("coordinate tail changed", a)
						}
					}
				})
				if tails == 0 {
					t.Fatal("numeric destination silently dropped")
				}
			})
		}
	}
}
func TestStaticWidgetsRequireProvenKeptOwners(t *testing.T) {
	for _, mode := range []string{"kept", "transformed", "conflict", "unresolved"} {
		t.Run(mode, func(t *testing.T) {
			d := pdffixture.StaticForm("widget", "Helvetica", true)
			d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 8 0 R] >>")
			d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 700 900] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"))
			if mode == "transformed" {
				d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("[0 0 612 792]"), []byte("[0 0 700 900]"))
			}
			if mode == "conflict" {
				d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/P 3 0 R"), []byte("/P 8 0 R"))
			}
			if mode == "unresolved" {
				d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte(" /Annots [6 0 R]"), nil)
				d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte(" /P 3 0 R"), nil)
			}
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, output := selectiveRequest(t, d.Bytes(), "Letter", op)
				if mode == "kept" {
					if result.Status != "ok" {
						t.Fatal(result.Diagnostics)
					}
					if op == app.Build {
						qpdfCheck(t, output)
					}
				} else if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitFormUnsupported) {
					t.Fatal(mode, result.Diagnostics)
				}
			}
		})
	}
}

func visitQPDFValues(t *testing.T, raw []byte, visit func(any)) {
	t.Helper()
	var value any
	if e := json.Unmarshal(raw, &value); e != nil {
		t.Fatal(e)
	}
	var walk func(any)
	walk = func(v any) {
		visit(v)
		switch x := v.(type) {
		case map[string]any:
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
}

func TestFittingAnnotationScanStartsWithTransformPages(t *testing.T) {
	for _, secondSubtype := range []string{"Square", "FreeText"} {
		d := pdffixture.FitAnnotation("FreeText", "/Contents (kept) /DA (/F1 10 Tf 0 g)", false, 0, 4)
		d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 7 0 R] >>")
		d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [8 0 R] >>"), []byte("<< /Type /Annot /Subtype /"+secondSubtype+" /Rect [10 10 90 90] /F 4 /Contents (transformed) /DA (/F1 10 Tf 0 g) >>"))
		result, _, output := selectiveRequest(t, d.Bytes(), "100x100", app.Build)
		if secondSubtype == "Square" {
			if result.Status != "ok" {
				t.Fatal(result.Diagnostics)
			}
			qpdfCheck(t, output)
		} else {
			if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.FitAnnotationUnsupported) {
				t.Fatal(result.Diagnostics)
			}
			for _, finding := range result.Diagnostics {
				if finding.Code == diag.FitAnnotationUnsupported && (finding.PDFLocation == nil || finding.PDFLocation.Page != 2) {
					t.Fatal("wrong original page", finding)
				}
			}
		}
	}
}
func TestDistinctAnnotationDictionariesMayShareAppearancesAcrossKeepAndTransform(t *testing.T) {
	d := pdffixture.FitAnnotation("Square", "", true, 0, 4)
	d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 9 0 R] >>")
	d.Objs = append(d.Objs, bytes.Clone(d.Objs[5]), []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [8 0 R] >>"))
	result, source, output := selectiveRequest(t, d.Bytes(), "100x100", app.Build)
	if result.Status != "ok" {
		t.Fatal(result.Diagnostics)
	}
	qpdfCheck(t, output)
	if !reflect.DeepEqual(fitRaster(t, source, 1, 72), fitRaster(t, output, 1, 72)) {
		t.Fatal("shared AP mutation changed the kept page")
	}
}
func TestKeptMissingAppearanceStillObeysPDF20OutputPolicy(t *testing.T) {
	d := pdffixture.FitAnnotation("FreeText", "/Contents (kept) /DA (/F1 10 Tf 0 g)", false, 0, 4)
	other := pdffixture.WithBoxes("requires 2", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 100 100]"})
	other.Version = "2.0"
	for _, op := range []app.Operation{app.Check, app.Build} {
		dir := t.TempDir()
		one, two := filepath.Join(dir, "one.pdf"), filepath.Join(dir, "two.pdf")
		if e := d.WriteFile(one); e != nil {
			t.Fatal(e)
		}
		if e := other.WriteFile(two); e != nil {
			t.Fatal(e)
		}
		target, e := packet.ParseSize("100x100", false)
		if e != nil {
			t.Fatal(e)
		}
		r := app.Execute(t.Context(), app.Request{Operation: op, InspectionWorkers: 1, WorkingDir: dir, Fitting: packet.DefaultFitPolicy(target), Output: filepath.Join(dir, "out.pdf"), Input: app.Items{app.SourceItem{Ref: "one", Path: one}, app.SourceItem{Ref: "two", Path: two}}})
		if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.SourceOutputIncompatible) || r.Output.Published {
			t.Fatal(r.Diagnostics)
		}
		if r.Parts[0].PageSizes == nil {
			t.Fatal("known kept metrics were lost")
		}
	}
}
func TestCompletePageSizesSurviveRefusalAndLaterWritingFailure(t *testing.T) {
	for _, lateWrite := range []bool{false, true} {
		dir := t.TempDir()
		source := filepath.Join(dir, "source.pdf")
		width := 14400.005
		targetText := "14400x100"
		if lateWrite {
			width = 100
			targetText = "100x100"
		}
		d := pdffixture.WithBoxes("physical", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: fmt.Sprintf("[0 0 %.17g 100]", width)})
		if e := d.WriteFile(source); e != nil {
			t.Fatal(e)
		}
		target, e := packet.ParseSize(targetText, false)
		if e != nil {
			t.Fatal(e)
		}
		items := []packet.Item{&packet.SourceItem{Ref: "source", Path: source}}
		output := filepath.Join(dir, "output.pdf")
		if lateWrite {
			output = filepath.Join(dir, "absent", "output.pdf")
			items = append(items, &packet.PageItem{Ref: "body", Count: 1})
		} else {
			items = append(items, &packet.GroupItem{Ref: "odd", StartOn: packet.StartOdd, Items: []packet.Item{&packet.PageItem{Ref: "body", Count: 1, Style: packet.Defaults()}}})
		}
		r := app.Execute(t.Context(), app.Request{Operation: app.Build, InspectionWorkers: 1, WorkingDir: dir, Fitting: packet.DefaultFitPolicy(target), Output: output, Input: app.Declared{Declaration: packet.Declaration{Items: items}}})
		want := diag.PageSizeTooLarge
		if lateWrite {
			want = diag.OutputWriteFailed
		}
		if !codePresent(r.Diagnostics, want) || r.Output.Published || r.Parts[0].PageSizes == nil {
			t.Fatal(r.Diagnostics)
		}
		if lateWrite && (r.Parts[1].OutputSize == nil || *r.Parts[1].OutputSize != target) {
			t.Fatal("write failure lost resolved generated geometry", r.Parts)
		}
		if !lateWrite && (r.Pages.Total != nil || r.Parts[1].OutputSize != nil) {
			t.Fatal("failed Resolve exposed partial layout", r.Parts)
		}
		run := (*r.Parts[0].PageSizes)[0]
		if run.SourceSize.Width != width || run.OutputSize.Width != width {
			t.Fatal("known keep expectation changed", run)
		}
	}
}

func TestSelectedMetricKnowledgeAndOwnershipBeforeLayout(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	d := pdffixture.WithBoxes("runs", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 100 200]"}, pdffixture.PageBoxes{Media: "[0 0 100.001 200]"}, pdffixture.PageBoxes{Media: "[0 0 200 100]"})
	if e := d.WriteFile(source); e != nil {
		t.Fatal(e)
	}
	target, e := packet.ParseSize("100x200", false)
	if e != nil {
		t.Fatal(e)
	}
	selection := []packet.PageRange{{First: 1, Last: 1}, {First: 3, Last: 3}}
	r := app.Execute(t.Context(), app.Request{Operation: app.Check, InspectionWorkers: 1, WorkingDir: dir, Fitting: packet.DefaultFitPolicy(target), Input: app.Declared{Declaration: packet.Declaration{Items: []packet.Item{
		&packet.GroupItem{Ref: "nested", Items: []packet.Item{&packet.SourceItem{Ref: "first", Path: source, Selection: selection}}},
		&packet.SourceItem{Ref: "invalid", Path: source, Selection: []packet.PageRange{{First: 4, Last: 4}}},
		&packet.SourceItem{Ref: "repeat", Path: source, Selection: selection},
	}}}})
	if r.Status != "invalid" || len(r.Parts) != 3 || r.Parts[0].PageSizes == nil || r.Parts[1].PageSizes != nil || r.Parts[2].PageSizes == nil {
		t.Fatal(r)
	}
	runs := *r.Parts[0].PageSizes
	if len(runs) != 2 || runs[0].Source != [2]int{1, 1} || runs[1].Source != [2]int{3, 3} || runs[0].SourceSize != runs[0].OutputSize || runs[1].OutputSize != (packet.Size{Width: 100, Height: 200}) {
		t.Fatal(runs)
	}
	runs[0].SourceSize.Width++
	if (*r.Parts[2].PageSizes)[0].SourceSize.Width != 100 {
		t.Fatal("repeated report occurrences share mutable geometry")
	}
}
func TestNumberingPreflightUsesActualKeptSizeAtTightMargin(t *testing.T) {
	numbers := packet.DefaultPageNumbers()
	numbers.Position = packet.NumberBottomLeft
	numbers.Size = 9
	numbers.Margin = 10
	target := packet.Size{Width: 100, Height: 200}
	actual := packet.Size{Width: 99.995, Height: 200}
	footer, e := compose.NewPageNumberLayout(defaultFont(t), *numbers)
	if e != nil {
		t.Fatal(e)
	}
	placed, e := footer.Place(target, 1)
	if e != nil {
		t.Fatal(e)
	}
	right := math.Max(placed.Bounds.X+placed.Bounds.Width, placed.InkBounds.X+placed.InkBounds.Width)
	numbers.Margin += target.Width - right - .001
	footer, e = compose.NewPageNumberLayout(defaultFont(t), *numbers)
	if e != nil {
		t.Fatal(e)
	}
	if footer.Check(target, 1) != nil || footer.Check(actual, 1) == nil {
		t.Fatal("tight-margin control does not distinguish actual from nominal width")
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		dir := t.TempDir()
		source := filepath.Join(dir, "source.pdf")
		d := pdffixture.WithBoxes("tight", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 99.995 200]"})
		if e := d.WriteFile(source); e != nil {
			t.Fatal(e)
		}
		sheet, e := packet.ParseSize("100x200", false)
		if e != nil {
			t.Fatal(e)
		}
		r := app.Execute(t.Context(), app.Request{Operation: op, InspectionWorkers: 1, WorkingDir: dir, Fitting: packet.DefaultFitPolicy(sheet), Output: filepath.Join(dir, "out.pdf"), Input: app.Declared{Declaration: packet.Declaration{PageNumbers: numbers, Items: []packet.Item{&packet.SourceItem{Ref: "source", Path: source}}}}})
		if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.TextOverflow) || r.Output.Published {
			t.Fatal(op, r.Diagnostics)
		}
		if r.Parts[0].PageSizes == nil || (*r.Parts[0].PageSizes)[0].OutputSize != actual {
			t.Fatal("tight kept expectation lost", r.Parts)
		}
	}
}
func TestNearTargetContentsLinksStayInAuthoredCoordinatesAcrossMixedPages(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	d := pdffixture.WithBoxes("mixed", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 612 792]"}, pdffixture.PageBoxes{Media: "[0 0 600 800]"})
	if e := d.WriteFile(source); e != nil {
		t.Fatal(e)
	}
	numbers := packet.DefaultPageNumbers()
	numbers.Position = packet.NumberTopLeft
	numbers.Margin = 30
	declaration := packet.Declaration{PageNumbers: numbers, Items: []packet.Item{
		&packet.ContentsItem{Ref: "contents", Size: packet.Size{Width: 612.005, Height: 792.005}},
		&packet.GroupItem{Ref: "one", Title: "First", Items: []packet.Item{&packet.SourceItem{Ref: "mixed", Path: source}}},
		&packet.GroupItem{Ref: "two", Title: "Second", Items: []packet.Item{&packet.PageItem{Ref: "body", Count: 1, Style: packet.PageStyle{Size: packet.Setting[packet.Size]{Present: true, Value: packet.Size{Width: 300, Height: 400}}}}}},
	}}
	var controls [][4]float64
	for _, fit := range []bool{false, true} {
		request := app.Request{Operation: app.Build, InspectionWorkers: 1, WorkingDir: dir, Output: filepath.Join(dir, fmt.Sprintf("out-%t.pdf", fit)), Input: app.Declared{Declaration: declaration}}
		if fit {
			sheet, e := packet.ParseSize("Letter", false)
			if e != nil {
				t.Fatal(e)
			}
			request.Fitting = packet.DefaultFitPolicy(sheet)
		}
		r := app.Execute(t.Context(), request)
		if r.Status != "ok" {
			t.Fatal(r.Diagnostics)
		}
		qpdfCheck(t, request.Output)
		tools := oracle.RequireTools(t)
		doc, e := oracle.Load(tools, request.Output)
		if e != nil {
			t.Fatal(e)
		}
		targets, e := doc.ActionTargets(1)
		if e != nil || !reflect.DeepEqual(targets, []int{2, 4}) {
			t.Fatal(targets, e)
		}
		rects, e := doc.AnnotationRects(1)
		if e != nil {
			t.Fatal(e)
		}
		if !fit {
			controls = rects
		} else if !reflect.DeepEqual(rects, controls) {
			t.Fatal("kept contents links were scaled", rects, controls)
		}
		pages := qpdfPages(t, request.Output)
		if !reflect.DeepEqual(pages[0]["/MediaBox"], []any{float64(0), float64(0), 612.005, 792.005}) {
			t.Fatal("kept contents canvas changed", pages[0])
		}
	}
}

func validatePhysicalReport(t *testing.T, result app.Result) {
	t.Helper()
	var document, value any
	if err := json.Unmarshal(exchange.Schema, &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("result.json")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(exchange.Project(result))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(value); err != nil {
		t.Fatal("valid positive physical metrics rejected", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatal("result is not an object")
	}
	parts, ok := object["parts"].([]any)
	if !ok || len(parts) == 0 {
		t.Fatal("missing parts")
	}
	part, ok := parts[0].(map[string]any)
	if !ok {
		t.Fatal("missing part object")
	}
	complete := part["page_sizes"]
	part["page_sizes"] = []any{}
	if schema.Validate(value) == nil {
		t.Fatal("unknown geometry accepted as empty array")
	}
	part["page_sizes"] = complete
	runs, ok := complete.([]any)
	if !ok || len(runs) == 0 {
		t.Fatal("missing known geometry")
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatal("missing run object")
	}
	size, ok := run["source_size"].(map[string]any)
	if !ok {
		t.Fatal("missing physical dimensions")
	}
	size["width"] = float64(0)
	if schema.Validate(value) == nil {
		t.Fatal("zero source dimension accepted")
	}
}

func TestUnrepresentableSelectedTransformNamesOriginalSourcePage(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	tiny := strconv.FormatFloat(1e-306, 'f', -1, 64)
	d := pdffixture.WithBoxes("identity", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 100 100]"}, pdffixture.PageBoxes{Media: "[0 0 " + tiny + " " + tiny + "]", UserUnit: strconv.FormatFloat(1e308, 'f', -1, 64) + ".0"})
	if e := d.WriteFile(source); e != nil {
		t.Fatal(e)
	}
	target, e := packet.ParseSize("14400x14400", false)
	if e != nil {
		t.Fatal(e)
	}
	r := app.Execute(t.Context(), app.Request{Operation: app.Check, InspectionWorkers: 1, WorkingDir: dir, Fitting: packet.DefaultFitPolicy(target), Input: app.Declared{Declaration: packet.Declaration{Items: []packet.Item{&packet.SourceItem{Ref: "selected", Path: source, Selection: []packet.PageRange{{First: 2, Last: 2}}}}}}})
	found := false
	for _, d := range r.Diagnostics {
		if d.Code == diag.FitGeometryUnsupported {
			found = true
			if d.PDFLocation == nil || d.PDFLocation.Page != 2 {
				t.Fatalf("selected ordinal misreported as original page: %+v", d)
			}
		}
	}
	if !found {
		t.Fatal("representability refusal not exercised", r.Diagnostics)
	}
}
