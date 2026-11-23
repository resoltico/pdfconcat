// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestReopenedNavigationRejectsChangedDestination(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.pdf")
	d := pdffixture.NavigationPages("navigation", true, false)
	if err := d.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	facts, views, ds := pdf.Inspect(t.Context(), d.Bytes(), "", []pdf.SelectionRequest{{Item: "source"}})
	if diag.Outcome(ds) != "ok" {
		t.Fatal(ds)
	}
	part := pdf.Part{
		Part:       packet.Part{Ref: "source", Source: source, Kind: packet.PartSource, Pages: 2, FirstPage: 1},
		Occurrence: &pdf.Occurrence{File: &facts, Facts: views[0]},
		Input:      pdffixture.SourceInput(d.Bytes()),
	}
	job := pdf.Assembly{Parts: []pdf.Part{part}}
	var output bytes.Buffer
	built, err := pdf.Assemble(t.Context(), job, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := built.VerifyReader(t.Context(), bytes.NewReader(output.Bytes()), 2, job); err != nil {
		t.Fatalf("correct output: %v", err)
	}
	for _, change := range []string{"another-physical-page", "wrong-generation-operand", "wrong-generation-outer-binding"} {
		t.Run(change, func(t *testing.T) {
			config := model.NewStatelessConfiguration()
			config.WriteObjectStream = false
			config.WriteXRefStream = false
			graph, err := api.ReadContext(t.Context(), bytes.NewReader(output.Bytes()), config)
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.EnsurePageCount(); err != nil {
				t.Fatal(err)
			}
			page, pageRef, _, err := graph.PageDict(t.Context(), 1, false)
			if err != nil {
				t.Fatal(err)
			}
			annots, err := graph.DereferenceArray(page["Annots"])
			if err != nil || len(annots) != 1 {
				t.Fatal(annots, err)
			}
			link, err := graph.DereferenceDict(annots[0])
			if err != nil {
				t.Fatal(err)
			}
			dest, err := graph.DereferenceArray(link["Dest"])
			if err != nil || len(dest) != 5 {
				t.Fatal(dest, err)
			}
			dest = append(types.Array(nil), dest...)
			var injected *types.IndirectRef
			var before, after string
			switch change {
			case "wrong-generation-operand":
				injected, err = graph.IndRefForNewObject(dest[2])
				if err != nil {
					t.Fatal(err)
				}
				dest[2] = *injected
				before, after = " ", " "
				link["Dest"] = dest
			case "wrong-generation-outer-binding":
				injected, err = graph.IndRefForNewObject(dest)
				if err != nil {
					t.Fatal(err)
				}
				before = "/Dest "
				link["Dest"] = *injected
			default:
				dest[0] = *pageRef
				link["Dest"] = dest
			}
			var altered bytes.Buffer
			if err := api.WriteContext(t.Context(), graph, &altered); err != nil {
				t.Fatal(err)
			}
			data := altered.Bytes()
			if injected != nil {
				// Change only the serialized reference. The writer must emit
				// a consistent header and xref for the live scalar.
				wrong := *injected
				wrong.GenerationNumber++
				original := []byte(before + injected.PDFString() + after)
				replacement := []byte(before + wrong.PDFString() + after)
				if len(original) != len(replacement) || bytes.Count(data, original) != 1 {
					t.Fatalf("fixture has %d occurrences of %q", bytes.Count(data, original), original)
				}
				data = bytes.Replace(data, original, replacement, 1)
			}
			path := filepath.Join(t.TempDir(), "changed-destination.pdf")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			qpdfCheck(t, path)
			err = built.VerifyReader(t.Context(), bytes.NewReader(data), 2, job)
			message := "reopened local navigation"
			if change != "another-physical-page" {
				message = "dangling output object"
			}
			if err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid || !bytes.Contains([]byte(pdf.Diagnostic(err).Detail), []byte(message)) {
				t.Fatal("changed destination passed verification", err)
			}
		})
	}
}

