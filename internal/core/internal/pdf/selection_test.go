// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func inspectSelection(t *testing.T, doc *pdffixture.Doc, requests []SelectionRequest) (SourceFacts, map[string]OccurrenceFacts) {
	t.Helper()
	source, occurrences, ds := InspectSelections(t.Context(), doc.Bytes(), requests)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatalf("inspection: %+v", ds)
	}
	return source, occurrences
}
func selectedPacket(t *testing.T, doc *pdffixture.Doc, parts []packet.Part, fit *packet.Size) (string, []diag.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.pdf")
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	var replace func([]packet.Part)
	requests := []SelectionRequest{}
	replace = func(parts []packet.Part) {
		for i := range parts {
			if parts[i].Source != "" {
				parts[i].Source = path
				requests = append(requests, SelectionRequest{Item: parts[i].Pointer, Pages: parts[i].Selection})
			}
			replace(parts[i].Items)
		}
	}
	replace(parts)
	facts, occurrences := inspectSelection(t, doc, requests)
	domain := map[string]packet.SourceFacts{}
	for item, f := range occurrences {
		domain[item] = f.SourceFacts
	}
	layout, ds := packet.Resolve(packet.Job{Parts: parts, FitTo: fit}, domain, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	var output bytes.Buffer
	_, err := Assemble(t.Context(), layout, map[string]SourceFacts{path: facts}, occurrences, nil, fit, nil, &output)
	if err != nil {
		return "", []diag.Diagnostic{Diagnostic(err)}
	}
	target := filepath.Join(dir, "packet.pdf")
	if err := os.WriteFile(target, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(t.Context(), target, layout.TotalPages); err != nil {
		t.Fatal(err)
	}
	return target, nil
}

func TestGroupedSelectionsRetainIndependentGeometryAndIdentity(t *testing.T) {
	doc := pdffixture.WithBoxes("select", pdffixture.PageBoxes{Media: "[10 20 410 620]", Crop: "[20 40 390 600]", Rotate: "90"}, pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 200 300]", Crop: "[0 0 100 200]", Rotate: "0"}, pdffixture.PageBoxes{UserUnit: "2"})
	doc.Version = "2.0"
	facts, views := inspectSelection(t, doc, []SelectionRequest{{Item: "a", Pages: []packet.PageRange{{First: 1, Last: 1}, {First: 3, Last: 3}}}, {Item: "b", Pages: []packet.PageRange{{First: 2, Last: 2}}}, {Item: "full"}})
	if facts.Version != "2.0" || facts.Pages != 3 || facts.Bytes != int64(len(doc.Bytes())) || views["a"].Pages != 2 || views["b"].Pages != 1 || views["full"].Pages != 3 || views["a"].FirstSize != facts.FirstSize || views["a"].LastSize != facts.LastSize {
		t.Fatalf("facts %+v views %+v", facts, views)
	}
	path, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "a", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}, {First: 3, Last: 3}}}, {Pointer: "b", Source: "source", Selection: []packet.PageRange{{First: 2, Last: 2}}}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	tools := oracle.RequireTools(t)
	output, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := output.RawTexts()
	if err != nil || len(texts) != 3 || output.EffectiveVersion() != 20 {
		t.Fatalf("selected text/version: %q %v", texts, err)
	}
	for page, want := range []string{"select p1", "select p3", "select p2"} {
		if got := strings.Join(strings.Fields(texts[page]), " "); got != want {
			t.Fatalf("selected page %d text: %q; want words %q", page+1, texts[page], want)
		}
	}
	sourcePath := filepath.Join(t.TempDir(), "source.pdf")
	if err := doc.WriteFile(sourcePath); err != nil {
		t.Fatal(err)
	}
	original, err := oracle.Load(tools, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for page, sourcePage := range []int{1, 3, 2} {
		difference, err := output.AppearanceDifference(page+1, original, sourcePage, 0)
		if err != nil || difference != 0 {
			t.Fatalf("page %d pixels changed: %g %v", sourcePage, difference, err)
		}
	}
}

