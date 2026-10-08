// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

const (
	// fuzzDeadline bounds one Inspect or Assemble call on mutated input. The call must finish well inside
	// it; a mutated document that makes pdfcpu loop is reported as a failure.
	fuzzDeadline = 10 * time.Second

	// earliestVersion is PDF 1.0 in the engine's version encoding.
	earliestVersion pdfengine.Version = 10

	// maxFuzzRuns bounds the order a fuzz input can decode to.
	maxFuzzRuns = 6

	malformedAcroFormPDF = "%PDF-1.0000000\n1 0 obj <<0/Type /Catalog 0000000000000/AcroForm 0 0endobj 00"
)

func TestInspectRenderingFailureMatchesInspectionContract(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)

	path := filepath.Join(t.TempDir(), "rendering-state.pdf")
	if err := os.WriteFile(path, []byte(malformedAcroFormPDF), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := engine.Inspect(t.Context(), path)

	failure := requireFailure(t, err, pdfengine.CodeUnsupportedRendering)
	if !slices.Contains(knownCodes(), failure.Code) {
		t.Fatalf("document rendering rejection missing from Inspect contract: %v", failure)
	}

	if failure.Path != path || failure.Source != pdfengine.NoSource || failure.Err == nil {
		t.Fatalf("document rendering rejection lost source or cause: %+v", failure)
	}

	valid, info := inspectDoc(t, engine, "static-after-rendering-rejection", pdffixture.Plain("AFTER REJECTION"))
	requirePlausible(t, info)

	again, inspectErr := engine.Inspect(t.Context(), valid)
	if inspectErr != nil || again != info {
		t.Fatalf("valid inspection changed after rejected rendering state: %+v %v", again, inspectErr)
	}
}

// knownCodes are the failure codes Inspect may report.
func knownCodes() []pdfengine.Code {
	return []pdfengine.Code{
		pdfengine.CodeUnreadable, pdfengine.CodeInvalid, pdfengine.CodeEncrypted, pdfengine.CodeNoPages,
		pdfengine.CodePageGeometry, pdfengine.CodeCanceled, pdfengine.CodeUnsupportedRendering,
	}
}

// fuzzSourceNames are the fixtures a FuzzAssembleOrder request may draw from.
func fuzzSourceNames() []string {
	return []string{fixturePlainA, fixtureMulti, fixtureLinks, fixtureLegacy, fixtureVersion20}
}

// FuzzInspect feeds mutated PDFs to Inspect: it must return a value or an *Error with a known code,
// promptly, never panic, and agree with itself on a second call.
func FuzzInspect(f *testing.F) {
	for _, entry := range fixtureCatalog() {
		f.Add(entry.doc.Bytes())
	}

	f.Add([]byte{})
	f.Add([]byte("%PDF-1.7\n"))
	f.Add([]byte(malformedAcroFormPDF))

	cropped := pdffixture.PageBoxes{Media: sourceLetterBox, Crop: "[10 10 50 50]", Rotate: "90"}
	f.Add(pdffixture.WithBoxes("G", cropped, pdffixture.PageBoxes{UserUnit: "2"}).Bytes())

	engine := newEngine(f)
	path := filepath.Join(f.TempDir(), "input.pdf")

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), fuzzDeadline)
		defer cancel()

		start := time.Now()
		info, err := engine.Inspect(ctx, path)

		if elapsed := time.Since(start); elapsed > fuzzDeadline {
			t.Fatalf("Inspect took %v", elapsed)
		}

		if err != nil {
			if code := pdfengine.CodeOf(err); !slices.Contains(knownCodes(), code) {
				t.Fatalf("error %v has unexpected code %q", err, code)
			}

			return
		}

		requirePlausible(t, info)

		again, err := engine.Inspect(ctx, path)
		if err != nil || again != info {
			t.Fatalf("second Inspect differs: %+v %v vs %+v", again, err, info)
		}
	})
}

// requirePlausible fails when Inspect's facts could not describe any real document.
func requirePlausible(t *testing.T, info pdfengine.SourceInfo) {
	t.Helper()

	if info.Pages < 1 || info.Version < earliestVersion || info.Version > pdfengine.Version20 {
		t.Fatalf("implausible facts %+v", info)
	}

	for _, size := range []pdfengine.PageSize{info.First, info.Last} {
		if size.Width <= 0 || size.Height <= 0 || math.IsInf(size.Width, 0) || math.IsInf(size.Height, 0) {
			t.Fatalf("invalid visible size %+v", size)
		}
	}
}

