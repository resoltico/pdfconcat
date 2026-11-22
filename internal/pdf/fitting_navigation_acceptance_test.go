// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func landscapeDestination(operands string, rotation int, inherited bool) *pdffixture.Doc {
	d := localLink(operands, true)
	box := "[0 0 800 400]"
	if inherited {
		box = "[-20 -40 780 360]"
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte(" /MediaBox [0 0 612 792]"), nil)
		insertEntry(d, 2, "/MediaBox "+box)
		insertEntry(d, 3, "/UserUnit 2")
	} else {
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("[0 0 612 792]"), []byte(box))
	}
	if rotation != 0 {
		insertEntry(d, 2, fmt.Sprintf("/Rotate %d", rotation))
	}
	return d
}

func TestFittedDestinationDefaultUserSpace(t *testing.T) {
	for _, test := range []struct {
		input     string
		want      []any
		rotation  int
		inherited bool
	}{
		{"/XYZ 40 300 0", []any{"/XYZ", 20.0, 350.0, 0.0}, 0, false},
		{"/XYZ 40 300 2", []any{"/XYZ", 275.0, 570.0, 2.0}, 90, false},
		{"/XYZ null 300 null", []any{"/XYZ", 275.0, nil, nil}, 90, false},
		{"/XYZ 40 null 0", []any{"/XYZ", nil, 570.0, 0.0}, 90, false},
		{"/XYZ 40 300 null", []any{"/XYZ", 380.0, 250.0, nil}, 180, false},
		{"/XYZ 40 300 0", []any{"/XYZ", 125.0, 30.0, 0.0}, 270, false},
		{"/XYZ -40 500 0", []any{"/XYZ", -20.0, 450.0, 0.0}, 0, false},
		{"/XYZ 40 300 null", []any{"/XYZ", 30.0, 370.0, nil}, 0, true},
		{"/FitR 40 100 200 300", []any{"/FitR", 20.0, 250.0, 100.0, 350.0}, 0, false},
		{"/FitR 40 100 200 300", []any{"/FitR", 125.0, 450.0, 275.0, 570.0}, 90, false},
		{"/FitR 40 100 200 300", []any{"/FitR", 300.0, 250.0, 380.0, 350.0}, 180, false},
		{"/FitR 40 100 200 300", []any{"/FitR", 125.0, 30.0, 275.0, 150.0}, 270, false},
	} {
		t.Run(fmt.Sprintf("%s/%d/inherited=%v", test.input, test.rotation, test.inherited), func(t *testing.T) {
			d := landscapeDestination(test.input, test.rotation, test.inherited)
			policy := packet.DefaultFitPolicy(packet.Size{Width: 400, Height: 600})
			path, _, findings := assembleDocs(t, []*pdffixture.Doc{d}, policy)
			if diag.Outcome(findings) != "ok" || !codePresent(findings, diag.FitDestinationMapped) {
				t.Fatal(findings)
			}
			output, err := oracle.Load(oracle.RequireTools(t), path)
			if err != nil {
				t.Fatal(err)
			}
			page, got, err := output.AnnotationDestination(1, 0)
			if err != nil || page != 1 || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("destination on page %d: %v, want %v (%v)", page, got, test.want, err)
			}
		})
	}
}

func TestFittingDestinationsRefuseUnsafeOperands(t *testing.T) {
	for _, test := range []struct {
		input    string
		rotation int
		fit      string
	}{
		{"/FitH 20", 90, "400x600"},
		{"/FitR 200 100 40 300", 0, "400x600"},
		{"/FitR 40 100 40 300", 0, "400x600"},
		{"/FitR 0 0 40 1" + strings.Repeat("0", 308) + ".0", 0, "8000x6000"},
		{"/FitR 0 0 40 0." + strings.Repeat("0", 300) + "1", 0, "400x600"},
	} {
		t.Run(test.input, func(t *testing.T) {
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, _ := selectiveRequest(t, landscapeDestination(test.input, test.rotation, false).Bytes(), test.fit, op)
				if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitDestinationUnsupported) || r.Output.Published {
					t.Fatal(r.Diagnostics)
				}
			}
		})
	}
}