func TestReopenedNavigationRejectsTransferredDestination(t *testing.T) {
	source := pdffixture.NavigationPages("navigation", true, false)
	link := len(source.Objs)
	addSecond := fmt.Appendf(nil, "/Annots [%d 0 R %d 0 R]", link, link+1)
	source.Objs[3] = bytes.Replace(source.Objs[3], fmt.Appendf(nil, "/Annots [%d 0 R]", link), addSecond, 1)
	source.Objs = append(source.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [50 200 180 220] /P 4 0 R >>"))
	built, job, good := assembleNavigationOwnershipFixture(t, source)
	for _, change := range []string{"move to another page", "transfer to another annotation"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			graph, first, firstRef := readNavigationOwnershipPage(t, good)
			annots, err := graph.DereferenceArray(first["Annots"])
			if err != nil || len(annots) != 2 {
				t.Fatal(annots, err)
			}
			linkRef, link := ownershipAnnotation(t, graph, annots[0])
			changes := map[types.IndirectRef]types.Dict{linkRef: link}
			if change == "move to another page" {
				second, secondRef, _, err := graph.PageDict(t.Context(), 2, false)
				if err != nil || second["Annots"] != nil {
					t.Fatal(second, err)
				}
				first["Annots"], second["Annots"] = types.Array{annots[1]}, types.Array{linkRef}
				link["P"] = *secondRef
				changes[*firstRef], changes[*secondRef] = first, second
			} else {
				otherRef, other := ownershipAnnotation(t, graph, annots[1])
				other["Dest"] = link["Dest"]
				delete(link, "Dest")
				changes[otherRef] = other
			}
			checkNavigationOwnershipMutation(t, built, job, good, graph, changes)
		})
	}
}

func TestReopenedNavigationRejectsChangedSharedActionEdges(t *testing.T) {
	source := pdffixture.NavigationPages("shared navigation", true, false)
	first := len(source.Objs)
	second, third, action := first+1, first+2, first+3
	source.Objs[3] = bytes.Replace(source.Objs[3], fmt.Appendf(nil, "/Annots [%d 0 R]", first),
		fmt.Appendf(nil, "/Annots [%d 0 R %d 0 R %d 0 R]", first, second, third), 1)
	// Both links reach a local destination through the same nonlocal
	// action. Only the first edge expands its Next subtree.
	source.Objs[first-1] = fmt.Appendf(nil, "<< /Type /Annot /Subtype /Link /Rect [15 60 40 80] /P 4 0 R /A %d 0 R >>", action)
	source.Objs = append(source.Objs,
		fmt.Appendf(nil, "<< /Type /Annot /Subtype /Link /Rect [15 90 40 110] /P 4 0 R /A %d 0 R >>", action),
		[]byte("<< /Type /Annot /Subtype /Link /Rect [15 150 40 170] /P 4 0 R >>"),
		[]byte("<< /S /URI /URI (https://example.org/) /Next << /S /GoTo /D [6 0 R /XYZ 20 30 null] >> >>"))
	built, job, good := assembleNavigationOwnershipFixture(t, source)
	for _, change := range []string{"delete later shared edge", "transfer later shared edge"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			graph, page, _ := readNavigationOwnershipPage(t, good)
			annots, err := graph.DereferenceArray(page["Annots"])
			if err != nil || len(annots) != 3 {
				t.Fatal(annots, err)
			}
			_, first := ownershipAnnotation(t, graph, annots[0])
			secondRef, second := ownershipAnnotation(t, graph, annots[1])
			action, ok := second["A"].(types.IndirectRef)
			if !ok || first["A"] != action {
				t.Fatalf("expected a shared action: %v %v", first["A"], second["A"])
			}
			delete(second, "A")
			changes := map[types.IndirectRef]types.Dict{secondRef: second}
			if change == "transfer later shared edge" {
				thirdRef, third := ownershipAnnotation(t, graph, annots[2])
				third["A"] = action
				changes[thirdRef] = third
			}
			checkNavigationOwnershipMutation(t, built, job, good, graph, changes)
		})
	}
}

