// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	exchange "github.com/resoltico/pdfconcat/internal/wire"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestSignatureDiscardResult(t *testing.T) {
	type counts = exchange.SignatureDiscardCounts
	for _, tc := range []struct {
		name          string
		fixture       func() *pdffixture.Doc
		catalog       string
		want          *counts
		refused, fail bool
	}{
		{"signature", pdffixture.SignatureArtwork, "", &counts{PopulatedSignatureFields: 1, Widgets: 1, SignatureValues: 1}, false, false},
		{"timestamp", timestampField, "", &counts{PopulatedSignatureFields: 1, Widgets: 1, DocumentTimestamps: 1}, false, false},
		{"shared timestamp", timestampField, "/DocTimeStamp 7 0 R", &counts{PopulatedSignatureFields: 1, Widgets: 1, DocumentTimestamps: 1}, false, false},
		{"mixed", timestampField, "/Perms << /UR3 << /Type /Sig >> >>", &counts{PopulatedSignatureFields: 1, Widgets: 1, DocumentTimestamps: 1, SignatureValues: 1, PermissionStructures: 1}, false, false},
		{"metadata", func() *pdffixture.Doc { return pdffixture.Plain("metadata") }, "/Perms << >> /DSS << >> /VRI << >> /AcroForm << /Fields [] /SigFlags 3 >>", &counts{PermissionStructures: 1, ValidationStores: 2, SignatureFlagEntries: 1}, false, false},
		{"unsigned", func() *pdffixture.Doc { return pdffixture.EmptySignatureField("unsigned") }, "", nil, false, false},
		{"refused", pdffixture.SignatureArtwork, "", nil, true, false},
		{"output failure", timestampField, "", &counts{PopulatedSignatureFields: 1, Widgets: 1, DocumentTimestamps: 1}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := tc.fixture()
			if tc.catalog != "" {
				cat := bytes.TrimSuffix(source.Objs[0], []byte(">>"))
				source.Objs[0] = fmt.Appendf(cat, " %s >>", tc.catalog)
			}
			if err := source.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			policy := "discard"
			if tc.refused {
				policy = "refuse"
			}
			plan := fmt.Sprintf(`{"version":11,"signatures":%q,"items":[%s{"source":"source.pdf"}]}`, policy, strings.Repeat(`{"source":"source.pdf"},`, 20))
			output := filepath.Join(dir, "out.pdf")
			if tc.fail {
				if runtime.GOOS == "windows" {
					t.Skip("requires a POSIX directory without write permission")
				}
				parent := filepath.Join(dir, "read-only")
				if err := os.Mkdir(parent, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(parent, 0700); err != nil {
						t.Error(err)
					}
				})
				output = filepath.Join(parent, "out.pdf")
				if err := os.WriteFile(output, nil, 0600); err == nil {
					t.Skip("account can stage files in directories without write permission")
				} else if !os.IsPermission(err) {
					t.Fatal(err)
				}
			}
			for _, op := range []string{"check", "build"} {
				report := filepath.Join(dir, op+".json")
				args := []string{op, "--plan", "-", "--base-dir", dir, "--report", report, "-o", output}
				summary, exit, _ := invokeJSON(t, args, plan)
				wantExit := 0
				switch {
				case tc.refused:
					wantExit = 2
				case tc.fail && op == "build":
					wantExit = 1
				}
				if exit != wantExit || summary.Output == nil || summary.Output.Published != (op == "build" && wantExit == 0) {
					t.Fatalf("%s exit %d, want %d and matching publication: %+v", op, exit, wantExit, summary)
				}
				if wantExit == 1 && !hasDiagnostic(summary, "output_write_failed") {
					t.Fatalf("expected staged output failure: %+v", summary.Diagnostics)
				}
				raw, err := os.ReadFile(report)
				if err != nil {
					t.Fatal(err)
				}
				validateResult(t, raw)
				var full exchange.Result
				if err = json.Unmarshal(raw, &full); err != nil {
					t.Fatal(err)
				}
				for _, view := range []struct {
					result         exchange.Result
					items, omitted int
				}{{summary, 20, 1}, {full, 21, 0}} {
					found := 0
					for _, d := range view.result.Diagnostics {
						if d.Code.String() != "source_signature_discarded" {
							continue
						}
						found++
						if tc.want == nil || d.SignatureDiscard == nil || *d.SignatureDiscard != *tc.want || d.PDFLocation == nil || len(d.Items) != view.items || d.ItemsOmitted != view.omitted {
							t.Fatalf("discard finding: %+v, counts: %+v", d, d.SignatureDiscard)
						}
						for i, item := range d.Items {
							if want := fmt.Sprintf("/items/%d", i); item != want {
								t.Fatalf("consumer ref %q, want %q", item, want)
							}
						}
					}
					if found > 1 || (found == 1) != (tc.want != nil) {
						t.Fatalf("discard warning count %d for %s: %v", found, op, view.result.Diagnostics)
					}
				}
			}
		})
	}
}