// FuzzAssembleOrder decodes bytes into an assembly request over real fixtures and checks that Assemble
// accepts exactly the requests an independent model accepts, and that an accepted request yields a
// valid PDF of exactly the expected page count.
func FuzzAssembleOrder(f *testing.F) {
	tools := pdforacle.RequireTools(f)
	f.Add([]byte{0, 0, 1, 1, 0, 1, 0, 5, 0, 0, 0, 3})
	f.Add([]byte{1, 0, 1, 5, 1, 2, 1, 3, 2, 0, 0, 1})
	f.Add([]byte{1, 3, 0, 2, 0, 3, 0, 2, 0})
	f.Add([]byte{2, 255, 255, 255})

	dir := f.TempDir()
	engine := newEngine(f)
	paths, resource := writeCatalog(f, dir)

	names := fuzzSourceNames()
	sources := make([]pdfengine.SourceFile, len(names))

	for index, name := range names {
		info, err := engine.Inspect(context.Background(), paths[name])
		if err != nil {
			f.Fatal(err)
		}

		sources[index] = pdfengine.SourceFile{Path: paths[name], Info: info}
	}

	output := filepath.Join(dir, outputFilename)

	f.Fuzz(func(t *testing.T, data []byte) {
		request := decodeRequest(data, sources, resource, output)

		accepted := modelAccepts(&request)

		ctx, cancel := context.WithTimeout(context.Background(), fuzzDeadline)
		defer cancel()

		err := engine.Assemble(ctx, &request)
		if accepted != (err == nil) {
			t.Fatalf("model accepts=%t but Assemble returned %v for %+v", accepted, err, request.Order)
		}

		if err != nil {
			if code := pdfengine.CodeOf(err); code == "" {
				t.Fatalf("error without a code: %v", err)
			}

			return
		}

		checkAssembledFuzzOutput(t, tools, output, request.ExpectedPages)
	})
}

func checkAssembledFuzzOutput(t *testing.T, tools pdforacle.Tools, output string, expected int) {
	t.Helper()

	if err := tools.Check(output); err != nil {
		t.Fatal(err)
	}

	document, err := pdforacle.Load(tools, output)
	if err != nil {
		t.Fatal(err)
	}

	if document.PageCount() != expected {
		t.Fatalf("assembled PDF has %d pages, expected %d", document.PageCount(), expected)
	}
}

// decodeRequest turns fuzz bytes into a request: a header byte for the expected-total adjustment, then
// groups of four bytes (kind/index, start, count, repeat-or-huge flag).
func decodeRequest(data []byte, sources []pdfengine.SourceFile, resource, destination string) pdfengine.AssembleRequest {
	request := pdfengine.AssembleRequest{
		Sources: sources, Resource: &pdfengine.ResourceDocument{Path: resource, Pages: resourcePages}, Destination: destination,
	}

	if len(data) == 0 {
		return request
	}

	adjust := int(data[0] % 3) // 0: exact total, 1: one more, 2: one fewer

	total := 0

	for offset := 1; offset+3 < len(data) && len(request.Order) < maxFuzzRuns; offset += 4 {
		kind, startByte, countByte, flag := data[offset], data[offset+1], data[offset+2], data[offset+3]

		var run pdfengine.Run

		if kind%4 == 3 {
			run = pdfengine.GeneratedPages(int(kind>>2)%(resourcePages+1), int(countByte%20))
		} else {
			run = pdfengine.SourcePages(int(kind>>2)%(len(sources)+1), int(startByte%7), int(countByte%7))
		}

		if flag == 255 && run.Generated {
			run.Count = math.MaxInt
		}

		request.Order = append(request.Order, run)

		if run.Count > 0 && run.Count <= pdfengine.MaxOutputPages-total {
			total += run.Count
		}
	}

	switch adjust {
	case 1:
		total++
	case 2:
		total = max(total-1, 0)
	default:
	}

	request.ExpectedPages = total

	return request
}

// modelAccepts is an independent statement of the request rules, written without sharing the engine's
// code: positive counts, indices and ranges in bounds, whole use of per-occurrence sources, at most one
// legacy-destinations occurrence, a total within MaxOutputPages equal to ExpectedPages, and a non-empty order.
func modelAccepts(request *pdfengine.AssembleRequest) bool {
	if len(request.Order) == 0 {
		return false
	}

	total, legacy := 0, 0

	for _, run := range request.Order {
		if run.Count < 1 || run.Count > pdfengine.MaxOutputPages-total || !modelAcceptsRun(request, run) {
			return false
		}

		total += run.Count

		if !run.Generated && request.Sources[run.Index].Info.LegacyDests {
			legacy++
		}
	}

	return legacy <= 1 && total == request.ExpectedPages
}

// modelAcceptsRun states the index, range and whole-use rules for one run with a positive count.
func modelAcceptsRun(request *pdfengine.AssembleRequest, run pdfengine.Run) bool {
	if run.Generated {
		return run.Index >= 0 && run.Index < resourcePages
	}

	if run.Index < 0 || run.Index >= len(request.Sources) {
		return false
	}

	info := request.Sources[run.Index].Info
	if run.Start < 1 || run.Start+run.Count-1 > info.Pages || run.Start+run.Count < run.Start {
		return false
	}

	return modelAllowsRange(run, info)
}

// modelAllowsRange states that a source whose objects tie to its pages must be used whole.
func modelAllowsRange(run pdfengine.Run, info pdfengine.SourceInfo) bool {
	whole := run.Start == 1 && run.Count == info.Pages
	perOccurrence := info.PageLocal || info.AcroForm || info.NamedDests || info.LegacyDests

	return whole || !perOccurrence
}
