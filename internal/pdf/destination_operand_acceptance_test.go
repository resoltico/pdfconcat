// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func destinationOperandFixture(carrier, tail, object string) *pdffixture.Doc {
	d := localLink("/XYZ 100 300 0", false)
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("[0 0 612 792]"), []byte("[1 0 301 400]"))
	d.Objs = append(d.Objs, []byte(object), []byte("/XYZ"), []byte("2"))
	array := "[3 0 R " + tail + "]"
	link := "<< /Type /Annot /Subtype /Link /Rect [10 20 30 40] "
	switch carrier {
	case "link":
		d.Objs[5] = []byte(link + "/Dest " + array + " >>")
	case "goto":
		d.Objs[5] = []byte(link + "/A << /S /GoTo /D " + array + " >> >>")
	case "outline":
		insertEntry(d, 1, "/Outlines 10 0 R")
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Annots [6 0 R]"), nil)
		d.Objs = append(d.Objs, []byte("<< /Type /Outlines /First 11 0 R /Last 11 0 R /Count 1 >>"),
			[]byte("<< /Title (Target) /Parent 10 0 R /Dest "+array+" >>"))
	case "name-tree":
		insertEntry(d, 1, "/Names << /Dests << /Names [(target) << /D "+array+" >>] >> >>")
		d.Objs[5] = []byte(link + "/Dest (target) >>")
	case "legacy":
		insertEntry(d, 1, "/Dests << /target "+array+" >>")
		d.Objs[5] = []byte(link + "/Dest /target >>")
	}
	return d
}

func destinationOperandGenerationOne(data []byte) []byte {
	// Keep offsets stable and change both the object header and xref identity.
	data = bytes.Replace(data, []byte("\n7 0 obj"), []byte("\n7 1 obj"), 1)
	lines := bytes.Split(data, []byte{'\n'})
	for i, line := range lines {
		if string(line) == "xref" {
			lines[i+2+7] = bytes.Replace(lines[i+2+7], []byte("00000 n"), []byte("00001 n"), 1)
			break
		}
	}
	return bytes.Join(lines, []byte{'\n'})
}

func assertExplicitTail(t *testing.T, path string, want []any) {
	t.Helper()
	raw := run(t, qpdfTool, "--json", path)
	var physical struct {
		Pages []struct{ Object string } `json:"pages"`
	}
	if err := json.Unmarshal(raw, &physical); err != nil || len(physical.Pages) != 1 {
		t.Fatal(err, physical)
	}
	count := 0
	visitQPDFValues(t, raw, func(v any) {
		a, ok := v.([]any)
		if !ok || len(a) < 2 || !reflect.DeepEqual(a[1], want[0]) {
			return
		}
		count++
		if a[0] != physical.Pages[0].Object || !reflect.DeepEqual(a[1:], want) {
			t.Fatalf("qpdf found %v, want target %s with %v", a, physical.Pages[0].Object, want)
		}
	})
	if count == 0 {
		t.Fatal("qpdf found no explicit destination")
	}
}

func TestLocalDestinationOperandsResolveWithFullIdentity(t *testing.T) {
	for _, carrier := range []string{"link", "goto", "outline", "name-tree", "legacy"} {
		for _, test := range []struct {
			name, reference, object string
			generationOne           bool
			kept, fitted            any
		}{
			{"numeric", "7 0 R", "100", false, 100.0, 99.0},
			{"valid-generation-one", "7 1 R", "100", true, 100.0, 99.0},
			{"indirect-null", "7 0 R", "null", false, nil, nil},
			{"missing", "20 0 R", "100", false, nil, nil},
			{"wrong-generation", "7 1 R", "100", false, nil, nil},
			{"wrong-generation-nonnumeric-current-object", "7 1 R", "(unrelated)", false, nil, nil},
		} {
			t.Run(carrier+"/"+test.name, func(t *testing.T) {
				doc := destinationOperandFixture(carrier, "/XYZ "+test.reference+" 300 9 0 R", test.object)
				data := doc.Bytes()
				if test.generationOne {
					data = destinationOperandGenerationOne(data)
				}
				for _, fit := range []struct {
					target string
					x, y   any
				}{{"", test.kept, 300.0}, {"300x600pt", test.fitted, 400.0}} {
					for _, op := range []app.Operation{app.Check, app.Build} {
						result, _, output := selectiveRequest(t, data, fit.target, op)
						if result.Status != "ok" || result.Output.Published != (op == app.Build) {
							t.Fatal(result.Diagnostics)
						}
						if op == app.Build {
							assertExplicitTail(t, output, []any{"/XYZ", fit.x, fit.y, 2.0})
						}
					}
				}
			})
		}
	}
}