func timestampField() *pdffixture.Doc {
	d := pdffixture.SignatureArtwork()
	d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
	d.Objs[5] = bytes.Replace(d.Objs[5], []byte(" /AP << /N 8 0 R >>"), nil, 1)
	d.Objs[6] = bytes.ReplaceAll(d.Objs[6], []byte("/adbe.pkcs7.detached"), []byte("/ETSI.RFC3161"))
	d.Objs[7] = []byte("null")
	return d
}

func TestSignatureDiscardResultSchemaRejectsInvalidEvidence(t *testing.T) {
	var schemaOutput, stderr bytes.Buffer
	if exit := run([]string{"schema", "result"}, strings.NewReader(""), &schemaOutput, &stderr); exit != 0 {
		t.Fatalf("schema command exit %d: %s", exit, &stderr)
	}
	var document any
	if err := json.Unmarshal(schemaOutput.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("result.json")
	if err != nil {
		t.Fatal(err)
	}
	var versionOutput bytes.Buffer
	if exit := run([]string{"version", "--json"}, strings.NewReader(""), &versionOutput, &stderr); exit != 0 {
		t.Fatalf("version command exit %d: %s", exit, &stderr)
	}
	validateResult(t, versionOutput.Bytes())
	dir := t.TempDir()
	if err := timestampField().WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	const plan = `{"version":11,"signatures":"discard","structure":"discard","items":[{"source":"source.pdf"}]}`
	args := []string{"check", "--plan", "-", "--base-dir", dir}
	summary, exit, raw := invokeJSON(t, args, plan)
	if exit != 0 || len(summary.Diagnostics) != 1 {
		t.Fatal(raw)
	}
	var stdout bytes.Buffer
	const detail = "Counts of removed state are populated signature fields (1), widgets (1), signature values (0), document timestamps (1), permission structures (0), validation stores (0) and signature flag entries (0)."
	if exit := run(args, strings.NewReader(plan), &stdout, &stderr); exit != 0 ||
		!strings.Contains(stderr.String(), detail) || summary.Diagnostics[0].Detail != detail {
		t.Fatalf("text and JSON should show the same counts: %d, %s, %s", exit, &stderr, raw)
	}
	for _, tc := range []struct {
		name string
		edit func(root, finding, counts map[string]any)
	}{
		{"v10", func(root, _, _ map[string]any) { root["format_version"] = float64(10) }},
		{"wrong code", func(_, d, _ map[string]any) { d["code"] = "source_viewer_settings_removed" }},
		{"missing payload", func(_, d, _ map[string]any) { delete(d, "signature_discard") }},
		{"null payload", func(_, d, _ map[string]any) { d["signature_discard"] = nil }},
		{"missing zero field", func(_, _, c map[string]any) { delete(c, "signature_values") }},
		{"negative", func(_, _, c map[string]any) { c["widgets"] = float64(-1) }},
		{"fractional", func(_, _, c map[string]any) { c["widgets"] = 0.5 }},
		{"extra key", func(_, _, c map[string]any) { c["unknown"] = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var root map[string]any
			if err := json.Unmarshal([]byte(raw), &root); err != nil {
				t.Fatal(err)
			}
			diagnostics, ok := root["diagnostics"].([]any)
			if !ok || len(diagnostics) != 1 {
				t.Fatalf("expected one diagnostic: %v", root["diagnostics"])
			}
			finding, ok := diagnostics[0].(map[string]any)
			if !ok {
				t.Fatalf("expected a diagnostic object: %v", diagnostics[0])
			}
			counts, ok := finding["signature_discard"].(map[string]any)
			if !ok {
				t.Fatalf("expected a counts object: %v", finding["signature_discard"])
			}
			tc.edit(root, finding, counts)
			if err := schema.Validate(root); err == nil {
				t.Fatal("schema accepted an invalid signature discard result")
			}
		})
	}
}
