// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestNativeCompositionFreesUnwrittenTrailingObjects(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, cleanup := range []bool{false, true} {
		s, _, err := generatedContext(t.Context(), []Page{{Width: 300, Height: 200}}, 1, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Classic xrefs make the defect visible without a later xref-stream object
		// accidentally extending the serialized range. Both writer modes must work.
		s.WriteObjectStream = false
		s.WriteXRefStream = false
		spare, err := s.IndRefForNewObject(types.Dict{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.IndRefForNewObject(types.Dict{"Unreachable": types.Boolean(true)})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FreeObject(spare.ObjectNumber.Value()); err != nil {
			t.Fatal(err)
		}
		if cleanup {
			if err := freeUnreachable(t.Context(), s); err != nil {
				t.Fatal(err)
			}
		}
		var output bytes.Buffer
		if err := api.WriteContext(t.Context(), s, &output); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "packet.pdf")
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		report, err := exec.CommandContext(t.Context(), tools.QPDF, "--check", path).CombinedOutput()
		if cleanup {
			if err != nil {
				t.Fatalf("cleaned native xref: %v\n%s", err, report)
			}
		} else if err == nil || !bytes.Contains(report, []byte("reported number of objects")) {
			t.Fatalf("negative control did not detect stale Size: %v\n%s", err, report)
		}
	}
}

func TestReachabilityPreservesStructuralReferencesAndCancellation(t *testing.T) {
	s, _, err := generatedContext(t.Context(), []Page{{Width: 300, Height: 200}}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := types.Dict{}
	ref, err := s.IndRefForNewObject(d)
	if err != nil {
		t.Fatal(err)
	}
	array, err := s.IndRefForNewObject(types.Array{*ref})
	if err != nil {
		t.Fatal(err)
	}
	d["Cycle"] = *array
	stream, err := s.NewStreamDictForBuf([]byte("retained private bytes"))
	if err != nil {
		t.Fatal(err)
	}
	stream.Dict["PrivateReference"] = *ref
	streamRef, err := s.IndRefForNewObject(*stream)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	cat["PrivateObject"] = *streamRef
	orphan, err := s.IndRefForNewObject(types.Dict{})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := freeUnreachable(canceled, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation hidden: %v", err)
	}
	if err := freeUnreachable(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []types.IndirectRef{*ref, *array, *streamRef} {
		if s.Table[kept.ObjectNumber.Value()].Free {
			t.Fatalf("retained object was freed: %s", kept)
		}
	}
	if !s.Table[orphan.ObjectNumber.Value()].Free {
		t.Fatal("unreachable object remains live")
	}
}

func TestReachabilityKeepsIndirectObjectChainsFromReadContext(t *testing.T) {
	doc := pdffixture.Plain("reference chain")
	page := doc.Objs[2]
	last := bytes.LastIndex(page, []byte(">>"))
	doc.Objs[2] = append(append(append([]byte{}, page[:last]...), []byte(" /Annots [6 0 R] ")...), page[last:]...)
	doc.Objs = append(doc.Objs, []byte("<< /Subtype /Link /Rect [10 10 20 20] /Dest [3 0 R /Fit] /Private 7 0 R >>"), []byte("8 0 R"), []byte("<< /Marker (reachable) /Cycle 7 0 R >>"))
	s, err := read(t.Context(), doc.Bytes())
	if err != nil {
		t.Fatalf("source read rejected the graph before cleanup: %v", err)
	}
	if err := freeUnreachable(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	for _, number := range []int{7, 8} {
		if s.Table[number].Free {
			t.Fatalf("reachable indirect chain object %d was freed", number)
		}
	}
	// The source reader admits this graph, but the pinned native writer cannot
	// serialize an indirect reference used as an object body. Cleanup must keep
	// the graph intact and preserve the backend's failed outcome.
	_, ds := selectedPacket(t, doc, []packet.Part{{Pointer: "source", Source: "source"}}, nil)
	if len(ds) != 1 || ds[0].Code != diag.AssemblyFailed {
		t.Fatalf("backend graph failure changed classification: %+v", ds)
	}
}
