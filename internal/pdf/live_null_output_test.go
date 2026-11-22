// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestBuildPreservesLiveIndirectNullObjects(t *testing.T) {
	for _, test := range []struct {
		name, key string
		doc       *pdffixture.Doc
		owner     int
	}{
		{"unsigned-signature-value", "V", pdffixture.EmptySignatureField("unsigned"), 6},
		{"private-page-entry", "PrivateNull", pdffixture.Plain("drawing"), 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.doc.Objs = append(test.doc.Objs, []byte("null"))
			insertEntry(test.doc, test.owner, fmt.Sprintf("/%s %d 0 R", test.key, len(test.doc.Objs)))
			for _, op := range []app.Operation{app.Check, app.Build} {
				result, _, out := selectiveRequest(t, test.doc.Bytes(), "", op)
				if result.Status != "ok" || result.Output.Published != (op == app.Build) {
					t.Fatal(result.Diagnostics)
				}
				if op == app.Build {
					assertLiveNullObject(t, out, test.key)
				}
			}
		})
	}
}

func assertLiveNullObject(t *testing.T, path, key string) {
	t.Helper()
	data := readPDF(t, path)
	reference := regexp.MustCompile(`/` + regexp.QuoteMeta(key) + ` ([0-9]+) 0 R\b`).FindSubmatch(data)
	start := regexp.MustCompile(`\nstartxref\n([0-9]+)\n%%EOF\n$`).FindSubmatch(data)
	if len(reference) != 2 || len(start) != 2 {
		t.Fatalf("missing indirect /%s or startxref", key)
	}
	number, err := strconv.Atoi(string(reference[1]))
	if err != nil || number < 1 {
		t.Fatalf("invalid referenced object number %q: %v", reference[1], err)
	}
	xrefOffset, err := strconv.Atoi(string(start[1]))
	if err != nil || xrefOffset < 0 || xrefOffset >= len(data) {
		t.Fatalf("invalid xref offset %q: %v", start[1], err)
	}
	table := bytes.Split(data[xrefOffset:], []byte("\n"))
	if len(table) <= number+2 || string(table[0]) != "xref" {
		t.Fatal("xref does not contain the referenced object")
	}
	entry := strings.Fields(string(table[number+2]))
	if len(entry) != 3 || entry[1] != "00000" || entry[2] != "n" {
		t.Fatalf("object %d lacks a live generation-zero xref entry: %q", number, table[number+2])
	}
	offset, err := strconv.Atoi(entry[0])
	header := fmt.Appendf(nil, "%d 0 obj\nnull\nendobj\n", number)
	if err != nil || offset < 0 || offset >= xrefOffset || !bytes.HasPrefix(data[offset:], header) {
		t.Fatalf("xref offset %q does not point at object %d containing null", entry[0], number)
	}
	qpdfCheck(t, path)
	if actual := strings.TrimSpace(string(run(t, qpdfTool, "--show-object="+string(reference[1])+",0", path))); actual != "null" {
		t.Fatalf("qpdf found %q at object %d", actual, number)
	}
}
