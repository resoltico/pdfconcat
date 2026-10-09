// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	catalog = "<< /Type /Catalog /Pages 2 0 R >>"
	page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>"
)

var errBoom = errors.New("boom")

// readUnvalidated parses a document without pdfcpu's validation, so tests can present the walker and
// the inspection with page trees that validation would have rejected first.
func readUnvalidated(t *testing.T, doc *pdffixture.Doc) *model.Context {
	t.Helper()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	pdf, err := api.ReadContext(context.Background(), bytes.NewReader(doc.Bytes()), engine.conf.Clone())
	if err != nil {
		t.Fatalf(readFailureFormat, err)
	}

	return pdf
}

func rawDoc(objects ...string) *pdffixture.Doc {
	doc := &pdffixture.Doc{Version: fitCatalogSourceVersion}
	for _, object := range objects {
		doc.Objs = append(doc.Objs, []byte(object))
	}

	return doc
}

func TestWalkPagesRejectsCorruptTrees(t *testing.T) {
	t.Parallel()

	deep := make([]string, 0, maxPageTreeDepth+4)
	deep = append(deep, catalog)

	for level := range maxPageTreeDepth + 2 {
		deep = append(deep, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", level+3))
	}

	deep = append(deep, page)

	cases := []struct {
		name    string
		doc     *pdffixture.Doc
		wantErr string
	}{
		{"cycle", rawDoc(catalog, "<< /Type /Pages /Kids [2 0 R] /Count 1 >>"), "reachable twice"},
		{"page listed twice", rawDoc(catalog, "<< /Type /Pages /Kids [3 0 R 3 0 R] /Count 2 >>", page), "reachable twice"},
		{"too deep", rawDoc(deep...), "nested too deeply"},
		{"null node", rawDoc(catalog, singlePageTreeBody, nullPDFObject), "not an indirect reference"},
		{"kid is not a reference", rawDoc(catalog, "<< /Type /Pages /Kids [<< /Type /Page >>] /Count 1 >>"), "not an indirect reference"},
		{"kids is not an array", rawDoc(catalog, "<< /Type /Pages /Kids 5 /Count 1 >>"), "kids"},
		{"node is not a dictionary", rawDoc(catalog, singlePageTreeBody, "[1 2]"), "page tree node 3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pdf := readUnvalidated(t, tc.doc)

			root, err := pdf.Pages()
			if err != nil {
				t.Fatal(err)
			}

			err = walkPages(context.Background(), pdf, *root, func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf(errorContainingFormat, err, tc.wantErr)
			}
		})
	}
}

func TestWalkPagesStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	pdf := readUnvalidated(t, pdffixture.Plain("P"))

	root, err := pdf.Pages()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = walkPages(ctx, pdf, *root, func(types.IndirectRef, types.Dict, inheritedAttrs) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestInspectContextRejectsInconsistentDocuments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  *pdffixture.Doc
		code Code
	}{
		{"no pages", rawDoc(catalog, "<< /Type /Pages /Kids [] /Count 0 >>"), CodeNoPages},
		{"count disagrees with tree", rawDoc(catalog, "<< /Type /Pages /Kids [3 0 R] /Count 5 >>", page), CodeInvalid},
		{
			"names is not a dictionary",
			rawDoc("<< /Type /Catalog /Pages 2 0 R /Names 5 >>", singlePageTreeBody, page),
			CodeInvalid,
		},
		{"no page tree", rawDoc(emptyCatalogBody), CodeInvalid},
		{"corrupt tree", rawDoc(catalog, "<< /Type /Pages /Kids [2 0 R] /Count 1 >>"), CodeInvalid},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := inspectContext(context.Background(), readUnvalidated(t, tc.doc), "doc.pdf")
			if got := CodeOf(err); got != tc.code {
				t.Fatalf("got code %q (%v), want %q", got, err, tc.code)
			}
		})
	}
}

func TestInspectContextReportsCancellationDuringWalk(t *testing.T) {
	t.Parallel()

	pdf := readUnvalidated(t, pdffixture.Pages("P", 3))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := inspectContext(ctx, pdf, "doc.pdf")
	if got := CodeOf(err); got != CodeCanceled {
		t.Fatalf("got code %q (%v)", got, err)
	}
}

func TestErrorFormatting(t *testing.T) {
	t.Parallel()

	cause := errBoom

	withPath := newError(CodeInvalid, 2, "a.pdf", cause)
	if got, want := withPath.Error(), "pdf_invalid: a.pdf: boom"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	withoutPath := newError(CodeRequestInvalid, NoSource, "", cause)
	if got, want := withoutPath.Error(), "assemble_request_invalid: boom"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if !errors.Is(withPath, cause) {
		t.Error("Unwrap does not expose the cause")
	}

	if got := CodeOf(fmt.Errorf("context: %w", withPath)); got != CodeInvalid {
		t.Errorf("CodeOf through a wrapper: %q", got)
	}

	if got := CodeOf(cause); got != "" {
		t.Errorf("CodeOf of a foreign error: %q", got)
	}
}