func TestPartialSelectionSafetyAndSelectedCompatibility(t *testing.T) {
	signed := pdffixture.Form("signed")
	for i, object := range signed.Objs {
		if bytes.Contains(object, []byte("/T (b)")) {
			last := bytes.LastIndex(object, []byte(">>"))
			signed.Objs[i] = append(append(append([]byte{}, object[:last]...), []byte(" /FT /Sig /V << /Type /Sig >> ")...), object[last:]...)
		}
	}
	for _, tc := range []struct {
		doc  *pdffixture.Doc
		code diag.Code
	}{{pdffixture.Form("form"), diag.SourceSelectionFormUnsupported}, {signed, diag.SourceSigned}} {
		facts, _, ds := InspectSelections(t.Context(), tc.doc.Bytes(), []SelectionRequest{{Item: "part", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
		found := false
		for _, d := range ds {
			found = found || d.Code == tc.code
		}
		if status, _ := diag.Outcome(ds); status != "invalid" || !found || facts.Bytes == 0 || facts.Pages != 2 {
			t.Fatalf("whole-file safety facts: %+v %+v", facts, ds)
		}
	}
	inspectSelection(t, pdffixture.Form("whole"), []SelectionRequest{{Item: "full", Pages: []packet.PageRange{{First: 1, Last: 2}}}})

	doc := pdffixture.Links("links")
	source, views := inspectSelection(t, doc, []SelectionRequest{{Item: "one", Pages: []packet.PageRange{{First: 1, Last: 1}}}, {Item: "two", Pages: []packet.PageRange{{First: 2, Last: 2}}}})
	if !source.HasCoordinateDestinations || views["one"].HasCoordinateDestinations || views["two"].HasCoordinateDestinations {
		t.Fatalf("omitted links still restrict fitting: %+v", views)
	}
	_, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "one", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}, {Pointer: "two", Source: "source", Selection: []packet.PageRange{{First: 2, Last: 2}}}}, &packet.Size{Width: 300, Height: 400})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	// Annotation appearances on omitted pages do not constrain the selected view.
	d := pdffixture.Pages("appearance", 2)
	last := bytes.LastIndex(d.Objs[3], []byte(">>"))
	d.Objs[3] = append(append(append([]byte{}, d.Objs[3][:last]...), []byte(" /Annots [8 0 R] ")...), d.Objs[3][last:]...)
	d.Objs = append(d.Objs, []byte("<< /Subtype /Text /Rect [10 10 20 20] >>"))
	_, views = inspectSelection(t, d, []SelectionRequest{{Item: "omit", Pages: []packet.PageRange{{First: 2, Last: 2}}}, {Item: "retain", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
	if views["omit"].PDF20AppearanceIssue != "" || views["retain"].PDF20AppearanceIssue == "" {
		t.Fatal(views)
	}
}

func TestSelectedProjectionPrunesOnlyOwnOccurrenceTargets(t *testing.T) {
	for _, doc := range []*pdffixture.Doc{pdffixture.Links("links"), pdffixture.NamedDests("named"), pdffixture.LegacyDests("legacy")} {
		path, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "one", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}, {Pointer: "two", Source: "source", Selection: []packet.PageRange{{First: 2, Last: 2}}}}, nil)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		output, err := oracle.Load(oracle.RequireTools(t), path)
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range []int{1, 2} {
			targets, err := output.ActionTargets(page)
			if err != nil || len(targets) > 0 {
				t.Fatalf("target crossed occurrences: %v %v", targets, err)
			}
		}
	}
}

