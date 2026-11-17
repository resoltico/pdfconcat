// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func navigationEntry(doc *pdffixture.Doc, object int, entry string) {
	body := doc.Objs[object-1]
	end := bytes.LastIndex(body, []byte(">>"))
	doc.Objs[object-1] = append(append(append([]byte{}, body[:end]...), []byte(" "+entry+" ")...), body[end:]...)
}

// Every fixture starts with page objects 4 and 6. Selecting source page 2
// therefore checks original attribution independently of its output page 1.
func navigationProvenanceFixture(route string) (*pdffixture.Doc, []diag.PDFLocation) {
	doc := pdffixture.Pages("provenance", 2)
	annotation := func(body string) {
		navigationEntry(doc, 6, "/Annots [8 0 R]")
		doc.Objs = append(doc.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [0 0 20 20] /P 6 0 R "+body+" >>"))
	}
	switch route {
	case "direct-link":
		annotation("/Dest [4 0 R /Fit]")
		return doc, []diag.PDFLocation{{Object: 8, Page: 2, Path: "Dest"}}
	case "direct-action":
		annotation("/A << /S /GoTo /D [4 0 R /Fit] >>")
		return doc, []diag.PDFLocation{{Object: 8, Page: 2, Path: "A"}}
	case "indirect-action":
		annotation("/A 9 0 R")
		doc.Objs = append(doc.Objs, []byte("<< /S /GoTo /D [4 0 R /Fit] >>"))
		return doc, []diag.PDFLocation{{Object: 8, Page: 2, Path: "A"}}
	case "page-action":
		navigationEntry(doc, 6, "/AA << /O << /S /GoTo /D [4 0 R /Fit] >> >>")
		return doc, []diag.PDFLocation{{Object: 6, Page: 2, Path: "AA.O"}}
	case "indirect-aa":
		navigationEntry(doc, 6, "/AA 8 0 R")
		doc.Objs = append(doc.Objs, []byte("<< /O 9 0 R >>"), []byte("<< /S /GoTo /D [4 0 R /Fit] >>"))
		return doc, []diag.PDFLocation{{Object: 8, Page: 2, Path: "O"}}
	case "name-tree":
		navigationEntry(doc, 1, "/Names << /Dests 8 0 R >>")
		doc.Objs = append(doc.Objs, []byte("<< /Names [(drop) [4 0 R /Fit] (keep) [6 0 R /Fit]] >>"))
		return doc, []diag.PDFLocation{{Object: 8, Path: "Names[1]"}}
	case "legacy":
		navigationEntry(doc, 1, "/Dests 8 0 R")
		doc.Objs = append(doc.Objs, []byte("<< /drop [4 0 R /Fit] /keep [6 0 R /Fit] >>"))
		return doc, []diag.PDFLocation{{Object: 8, Path: `["drop"]`}}
	case "dual-definitions":
		navigationEntry(doc, 1, "/Names << /Dests 8 0 R >> /Dests 9 0 R")
		doc.Objs = append(doc.Objs, []byte("<< /Names [(drop) [4 0 R /Fit]] >>"), []byte("<< /drop [4 0 R /Fit] >>"))
		return doc, []diag.PDFLocation{{Object: 8, Path: "Names[1]"}, {Object: 9, Path: `["drop"]`}}
	case "outline":
		navigationEntry(doc, 1, "/Outlines 8 0 R")
		doc.Objs = append(doc.Objs, []byte("<< /Type /Outlines /First 9 0 R /Last 10 0 R /Count 2 >>"), []byte("<< /Title (drop) /Parent 8 0 R /Next 10 0 R /Dest [4 0 R /Fit] >>"), []byte("<< /Title (keep) /Parent 8 0 R /Prev 9 0 R /Dest [6 0 R /Fit] >>"))
		return doc, []diag.PDFLocation{{Object: 9, Path: "Dest"}}
	default:
		panic("unknown navigation fixture: " + route)
	}
}

func navigationFindings(t *testing.T, data []byte, requests []SelectionRequest) []diag.Diagnostic {
	t.Helper()
	_, _, ds := Inspect(t.Context(), data, packet.StructurePreserve, requests)
	if outcome := diag.Outcome(ds); outcome != "ok" {
		t.Fatalf("inspection: %+v", ds)
	}
	return slices.DeleteFunc(ds, func(d diag.Diagnostic) bool { return d.Code != diag.SourceNavigationRemoved })
}

func assertNavigationSites(t *testing.T, findings []diag.Diagnostic, item string, want []diag.PDFLocation) {
	t.Helper()
	got := []diag.PDFLocation{}
	for _, finding := range findings {
		if !slices.Equal(finding.Items, []string{item}) {
			t.Errorf("wrong occurrence attribution: %+v", finding)
		}
		if finding.PDFLocation == nil {
			t.Errorf("missing source location: %+v", finding)
			continue
		}
		if finding.Detail == "" {
			t.Errorf("missing mutation evidence: %+v", finding)
		}
		got = append(got, *finding.PDFLocation)
	}
	less := func(a, b diag.PDFLocation) int {
		return strings.Compare(fmt.Sprintf("%09d/%09d/%s", a.Object, a.Page, a.Path), fmt.Sprintf("%09d/%09d/%s", b.Object, b.Page, b.Path))
	}
	slices.SortFunc(got, less)
	want = slices.Clone(want)
	slices.SortFunc(want, less)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source sites: got %+v; want %+v", got, want)
	}
}