func TestVersionConversion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   model.Version
		want Version
	}{{model.V10, 10}, {model.V14, 14}, {model.V17, Version17}, {model.V20, Version20}} {
		if got := versionOf(tc.in); got != tc.want {
			t.Errorf("versionOf(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestVerifyOutputFailures presents verifyOutput with files the assembly itself would never write.
func TestVerifyOutputFailures(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	write := func(name string, content []byte) string {
		path := filepath.Join(dir, name)
		if writeErr := os.WriteFile(path, content, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}

		return path
	}

	valid := write("valid.pdf", pdffixture.Pages("P", 2).Bytes())
	emptyCrop := write(
		"crop.pdf",
		pdffixture.WithBoxes("P", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 9 9]", Crop: "[1 1 1 5]"}).Bytes(),
	)
	garbage := write("garbage.pdf", []byte("not a pdf"))

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name     string
		path     string
		want     Code
		pages    int
		canceled bool
	}{
		{"matches", valid, "", 2, false},
		{"page count differs", valid, CodeOutputPageCount, 3, false},
		{"not a PDF", garbage, CodeOutputInvalid, 2, false},
		{"first page geometry unusable", emptyCrop, CodeOutputInvalid, 1, false},
		{"missing", filepath.Join(dir, "absent.pdf"), CodeUnreadable, 2, false},
		{"canceled", valid, CodeCanceled, 2, true},
	}

	for _, tc := range cases {
		ctx := context.Background()
		if tc.canceled {
			ctx = canceledCtx
		}

		verifyErr := engine.verifyOutput(ctx, &AssembleRequest{Destination: tc.path, ExpectedPages: tc.pages})
		if got := CodeOf(verifyErr); got != tc.want {
			t.Errorf("%s: got code %q (%v), want %q", tc.name, got, verifyErr, tc.want)
		}
	}
}

func TestPoolRejectsDamagedCatalogs(t *testing.T) {
	t.Parallel()

	for name, check := range map[string]func(t *testing.T){
		"names is not a dictionary": checkNamesNotDictionary,
		"no page tree":              checkNoPageTree,
		"root is not a dictionary":  checkRootNotDictionary,
		"merge appended nothing":    checkMergeAppendedNothing,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			check(t)
		})
	}
}

func checkNamesNotDictionary(t *testing.T) {
	t.Helper()

	damaged := pool{
		pdf: readUnvalidated(
			t,
			rawDoc("<< /Type /Catalog /Pages 2 0 R /Names 5 >>", singlePageTreeBody, page),
		),
	}
	if err := damaged.keepOnlyDestinationNames(t.Context()); err == nil {
		t.Fatal("no error")
	}

	if err := damaged.reorder(t.Context(), nil); err == nil {
		t.Fatal("reorder accepted a damaged /Names")
	}
}

func checkNoPageTree(t *testing.T) {
	t.Helper()

	treeless := pool{pdf: readUnvalidated(t, rawDoc(emptyCatalogBody))}

	if _, _, err := treeless.root(t.Context()); err == nil {
		t.Error("root found a page tree")
	}

	if _, err := treeless.lastTree(t.Context()); err == nil {
		t.Error("lastTree found a page tree")
	}

	if err := treeless.reorder(t.Context(), nil); err == nil {
		t.Error("reorder accepted a missing page tree")
	}
}

func checkRootNotDictionary(t *testing.T) {
	t.Helper()

	damaged := pool{pdf: readUnvalidated(t, rawDoc(catalog, "[1 2]"))}

	if _, _, err := damaged.root(t.Context()); err == nil {
		t.Error("root accepted an array")
	}
}

func checkMergeAppendedNothing(t *testing.T) {
	t.Helper()

	for name, tree := range map[string]string{
		"no kids":          "<< /Type /Pages /Count 0 >>",
		"kids not array":   "<< /Type /Pages /Kids 5 /Count 0 >>",
		"empty kids":       "<< /Type /Pages /Kids [] /Count 0 >>",
		"last kid not ref": "<< /Type /Pages /Kids [<< >>] /Count 0 >>",
	} {
		merged := pool{pdf: readUnvalidated(t, rawDoc(catalog, tree))}
		if _, err := merged.lastTree(t.Context()); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestPoolMergeFailureIsAttributedToTheSource(t *testing.T) {
	t.Parallel()

	p := pool{}
	p.adopt(readUnvalidated(t, pdffixture.Plain("BASE")))

	err := p.merge(context.Background(), 4, "broken.pdf", readUnvalidated(t, rawDoc(emptyCatalogBody)))
	if CodeOf(err) != CodeAssemblyFailed {
		t.Fatalf("got %v", err)
	}

	var failure *Error
	if !errors.As(err, &failure) || failure.Source != 4 || failure.Path != "broken.pdf" {
		t.Fatalf("error does not identify the source: %v", err)
	}
}