func TestLocalDestinationIndirectModesAndNullSlots(t *testing.T) {
	for _, test := range []struct {
		input string
		want  []any
	}{
		{"8 0 R 7 0 R 9 1 R 20 0 R", []any{"/XYZ", nil, nil, nil}},
		{"/FitH 7 0 R", []any{"/FitH", nil}},
	} {
		doc := destinationOperandFixture("link", test.input, "null")
		for _, fit := range []string{"", "300x600pt"} {
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, output := selectiveRequest(t, doc.Bytes(), fit, op)
				if result.Status != "ok" || result.Output.Published != (op == app.Build) ||
					codePresent(result.Diagnostics, diag.FitDestinationMapped) {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					assertExplicitTail(t, output, test.want)
				}
			}
		}
	}
}

func TestLocalDestinationRefusesNullRequiredSlots(t *testing.T) {
	for _, tail := range []string{
		"8 1 R 100 300 0", "20 0 R", "7 0 R",
		"/FitR 7 1 R 10 200 300", "/FitR 7 0 R 10 200 300", "/FitR 20 0 R 10 200 300",
		"/XYZ 100 300",
	} {
		t.Run(tail, func(t *testing.T) {
			for _, carrier := range []string{"link", "name-tree"} {
				doc := destinationOperandFixture(carrier, tail, "null")
				for _, op := range []app.Operation{app.Check, app.Build} {
					result, _, _ := selectiveRequest(t, doc.Bytes(), "", op)
					if result.Status != "invalid" || result.Output.Published ||
						!codePresent(result.Diagnostics, diag.SourceUnsupported) {
						t.Fatal(result.Diagnostics)
					}
				}
			}
		})
	}
}

func TestLocalDestinationNormalizationLeavesRemoteOperands(t *testing.T) {
	doc := destinationOperandFixture("goto", "/XYZ 7 0 R 300 0", "100")
	doc.Objs[5] = bytes.Replace(doc.Objs[5], []byte("/D [3 0 R /XYZ 7 0 R 300 0]"),
		[]byte("/D [3 0 R /XYZ 7 0 R 300 0] /Next << /S /GoToR /F (remote.pdf) /D [0 /XYZ 7 0 R 300 0] >>"), 1)
	result, _, output := selectiveRequest(t, doc.Bytes(), "300x600pt", app.Build)
	if result.Status != "ok" {
		t.Fatal(result.Diagnostics)
	}
	var local, remote bool
	visitQPDFValues(t, run(t, qpdfTool, "--json", output), func(v any) {
		d, ok := v.(map[string]any)
		if !ok {
			return
		}
		switch d["/S"] {
		case "/GoTo":
			a, ok := d["/D"].([]any)
			local = ok && len(a) == 5 && reflect.DeepEqual(a[1:], []any{"/XYZ", 99.0, 400.0, 0.0})
		case "/GoToR":
			a, ok := d["/D"].([]any)
			if !ok || len(a) != 5 {
				t.Fatal(d)
			}
			ref, indirect := a[2].(string)
			remote = indirect && ref != "" && a[0] == float64(0) && a[3] == float64(300)
		}
	})
	if !local || !remote {
		t.Fatalf("qpdf found local mapping=%v, remote operands preserved=%v", local, remote)
	}
}

func TestLocalDestinationNormalizationLeavesPrivateNameList(t *testing.T) {
	doc := destinationOperandFixture("name-tree", "/XYZ 7 0 R 300 0", "100")
	doc.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /Names << /Dests 11 0 R >> >>")
	doc.Objs = append(doc.Objs, []byte("[(target) << /D [3 0 R /XYZ 7 0 R 300 0] >>]"),
		[]byte("<< /Names 10 0 R >>"))
	insertEntry(doc, 3, "/Private 10 0 R /PrivateNode 11 0 R")
	result, _, output := selectiveRequest(t, doc.Bytes(), "", app.Build)
	if result.Status != "ok" {
		t.Fatal(result.Diagnostics)
	}
	assertLocalTargets(t, output, 1)
	page := qpdfPages(t, output)[0]
	entries, ok := page["/Private"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatal(page)
	}
	definition, ok := entries[1].(map[string]any)
	if !ok {
		t.Fatal(entries)
	}
	destination, ok := definition["/D"].([]any)
	if !ok || len(destination) != 5 {
		t.Fatal(definition)
	}
	if _, indirect := destination[2].(string); !indirect {
		t.Fatal("the private name list lost its indirect operand", definition)
	}
	node, ok := page["/PrivateNode"].(map[string]any)
	if !ok {
		t.Fatal(page)
	}
	if _, indirect := node["/Names"].(string); !indirect {
		t.Fatal("the private name tree node lost its original list reference", node)
	}
}