func assembleNavigationOwnershipFixture(t *testing.T, source *pdffixture.Doc) (pdf.Assembled, pdf.Assembly, []byte) {
	t.Helper()
	raw := source.Bytes()
	facts, views, ds := pdf.Inspect(t.Context(), raw, "", []pdf.SelectionRequest{{Item: "source"}})
	if diag.Outcome(ds) != "ok" {
		t.Fatal(ds)
	}
	job := pdf.Assembly{Parts: []pdf.Part{{
		Part:       packet.Part{Ref: "source", Source: "source.pdf", Kind: packet.PartSource, Pages: 2, FirstPage: 1},
		Occurrence: &pdf.Occurrence{File: &facts, Facts: views[0]},
		Input:      pdffixture.SourceInput(raw),
	}}}
	var data bytes.Buffer
	built, err := pdf.Assemble(t.Context(), job, &data)
	if err != nil {
		t.Fatal(err)
	}
	if err := built.VerifyReader(t.Context(), bytes.NewReader(data.Bytes()), 2, job); err != nil {
		t.Fatalf("correct ownership must reopen: %v", err)
	}
	return built, job, data.Bytes()
}

func readNavigationOwnershipPage(t *testing.T, data []byte) (*model.Context, types.Dict, *types.IndirectRef) {
	t.Helper()
	graph, err := api.ReadContext(t.Context(), bytes.NewReader(data), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.EnsurePageCount(); err != nil {
		t.Fatal(err)
	}
	page, ref, _, err := graph.PageDict(t.Context(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	return graph, page, ref
}

func ownershipAnnotation(t *testing.T, graph *model.Context, raw types.Object) (types.IndirectRef, types.Dict) {
	t.Helper()
	ref, ok := raw.(types.IndirectRef)
	if !ok {
		t.Fatalf("expected an indirect annotation: %v", raw)
	}
	dict, err := graph.DereferenceDict(ref)
	if err != nil || dict == nil {
		t.Fatal(dict, err)
	}
	return ref, dict
}

func checkNavigationOwnershipMutation(t *testing.T, built pdf.Assembled, job pdf.Assembly, good []byte, graph *model.Context, changes map[types.IndirectRef]types.Dict) {
	t.Helper()
	altered := appendNavigationRevision(t, good, graph, changes)
	path := filepath.Join(t.TempDir(), "altered.pdf")
	if err := os.WriteFile(path, altered, 0600); err != nil {
		t.Fatal(err)
	}
	qpdfCheck(t, path)
	// Isolate semantic verification from the application-level byte digest.
	err := built.VerifyReader(t.Context(), bytes.NewReader(altered), 2, job)
	if err == nil {
		t.Fatal("changed local navigation ownership passed verification")
	}
	finding := pdf.Diagnostic(err)
	if finding.Code != diag.OutputInvalid || !strings.Contains(finding.Detail, "reopened local navigation") {
		t.Fatalf("expected a semantic navigation refusal: %+v", finding)
	}
}

// Append the changed dictionaries and leave the other serialized bytes,
// including Info metadata, intact so the ownership change is isolated.
func appendNavigationRevision(t *testing.T, good []byte, graph *model.Context, changes map[types.IndirectRef]types.Dict) []byte {
	t.Helper()
	last := regexp.MustCompile(`startxref\s+([0-9]+)\s+%%EOF\s*$`).FindSubmatch(good)
	if len(last) != 2 || graph.Root == nil || graph.Size == nil {
		t.Fatal("unexpected assembled trailer")
	}
	previous, err := strconv.Atoi(string(last[1]))
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Clone(good)
	var entries strings.Builder
	for _, ref := range slices.SortedFunc(maps.Keys(changes), func(a, b types.IndirectRef) int {
		return cmp.Compare(a.ObjectNumber, b.ObjectNumber)
	}) {
		offset := len(data)
		data = fmt.Appendf(data, "%d %d obj\n%s\nendobj\n", ref.ObjectNumber.Value(), ref.GenerationNumber.Value(), changes[ref].PDFString())
		fmt.Fprintf(&entries, "%d 1\n%010d %05d n \n", ref.ObjectNumber.Value(), offset, ref.GenerationNumber.Value())
	}
	trailer := types.Dict{"Root": *graph.Root, "Size": types.Integer(*graph.Size), "Prev": types.Integer(previous)}
	if graph.Info != nil {
		trailer["Info"] = *graph.Info
	}
	return fmt.Appendf(data, "xref\n%strailer\n%s\nstartxref\n%d\n%%%%EOF\n", entries.String(), trailer.PDFString(), len(data))
}