func TestFittingLeavesPrivateAliasOfDestinationArray(t *testing.T) {
	d := landscapeDestination("/XYZ 40 300 0", 0, false)
	insertEntry(d, 3, "/Private 7 0 R")
	d.Objs[6] = []byte("[3 0 R /XYZ 8 0 R 300 0]")
	d.Objs = append(d.Objs, []byte("40"))
	for _, test := range []struct {
		fit  string
		want []any
	}{
		{"", []any{"/XYZ", 40.0, 300.0, 0.0}},
		{"400x600", []any{"/XYZ", 20.0, 350.0, 0.0}},
	} {
		r, _, out := selectiveRequest(t, d.Bytes(), test.fit, app.Build)
		if r.Status != "ok" {
			t.Fatal(r.Diagnostics)
		}
		private, ok := qpdfPages(t, out)[0]["/Private"].([]any)
		if !ok || len(private) != 5 || private[3] != float64(300) {
			t.Fatal("private array was remapped", private)
		}
		if _, indirect := private[2].(string); !indirect {
			t.Fatal("the private array lost its indirect operand", private)
		}
		o, err := oracle.Load(oracle.RequireTools(t), out)
		if err != nil {
			t.Fatal(err)
		}
		page, tail, err := o.AnnotationDestination(1, 0)
		if err != nil || page != 1 || !reflect.DeepEqual(tail, test.want) {
			t.Fatal(page, tail, err)
		}
	}
}

func TestRetainedStructureDestinationRefusal(t *testing.T) {
	for _, carrier := range []string{"name-tree", "legacy", "action", "direct"} {
		t.Run(carrier, func(t *testing.T) {
			d := landscapeDestination("/Fit", 0, false)
			d.Version = "2.0"
			d.Objs = append(d.Objs, []byte("<< /Type /StructElem /S /Document /P 9 0 R /Pg 3 0 R >>"), []byte("<< /Type /StructTreeRoot /K 8 0 R >>"))
			insertEntry(d, 1, "/StructTreeRoot 9 0 R")
			switch carrier {
			case "name-tree":
				insertEntry(d, 1, "/Names << /Dests << /Names [(test) << /D [3 0 R /Fit] /SD [8 0 R /Fit] >>] >> >>")
			case "legacy":
				insertEntry(d, 1, "/Dests << /test << /D [3 0 R /Fit] /SD [8 0 R /Fit] >> >>")
			case "direct":
				d.Objs[6] = []byte("[8 0 R /Fit]")
			case "action":
				d.Objs[5] = []byte("<< /Subtype /Link /Rect [10 20 30 40] /A << /S /GoTo /D [3 0 R /Fit] /SD [8 0 R /Fit] >> >>")
			}
			_, _, findings := pdf.Inspect(t.Context(), d.Bytes(), "", nil)
			if !codePresent(findings, diag.SourceStructureDestination) {
				t.Fatal(findings)
			}
			err := pdf.Verify(t.Context(), d.Bytes(), 1)
			if err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid ||
				!strings.Contains(pdf.Diagnostic(err).Detail, "retained structure destination") {
				t.Fatalf("serialized structure destination passed: %v", err)
			}
		})
	}
}

func TestFittingPreservesLargeIntegerDestinationCoordinates(t *testing.T) {
	for _, test := range []struct {
		input string
		mode  string
		want  []string
	}{
		{"/XYZ 9007199254740993 300 9007199254740993", "/XYZ", []string{"9007199254740992", "400", "9007199254740993"}},
		{"/FitV 9007199254740993", "/FitV", []string{"9007199254740992"}},
		{"/FitH 9007199254740993", "/FitH", []string{"9007199254741093"}},
		{"/FitR 9007199254740992 20 9007199254740993 55", "/FitR", []string{"9007199254740991", "120", "9007199254740992", "155"}},
	} {
		t.Run(test.mode, func(t *testing.T) {
			doc := localLink(test.input, false)
			doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("[0 0 612 792]"), []byte("[1 0 301 400]"))
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, output := selectiveRequest(t, doc.Bytes(), "300x600pt", op)
				if result.Status != "ok" || result.Output.Published != (op == app.Build) || !codePresent(result.Diagnostics, diag.FitDestinationMapped) {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					checkLargeDestination(t, output, test.mode, test.want)
				}
			}
		})
	}
	doc := localLink("/XYZ 9007199254740993 300 0", false)
	doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("[0 0 612 792]"), []byte("[0 0 300 400]"))
	for _, op := range []app.Operation{app.Check, app.Build} {
		r, _, _ := selectiveRequest(t, doc.Bytes(), "150x300pt", op)
		if r.Status != "invalid" || r.Output.Published || !codePresent(r.Diagnostics, diag.FitDestinationUnsupported) {
			t.Fatal(r.Diagnostics)
		}
	}
	result, _, output := selectiveRequest(t, doc.Bytes(), "300x400pt", app.Build)
	if result.Status != "ok" || codePresent(result.Diagnostics, diag.FitDestinationMapped) {
		t.Fatal(result.Diagnostics)
	}
	checkLargeDestination(t, output, "/XYZ", []string{"9007199254740993", "300", "0"})
}