func assertNavigationTargetEvidence(t *testing.T, findings []diag.Diagnostic, target string) {
	t.Helper()
	for _, finding := range findings {
		if target != "" && !strings.Contains(finding.Detail, target) {
			t.Errorf("missing original target binding %q: %+v", target, finding)
		}
		if !strings.Contains(finding.Detail, "Target original source page 1 is not retained.") {
			t.Errorf("missing original target page 1: %+v", finding)
		}
	}
}

func TestNavigationWarningsIdentifyOriginalChangedBindings(t *testing.T) {
	for _, route := range []string{"direct-link", "direct-action", "indirect-action", "page-action", "indirect-aa", "name-tree", "legacy", "dual-definitions", "outline"} {
		t.Run(route, func(t *testing.T) {
			doc, want := navigationProvenanceFixture(route)
			findings := navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "page-two", Pages: []packet.PageRange{{First: 2, Last: 2}}}})
			assertNavigationSites(t, findings, "page-two", want)
			target := map[string]string{"direct-action": "source object 8 0 A.D", "indirect-action": "source object 9 0 D", "page-action": "source object 6 0 AA.O.D", "indirect-aa": "source object 9 0 D"}[route]
			assertNavigationTargetEvidence(t, findings, target)
			if got := navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "whole"}}); len(got) != 0 {
				t.Fatalf("valid whole-source navigation reported removed: %+v", got)
			}
		})
	}
}

func TestNavigationWarningsIgnoreBindingsOnOmittedPages(t *testing.T) {
	doc, _ := navigationProvenanceFixture("direct-link")
	// The annotation belongs only to omitted page 2; its target page 1 survives.
	if got := navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "page-one", Pages: []packet.PageRange{{First: 1, Last: 1}}}}); len(got) != 0 {
		t.Fatalf("omitted annotation was described as a retained mutation: %+v", got)
	}
}

func TestNavigationWarningsDistinguishSharedActionEdges(t *testing.T) {
	doc := pdffixture.Pages("shared-provenance", 2)
	navigationEntry(doc, 6, "/Annots [8 0 R] /AA << /O 9 0 R /C 9 0 R >>")
	doc.Objs = append(doc.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [0 0 20 20] /P 6 0 R /A 9 0 R >>"), []byte("<< /S /GoTo /D [4 0 R /Fit] >>"))
	want := []diag.PDFLocation{{Object: 6, Page: 2, Path: "AA.O"}, {Object: 6, Page: 2, Path: "AA.C"}, {Object: 8, Page: 2, Path: "A"}}
	findings := navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "shared", Pages: []packet.PageRange{{First: 2, Last: 2}}}})
	assertNavigationSites(t, findings, "shared", want)
	assertNavigationTargetEvidence(t, findings, "source object 9 0 D")
}

func TestNavigationWarningsEncodeByteNamesWithoutLosingDefinitionSites(t *testing.T) {
	doc, want := navigationProvenanceFixture("name-tree")
	doc.Objs[7] = []byte("<< /Names [<ff2f22> [4 0 R /Fit] (keep) [6 0 R /Fit]] >>")
	findings := navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "byte-name", Pages: []packet.PageRange{{First: 2, Last: 2}}}})
	assertNavigationSites(t, findings, "byte-name", want)
	encoded, err := json.Marshal(findings)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("byte-name evidence is not valid JSON: %q %v", encoded, err)
	}
}

func TestNavigationWarningsLocatePopupParentAction(t *testing.T) {
	doc := pdffixture.Pages("popup-provenance", 2)
	navigationEntry(doc, 6, "/Annots [9 0 R]")
	doc.Objs = append(doc.Objs, []byte("<< /Type /Annot /Subtype /Text /Rect [0 0 20 20] /P 6 0 R /Popup 9 0 R /AA << /E << /S /GoTo /D [4 0 R /Fit] >> >> >>"), []byte("<< /Type /Annot /Subtype /Popup /Rect [0 0 20 20] /P 6 0 R /Parent 8 0 R >>"))
	want := []diag.PDFLocation{{Object: 8, Page: 2, Path: "AA.E"}}
	assertNavigationSites(t, navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "popup", Pages: []packet.PageRange{{First: 2, Last: 2}}}}), "popup", want)
}