func TestNativeSourceOutlinesRemainStyledAndNested(t *testing.T) {
	doc := pdffixture.Outlined("native")
	// Style and closed state remain native dictionary entries after grafting.
	for i, b := range doc.Objs {
		if bytes.Contains(b, []byte("/Title")) {
			doc.Objs[i] = bytes.Replace(b, []byte(">>"), []byte(" /C [1 0 0] /F 3 >>"), 1)
		}
	}
	path, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "section", Title: "Ā Evidence", Items: []packet.Part{{Pointer: "section/source", Source: "source"}}}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := output.OutlineTargets()
	if err != nil || !reflect.DeepEqual(targets, []int{1, 1, 2}) {
		t.Fatalf("native outlines %v %v", targets, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := read(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	forest, err := readOutlineForest(t.Context(), s)
	if err != nil || len(forest) != 1 || len(forest[0].children) != 2 {
		t.Fatalf("graft shape: %+v %v", forest, err)
	}
	for _, node := range forest[0].children {
		if node.dict["F"] == nil || node.dict["C"] == nil {
			t.Fatal("native styles dropped")
		}
	}
	// Independently exercise final native validation, separate from the observer.
	if err := api.ValidateContext(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedOutlinesKeepNativeClosedContainerAndActions(t *testing.T) {
	doc := pdffixture.Pages("native selection", 3)
	root := len(doc.Objs) + 1
	parent, child, leaf := root+1, root+2, root+3
	object := doc.Objs[0]
	last := bytes.LastIndex(object, []byte(">>"))
	doc.Objs[0] = append(append(append([]byte{}, object[:last]...), []byte(fmt.Sprintf(" /Outlines %d 0 R ", root))...), object[last:]...)
	doc.Objs = append(doc.Objs,
		[]byte(fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count 1 >>", parent, parent)),
		[]byte(fmt.Sprintf("<< /Title (Container) /Parent %d 0 R /Dest [4 0 R /Fit] /First %d 0 R /Last %d 0 R /Count -2 /C [1 0 0] /F 3 >>", root, child, leaf)),
		[]byte(fmt.Sprintf("<< /Title (Retained) /Parent %d 0 R /Next %d 0 R /A << /S /URI /URI (https://example.invalid) /Next << /S /GoTo /D [6 0 R /FitH 20] >> >> >>", parent, leaf)),
		[]byte(fmt.Sprintf("<< /Title (Omitted) /Parent %d 0 R /Prev %d 0 R /Dest [8 0 R /Fit] >>", parent, child)))
	path, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "outer", Title: "Outer", Items: []packet.Part{{Pointer: "inner", Title: "Inner", Items: []packet.Part{{Pointer: "selected", Source: "source", Selection: []packet.PageRange{{First: 2, Last: 2}}}}}}}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := output.OutlineTargets()
	if err != nil || !reflect.DeepEqual(targets, []int{1, 1, 1}) {
		t.Fatalf("outline targets %v %v", targets, err)
	}
	// Expected hierarchy and closed state come from fixture intent, independently
	// of the production readOutlineForest observer used below for native styles.
	retained := oracle.Outline{Title: "Retained", Targets: []int{1}}
	closed := oracle.Outline{Title: "Container", Open: false, Children: []oracle.Outline{retained}}
	inner := oracle.Outline{Title: "Inner", Targets: []int{1}, Open: true, Children: []oracle.Outline{closed}}
	expected := []oracle.Outline{{Title: "Outer", Targets: []int{1}, Open: true, Children: []oracle.Outline{inner}}}
	if err = output.CheckOutlineTree(expected); err != nil {
		t.Fatal(err)
	}
	expected[0].Children[0].Children[0].Open = true
	if err = output.CheckOutlineTree(expected); err == nil {
		t.Fatal("independent oracle accepted a wrong open state for a closed nonleaf")
	}
	expected[0].Children[0].Children[0].Open = false
	// qpdf QDF keeps object offsets unchanged for this equal-length count edit.
	// No production outline reader/writer constructs the rejecting control.
	tools := oracle.RequireTools(t)
	qdf := filepath.Join(t.TempDir(), "outline.qdf.pdf")
	if raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--qdf", "--object-streams=disable", path, qdf).CombinedOutput(); err != nil {
		t.Fatalf("qdf: %s %v", raw, err)
	}
	qdfData, err := os.ReadFile(qdf)
	if err != nil {
		t.Fatal(err)
	}
	objects := bytes.Split(qdfData, []byte("\nendobj"))
	changed := 0
	for n, object := range objects {
		if bytes.Contains(object, []byte("/Title (Container)")) {
			if !bytes.Contains(object, []byte("/Count -1")) {
				t.Fatalf("closed count not independently serialized as -1: %s", object)
			}
			objects[n] = bytes.Replace(object, []byte("/Count -1"), []byte("/Count -2"), 1)
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("found %d serialized closed containers", changed)
	}
	badCount := filepath.Join(t.TempDir(), "wrong-count.pdf")
	if err = os.WriteFile(badCount, bytes.Join(objects, []byte("\nendobj")), 0600); err != nil {
		t.Fatal(err)
	}
	wrongCount, err := oracle.Load(tools, badCount)
	if err != nil {
		t.Fatal(err)
	}
	if err = wrongCount.CheckOutlineTree(expected); err == nil || !strings.Contains(err.Error(), "outline count") {
		t.Fatalf("independent oracle did not reject the wrong closed descendant count: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := read(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	forest, err := readOutlineForest(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	node := forest[0].children[0].children[0]
	if node.open || node.dict["Dest"] != nil || node.dict["A"] != nil || len(node.children) != 1 || node.dict["F"] != types.Integer(3) {
		t.Fatalf("native container semantics lost: %+v", node)
	}
	action, err := s.DereferenceDict(node.children[0].dict["A"])
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.DereferenceDict(action["Next"])
	if err != nil {
		t.Fatal(err)
	}
	dest, err := s.DereferenceArray(next["D"])
	if err != nil || len(dest) != 3 || dest[1] != types.Name("FitH") || dest[2] != types.Integer(20) {
		t.Fatalf("native coordinate action lost: %v %v", dest, err)
	}
}

func TestComposedOutlineHierarchyUsesBackendBoundary(t *testing.T) {
	doc := pdffixture.Plain("deep outlines")
	levels := model.DefaultResourceLimits().MaxRecursionDepth + 1
	root, first := 6, 7
	last := bytes.LastIndex(doc.Objs[0], []byte(">>"))
	doc.Objs[0] = append(append(append([]byte{}, doc.Objs[0][:last]...), []byte(" /Outlines 6 0 R ")...), doc.Objs[0][last:]...)
	doc.Objs = append(doc.Objs, []byte(fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>", first, first, levels)))
	for i := range levels {
		parent := root
		if i > 0 {
			parent = first + i - 1
		}
		children := ""
		if i+1 < levels {
			children = fmt.Sprintf(" /First %d 0 R /Last %d 0 R /Count %d", first+i+1, first+i+1, levels-i-1)
		}
		doc.Objs = append(doc.Objs, []byte(fmt.Sprintf("<< /Title (level %d) /Parent %d 0 R /Dest [3 0 R /Fit] %s >>", i, parent, children)))
	}
	_, views := inspectSelection(t, doc, []SelectionRequest{{Item: "source"}})
	if views["source"].OutlineDepth != levels {
		t.Fatal(views)
	}
	_, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "section", Title: "Wrapper", Items: []packet.Part{{Pointer: "source", Source: "source"}}}}, nil)
	if len(ds) != 1 || ds[0].Code != diag.SourceOutlineDepthUnsupported {
		t.Fatalf("composed hierarchy not refused: %+v", ds)
	}
}

func TestGroupedFooterMeasurementsUseOnlyRequestedOriginalPages(t *testing.T) {
	doc := pdffixture.WithBoxes("footer geometry", pdffixture.PageBoxes{Media: "[0 0 400 600]"}, pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 200 300]"}, pdffixture.PageBoxes{Rotate: "90", UserUnit: "2"})
	facts, views := inspectSelection(t, doc, []SelectionRequest{{Item: "one", Pages: []packet.PageRange{{First: 1, Last: 1}, {First: 3, Last: 3}}, MeasureFooter: true}, {Item: "two", Pages: []packet.PageRange{{First: 2, Last: 2}}}, {Item: "repeat", Pages: []packet.PageRange{{First: 3, Last: 3}}, MeasureFooter: true}})
	if len(facts.PageSizes) != 2 || facts.PageSizes[1] != views["one"].FirstSize || facts.PageSizes[3] != views["repeat"].FirstSize {
		t.Fatalf("shared source geometry: %+v views %+v", facts.PageSizes, views)
	}
	if _, extra := facts.PageSizes[2]; extra {
		t.Fatal("unrequested source geometry was retained")
	}
	ordinary, _ := inspectSelection(t, doc, []SelectionRequest{{Item: "whole"}})
	if ordinary.PageSizes != nil {
		t.Fatal("ordinary inspection allocated footer geometry")
	}
	invalid, occurrences, ds := InspectSelections(t.Context(), doc.Bytes(), []SelectionRequest{{Item: "bad", Pages: []packet.PageRange{{First: 4, Last: 4}}, MeasureFooter: true}})
	if len(ds) != 1 || ds[0].Code != diag.SourceSelectionOutOfRange || invalid.PageSizes != nil || len(occurrences) != 0 {
		t.Fatalf("invalid selection invented geometry: %+v %+v %+v", invalid.PageSizes, occurrences, ds)
	}
}

func TestPartialSelectionChecksOriginalHashAfterOmittedPageChanges(t *testing.T) {
	doc := pdffixture.Pages("original", 2)
	original := doc.Bytes()
	request := []SelectionRequest{{Item: "selected", Pages: []packet.PageRange{{First: 1, Last: 1}}}}
	facts, occurrences := inspectSelection(t, doc, request)
	path := filepath.Join(t.TempDir(), "source.pdf")
	layout, ds := packet.Resolve(packet.Job{Parts: []packet.Part{{Pointer: "selected", Source: path, Selection: request[0].Pages}}}, map[string]packet.SourceFacts{"selected": occurrences["selected"].SourceFacts}, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	// Pages uses page objects4/6 and content objects5/7. Change only omitted
	// page2's equal-length marker; selected page1's objects remain identical.
	selectedContent := bytes.Clone(doc.Objs[4])
	omittedContent := bytes.Clone(doc.Objs[6])
	doc.Objs[6] = bytes.ReplaceAll(doc.Objs[6], []byte("original p2"), []byte("changed! p2"))
	changed := doc.Bytes()
	if bytes.Equal(omittedContent, doc.Objs[6]) || !bytes.Equal(selectedContent, doc.Objs[4]) || bytes.Equal(original, changed) {
		t.Fatal("fixture did not change only omitted content")
	}
	_, changedOccurrences := inspectSelection(t, doc, request)
	if !reflect.DeepEqual(occurrences["selected"].SourceFacts, changedOccurrences["selected"].SourceFacts) {
		t.Fatal("omitted content unexpectedly changed selected layout facts")
	}
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	_, err := Assemble(t.Context(), layout, map[string]SourceFacts{path: facts}, occurrences, nil, nil, nil, &output)
	if err == nil || Diagnostic(err).Code != diag.SourceChanged || output.Len() != 0 {
		t.Fatalf("changed omitted bytes were not refused before writing: err=%v bytes=%d", err, output.Len())
	}
}

func additionalActionSelection(owner, encoding string, siblings bool) *pdffixture.Doc {
	doc := pdffixture.Pages("additional actions", 2)
	addObject := func(body string) string {
		doc.Objs = append(doc.Objs, []byte(body))
		return fmt.Sprintf("%d 0 R", len(doc.Objs))
	}
	appendEntry := func(number int, entry string) {
		object := doc.Objs[number-1]
		last := bytes.LastIndex(object, []byte(">>"))
		doc.Objs[number-1] = append(append(append([]byte{}, object[:last]...), []byte(" "+entry+" ")...), object[last:]...)
	}
	removed := "<< /S /GoTo /D [6 0 R /FitH 20] /Next << /S /URI /URI (https://example.invalid/dropped-chain) >> >>"
	action := removed
	if siblings {
		action = "<< /S /URI /URI (https://example.invalid/retained) /Next [" + removed + " << /S /GoTo /D [4 0 R /Fit] >> << /S /GoToR /F (remote.pdf) /D [0 /XYZ 20 30 null] >>] >>"
	}
	if encoding != "direct" {
		action = addObject(action)
	}
	event, second := "O", "C"
	if owner == "screen" {
		event, second = "E", "X"
	}
	aa := "<< /" + event + " " + action
	if encoding == "shared" {
		aa += " /" + second + " " + action
	}
	aa += " >>"
	if encoding != "direct" {
		aa = addObject(aa)
	}
	if owner == "page" {
		appendEntry(4, "/AA "+aa)
	} else {
		annotation := addObject("<< /Subtype /Screen /Rect [10 10 40 40] /P 4 0 R /AA " + aa + " >>")
		appendEntry(4, "/Annots ["+annotation+"]")
	}
	return doc
}

func TestSelectedAdditionalActionsRemoveOmittedTargets(t *testing.T) {
	for _, owner := range []string{"page", "screen"} {
		for _, encoding := range []string{"direct", "indirect", "shared"} {
			for _, siblings := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/siblings=%v", owner, encoding, siblings), func(t *testing.T) {
					doc := additionalActionSelection(owner, encoding, siblings)
					path, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "selected", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}}, nil)
					if len(ds) > 0 {
						t.Fatal(ds)
					}
					tools := oracle.RequireTools(t)
					output, err := oracle.Load(tools, path)
					if err != nil {
						t.Fatal(err)
					}
					targets, err := output.ActionTargets(1)
					wanted := 0
					if siblings {
						wanted = 1
						if encoding == "shared" {
							wanted = 2
						}
					}
					if err != nil || len(targets) != wanted {
						t.Fatalf("retained action targets %v: %v", targets, err)
					}
					for _, target := range targets {
						if target != 1 {
							t.Fatalf("additional action retargeted omitted page: %v", targets)
						}
					}
					raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).CombinedOutput()
					if err != nil {
						t.Fatalf("qpdf: %v\n%s", err, raw)
					}
					if bytes.Contains(raw, []byte("https://example.invalid/dropped-chain")) {
						t.Fatal("removed action retained its own Next chain")
					}
					if siblings {
						if !bytes.Contains(raw, []byte(`"/S": "/GoToR"`)) || !bytes.Contains(raw, []byte("https://example.invalid/retained")) {
							t.Fatal("independent URI/remote siblings were removed")
						}
					} else if bytes.Contains(raw, []byte(`"/S": "/GoTo"`)) {
						t.Fatal("omitted additional action survived in final PDF")
					}
				})
			}
		}
	}
}