func TestFittingPreservesLargeRealDestinationCoordinates(t *testing.T) {
	for _, test := range []struct {
		name, input, mode, frame, target string
		want                             []string
	}{
		{"xyz", "/XYZ 9007199254740994.0 300 9007199254740994.0", "/XYZ", "[1 0 301 400]", "300x600pt", []string{"9007199254740993", "400", "9007199254740994"}},
		{"axis", "/FitV 9007199254740994.0", "/FitV", "[1 0 301 400]", "300x600pt", []string{"9007199254740993"}},
		{"rectangle", "/FitR 9007199254740994.0 20 9007199254740996.0 55", "/FitR", "[1 0 301 400]", "300x600pt", []string{"9007199254740993", "120", "9007199254740995", "155"}},
		{"from-smaller-real", "/XYZ 4503599627370497.0 300 0", "/XYZ", "[0.5 0 300.5 400]", "600x1000pt", []string{"9007199254740993", "700", "0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := localLink(test.input, false)
			doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("[0 0 612 792]"), []byte(test.frame))
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, output := selectiveRequest(t, doc.Bytes(), test.target, op)
				if result.Status != "ok" || result.Output.Published != (op == app.Build) || !codePresent(result.Diagnostics, diag.FitDestinationMapped) {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					checkLargeDestination(t, output, test.mode, test.want)
				}
			}
		})
	}

	doc := localLink("/XYZ 9007199254740994.0 300 0", false)
	doc.Objs[2] = bytes.ReplaceAll(doc.Objs[2], []byte("[0 0 612 792]"), []byte("[0.5 0 300.5 400]"))
	for _, op := range []app.Operation{app.Check, app.Build} {
		result, _, _ := selectiveRequest(t, doc.Bytes(), "300x600pt", op)
		if result.Status != "invalid" || result.Output.Published || !codePresent(result.Diagnostics, diag.FitDestinationUnsupported) {
			t.Fatal(result.Diagnostics)
		}
	}
	result, _, output := selectiveRequest(t, doc.Bytes(), "300x400pt", app.Build)
	if result.Status != "ok" || codePresent(result.Diagnostics, diag.FitDestinationMapped) {
		t.Fatal(result.Diagnostics)
	}
	checkLargeDestination(t, output, "/XYZ", []string{"9007199254740994", "300", "0"})
}

func checkLargeDestination(t *testing.T, output, mode string, want []string) {
	t.Helper()
	data := run(t, qpdfTool, "--json", output)
	// The QPDF object table also includes scalar and array objects.
	var document struct {
		QPDF []map[string]json.RawMessage
	}
	if err := json.Unmarshal(data, &document); err != nil || len(document.QPDF) != 2 {
		t.Fatal(err)
	}
	for _, raw := range document.QPDF[1] {
		var object struct {
			Value struct {
				Subtype string            `json:"/Subtype"`
				Dest    []json.RawMessage `json:"/Dest"`
			}
		}
		if err := json.Unmarshal(raw, &object); err != nil || object.Value.Subtype != "/Link" {
			continue
		}
		dest := object.Value.Dest
		if len(dest) != len(want)+2 || string(dest[1]) != strconv.Quote(mode) {
			t.Fatal(dest)
		}
		for i, expected := range want {
			got, ok := new(big.Rat).SetString(string(dest[i+2]))
			if !ok || got.RatString() != expected {
				t.Fatalf("operand %d got %s, want %s", i, dest[i+2], expected)
			}
		}
		return
	}
	t.Fatal("qpdf found no local link")
}