func TestNavigationWarningsLocateFieldOnlyActionWithoutInventingPage(t *testing.T) {
	doc := pdffixture.Form("field-provenance")
	// An otherwise valid page object outside the source page tree is not a
	// navigation target. Whole-source form selection remains supported.
	doc.Objs = append(doc.Objs, bytes.Clone(doc.Objs[3]))
	navigationEntry(doc, 20, "/AA << /K << /S /GoTo /D [24 0 R /Fit] >> >>")
	want := []diag.PDFLocation{{Object: 20, Path: "AA.K"}}
	assertNavigationSites(t, navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "field"}}), "field", want)
}

func TestNavigationWarningsRetainNonzeroGenerationIdentity(t *testing.T) {
	doc, want := navigationProvenanceFixture("indirect-action")
	data := bytes.ReplaceAll(doc.Bytes(), []byte("9 0 R"), []byte("9 1 R"))
	data = bytes.Replace(data, []byte("9 0 obj"), []byte("9 1 obj"), 1)
	lines := bytes.Split(data, []byte{'\n'})
	for i, line := range lines {
		if string(line) == "xref" {
			lines[i+2+9] = bytes.Replace(lines[i+2+9], []byte("00000 n"), []byte("00001 n"), 1)
			break
		}
	}
	findings := navigationFindings(t, bytes.Join(lines, []byte{'\n'}), []SelectionRequest{{Item: "generation-one", Pages: []packet.PageRange{{First: 2, Last: 2}}}})
	assertNavigationSites(t, findings, "generation-one", want)
	assertNavigationTargetEvidence(t, findings, "source object 9 1 D")
}

func TestNavigationWarningsKeepIndependentNextSiblings(t *testing.T) {
	doc := pdffixture.Pages("next-provenance", 2)
	navigationEntry(doc, 6, "/AA << /O << /S /GoTo /D [6 0 R /Fit] /Next 8 0 R >> >>")
	doc.Objs = append(doc.Objs, []byte("[<< /S /GoTo /D [4 0 R /Fit] /Next << /S /URI /URI (https://example.invalid/dropped-chain) >> >> << /S /GoTo /D [6 0 R /Fit] >> << /S /URI /URI (https://example.invalid/sibling) >>]"))
	want := []diag.PDFLocation{{Object: 8, Page: 2, Path: "[0]"}}
	assertNavigationSites(t, navigationFindings(t, doc.Bytes(), []SelectionRequest{{Item: "next", Pages: []packet.PageRange{{First: 2, Last: 2}}}}), "next", want)
	path, ds := selectedPacket(t, doc, []packet.Item{&packet.SourceItem{Ref: "next", Selection: []packet.PageRange{{First: 2, Last: 2}}}}, nil)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := output.ActionTargets(1)
	if err != nil || !slices.Equal(targets, []int{1, 1}) {
		t.Fatalf("independent targets changed: %v %v", targets, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The deterministic writer leaves URI string bytes directly inspectable.
	if bytes.Contains(raw, []byte("dropped-chain")) || !bytes.Contains(raw, []byte("sibling")) {
		t.Fatal("dependent chain or independent URI retention changed")
	}
}

func TestNavigationWarningsRepeatOriginalSitesThroughAssemblyAndFitting(t *testing.T) {
	doc, want := navigationProvenanceFixture("direct-action")
	path := filepath.Join(t.TempDir(), "source.pdf")
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	requests := []SelectionRequest{{Item: "first", Pages: []packet.PageRange{{First: 2, Last: 2}}}, {Item: "whole"}, {Item: "again", Pages: []packet.PageRange{{First: 2, Last: 2}}}}
	facts, views := inspectSelection(t, doc, requests)
	parts := []packet.Item{&packet.SourceItem{Ref: "first", Path: path, Selection: requests[0].Pages}, &packet.SourceItem{Ref: "whole", Path: path}, &packet.SourceItem{Ref: "again", Path: path, Selection: requests[2].Pages}}
	domain := map[string]packet.SourceFacts{}
	for item, view := range views {
		domain[item] = view.SourceFacts
	}
	fit := &packet.Size{Width: 300, Height: 400}
	layout, ds := resolvePacket(pdffixture.Plan(t, packet.Declaration{Output: "", FitTo: fit, PageNumbers: nil, PageDefaults: packet.PageStyle{}, Items: parts}), domain)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	var output bytes.Buffer
	assembled, err := Assemble(t.Context(), testAssembly(t, layout, map[string]SourceFacts{path: facts}, views, fit), &output)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"first", "again"} {
		findings := slices.DeleteFunc(slices.Clone(assembled.Diagnostics), func(d diag.Diagnostic) bool {
			return d.Code != diag.SourceNavigationRemoved || !slices.Contains(d.Items, item)
		})
		assertNavigationSites(t, findings, item, want)
		assertNavigationTargetEvidence(t, findings, "source object 8 0 A.D")
		for _, finding := range findings {
			if finding.File != path {
				t.Errorf("wrong original file: %+v", finding)
			}
		}
	}
}
