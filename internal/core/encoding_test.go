// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func encodingControl(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write([]byte("BT /F1 12 Tf 10 20 Td (VALID) Tj ET\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestEncodingAdmissionHasWholeSourceProvenance(t *testing.T) {
	valid := encodingControl(t)
	for _, tc := range []struct {
		name      string
		pages     [][]pdffixture.EncodedStream
		selection [][2]int
		page      int
	}{
		{"bad-header", [][]pdffixture.EncodedStream{{{Data: []byte("bad zlib header"), Filter: "/FlateDecode"}}}, nil, 1},
		{"bad-later-member", [][]pdffixture.EncodedStream{{{Data: valid, Filter: "/FlateDecode"}, {Data: []byte("bad"), Filter: "/FlateDecode"}}}, nil, 1},
		{"truncated-checksum", [][]pdffixture.EncodedStream{{{Data: valid[:len(valid)-1], Filter: "/FlateDecode"}}}, nil, 1},
		{"unknown-filter", [][]pdffixture.EncodedStream{{{Data: valid, Filter: "/MadeUpDecode"}}}, nil, 1},
		{"bad-omitted-page", [][]pdffixture.EncodedStream{{{Data: valid, Filter: "/FlateDecode"}}, {{Data: []byte("bad"), Filter: "/FlateDecode"}}}, [][2]int{{1, 1}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			good := filepath.Join(dir, "good.pdf")
			bad := filepath.Join(dir, "bad.pdf")
			if err := pdffixture.Plain("GOOD").WriteFile(good); err != nil {
				t.Fatal(err)
			}
			doc := pdffixture.EncodedPages("encoded", tc.pages...)
			original := doc.Bytes()
			if err := os.WriteFile(bad, original, 0600); err != nil {
				t.Fatal(err)
			}
			source := func() map[string]any {
				m := map[string]any{"source": bad}
				if tc.selection != nil {
					m["pages"] = tc.selection
				}
				return m
			}
			plan, err := json.Marshal(map[string]any{"version": 5, "items": []any{map[string]any{"source": good}, source(), source()}})
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range []core.Operation{core.Check, core.Build} {
				output := filepath.Join(dir, string(operation)+".pdf")
				r := core.Execute(t.Context(), core.Request{Operation: operation, WorkingDir: dir, Input: core.PlanBytes{Data: plan, Label: "encoding.json"}, Output: output, Report: filepath.Join(dir, string(operation)+".json")})
				schemaValid(t, r)
				if r.Status != "invalid" || core.ExitCode(r) != 2 || r.Output.Published || !r.Report.Written || len(r.Diagnostics) != 1 {
					t.Fatalf("unexpected admission outcome: %+v", r)
				}
				d := r.Diagnostics[0]
				if d.Code.String() != "source_invalid" || d.File != bad || !reflect.DeepEqual(d.Items, []string{"/items/1", "/items/2"}) || d.PDFLocation == nil || d.PDFLocation.Page != tc.page || d.PDFLocation.Object != len(doc.Objs) || d.Detail == "" {
					t.Fatalf("lost source encoding evidence: %+v", d)
				}
				if _, err = os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("refusal published a PDF", err)
				}
				for _, part := range r.Parts[1:] {
					if part.SHA256 != fmt.Sprintf("%x", sha256.Sum256(original)) {
						t.Fatal("refused original identity was lost")
					}
				}
			}
			unchanged, err := os.ReadFile(bad)
			if err != nil || !bytes.Equal(unchanged, original) {
				t.Fatal("original bytes changed", err)
			}
		})
	}
}

func TestValidCompressedPageBuilds(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "valid.pdf")
	if err := pdffixture.EncodedPages("valid", []pdffixture.EncodedStream{{Data: encodingControl(t), Filter: "/FlateDecode"}}).WriteFile(source); err != nil {
		t.Fatal(err)
	}
	r := core.Execute(t.Context(), core.Request{Operation: core.Build, WorkingDir: dir, Input: core.Items{core.SourceItem{Ref: "valid", Path: source}}, Output: filepath.Join(dir, "output.pdf")})
	if r.Status != "ok" || !r.Output.Published || r.Pages.Total == nil || *r.Pages.Total != 1 {
		t.Fatal(r)
	}
}
