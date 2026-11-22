// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"os"
	"path/filepath"
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