func TestSelectedAdditionalActionsDoNotRetainNumericFitRestriction(t *testing.T) {
	for _, owner := range []string{"page", "screen"} {
		t.Run(owner, func(t *testing.T) {
			doc := additionalActionSelection(owner, "indirect", false)
			whole, views := inspectSelection(t, doc, []SelectionRequest{{Item: "selected", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
			if !whole.HasCoordinateDestinations || views["selected"].HasCoordinateDestinations {
				t.Fatalf("omitted additional action still restricts fitting: %+v", views)
			}
			_, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "selected", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}}, &packet.Size{Width: 300, Height: 400})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
		})
	}
}

// sharedNamedAction is one ordinary GoTo dictionary shared by page-open and link owners.
func sharedNamedAction() *pdffixture.Doc {
	d := pdffixture.NamedDests("shared")
	action := len(d.Objs) + 1
	for i, b := range d.Objs {
		if bytes.Contains(b, []byte("/Subtype /Link")) {
			b = bytes.ReplaceAll(b, []byte("/Dest (chap2)"), fmt.Appendf(nil, "/A %d 0 R", action))
			b = bytes.ReplaceAll(b, []byte("/A << /S /GoTo /D (chap1) >>"), fmt.Appendf(nil, "/A %d 0 R", action))
		}
		if bytes.Contains(b, []byte("/Type /Page ")) {
			last := bytes.LastIndex(b, []byte(">>"))
			b = append(append(append([]byte{}, b[:last]...), fmt.Appendf(nil, " /AA << /O %d 0 R >> ", action)...), b[last:]...)
		}
		d.Objs[i] = b
	}
	d.Objs = append(d.Objs, []byte("<< /S /GoTo /D (chap2) >>"))
	return d
}
func TestSharedNavigationOwnersAcrossInterleavedOccurrences(t *testing.T) {
	d := sharedNamedAction()
	requests := []SelectionRequest{{Item: "first", Pages: []packet.PageRange{{First: 1, Last: 1}}}, {Item: "whole"}, {Item: "second", Pages: []packet.PageRange{{First: 2, Last: 2}}}, {Item: "again"}}
	whole, views := inspectSelection(t, d, requests)
	if !reflect.DeepEqual(whole.DestNames, []string{"chap1", "chap2"}) {
		t.Fatal(whole.DestNames)
	}
	for item, want := range map[string][]string{"first": {"chap1"}, "whole": {"chap1", "chap2"}, "second": {"chap2"}, "again": {"chap1", "chap2"}} {
		if !reflect.DeepEqual(views[item].DestNames, want) {
			t.Fatalf("%s: %v", item, views[item].DestNames)
		}
	}
	path, ds := selectedPacket(t, d, []packet.Part{{Pointer: "first", Source: "source", Selection: requests[0].Pages}, {Pointer: "whole", Source: "source"}, {Pointer: "second", Source: "source", Selection: requests[2].Pages}, {Pointer: "again", Source: "source"}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range [][]int{nil, {3, 3}, {3, 3}, {4, 4}, {6, 6}, {6, 6}} {
		got, err := output.ActionTargets(i + 1)
		if err != nil || len(got) != len(want) || len(want) > 0 && !reflect.DeepEqual(got, want) {
			t.Fatalf("page %d: %v want %v (%v)", i+1, got, want, err)
		}
	}
}
func TestRepeatedWholeFormRetainsFieldOnlyActions(t *testing.T) {
	d := pdffixture.Form("FIELD")
	// The parent field has no annotation role. Its action is intentionally observable only through Fields/Kids.
	for i, b := range d.Objs {
		if bytes.Contains(b, []byte("/T (grp)")) {
			d.Objs[i] = bytes.Replace(b, []byte("/FT /Tx"), []byte("/AA << /K << /S /GoTo /D [4 0 R /Fit] >> >> /FT /Tx"), 1)
		}
	}
	path, ds := selectedPacket(t, d, []packet.Part{{Pointer: "first", Source: "source"}, {Pointer: "again", Source: "source"}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := output.FieldActionTargets()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{2, 4}) {
		t.Fatalf("field actions %v want [2 4]", got)
	}
	_, _, refused := InspectSelections(t.Context(), d.Bytes(), []SelectionRequest{{Item: "partial", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
	found := false
	for _, d := range refused {
		if d.Code == diag.SourceSelectionFormUnsupported {
			found = true
		}
	}
	if !found {
		t.Fatal("partial form accepted", refused)
	}
}
func TestNativeOutlinesGraftedUnderAuthoredSections(t *testing.T) {
	path, ds := selectedPacket(t, pdffixture.Outlined("NATIVE"), []packet.Part{{Pointer: "section", Title: "Authored", Items: []packet.Part{{Pointer: "source", Source: "source"}}}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	expected := []oracle.Outline{{Title: "Authored", Targets: []int{1}, Open: true, Children: []oracle.Outline{{Title: "First", Targets: []int{1}}, {Title: "Second", Targets: []int{2}}}}}
	if err = output.CheckOutlineTree(expected); err != nil {
		t.Fatal(err)
	}
	expected[0].Children[1].Targets = []int{1}
	if err = output.CheckOutlineTree(expected); err == nil {
		t.Fatal("oracle accepted wrong target hierarchy")
	}
}

func TestLinkedAnnotationOwnersRemainOccurrenceLocal(t *testing.T) {
	d := pdffixture.Pages("LINKED", 2)
	base := len(d.Objs) + 1
	for n, b := range d.Objs {
		if bytes.Contains(b, []byte("/Type /Page ")) {
			last := bytes.LastIndex(b, []byte(">>"))
			d.Objs[n] = append(append(append([]byte{}, b[:last]...), fmt.Appendf(nil, " /Annots [%d 0 R] ", base)...), b[last:]...)
			break
		}
	}
	d.Objs = append(d.Objs, fmt.Appendf(nil, "<< /Type /Annot /Subtype /Text /Rect [0 0 20 20] /Popup %d 0 R /IRT %d 0 R >>", base+1, base+2), fmt.Appendf(nil, "<< /Type /Annot /Subtype /Popup /Rect [0 0 20 20] /Parent %d 0 R /AA << /O << /S /GoTo /D [6 0 R /Fit] >> >> >>", base), []byte("<< /Type /Annot /Subtype /Text /Rect [0 0 20 20] /AA << /E << /S /GoTo /D [4 0 R /Fit] >> >> >>"))
	path, ds := selectedPacket(t, d, []packet.Part{{Pointer: "partial", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}, {Pointer: "whole", Source: "source"}, {Pointer: "again", Source: "source"}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range [][]int{{1}, {2, 3}, nil, {4, 5}, nil} {
		got, err := output.ActionTargets(page + 1)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("page %d got %v want %v", page+1, got, want)
		}
	}
}

func TestOnPagePopupRetainsSoleParentActionsAcrossOccurrences(t *testing.T) {
	d := pdffixture.Pages("POPUP PARENT", 2)
	parent := len(d.Objs) + 1
	popup := parent + 1
	for n, b := range d.Objs {
		if bytes.Contains(b, []byte("/Type /Page ")) {
			last := bytes.LastIndex(b, []byte(">>"))
			d.Objs[n] = append(append(append([]byte{}, b[:last]...), fmt.Appendf(nil, " /Annots [%d 0 R] ", popup)...), b[last:]...)
			break
		}
	}
	// Only the Popup is on-page. Both relevant actions live solely on its Parent;
	// deleting Parent traversal in either production or oracle must lose evidence.
	d.Objs = append(d.Objs, fmt.Appendf(nil, "<< /Type /Annot /Subtype /Text /Rect [0 0 20 20] /Popup %d 0 R /AA << /E << /S /GoTo /D [6 0 R /Fit] >> /X << /S /GoTo /D [4 0 R /Fit] >> >> >>", popup), fmt.Appendf(nil, "<< /Type /Annot /Subtype /Popup /Rect [0 0 20 20] /Parent %d 0 R >>", parent))
	path, ds := selectedPacket(t, d, []packet.Part{{Pointer: "partial", Source: "source", Selection: []packet.PageRange{{First: 1, Last: 1}}}, {Pointer: "whole", Source: "source"}, {Pointer: "again", Source: "source"}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	output, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range [][]int{{1}, {2, 3}, nil, {4, 5}, nil} {
		targets, err := output.ActionTargets(page + 1)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(targets)
		if !slices.Equal(targets, want) {
			t.Fatalf("sole Popup.Parent page %d: got %v want %v", page+1, targets, want)
		}
	}
}
