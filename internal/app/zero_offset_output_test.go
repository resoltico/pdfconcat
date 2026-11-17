// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/oracle"
)

func TestZeroOffsetAdmissionBuildParityAndFidelity(t *testing.T) {
	tools := oracle.RequireTools(t)
	var reference []byte
	for _, name := range []string{"clean", "unreferenced-zero", "incremental-shadow-zero"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile("../pdf/testdata/xref/" + name + ".pdf")
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(dir, "source.pdf")
			if err = os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			req := app.Request{InspectionWorkers: 2, Operation: app.Check, Input: app.Items{app.SourceItem{Ref: "source", Path: source}}, WorkingDir: dir, Output: "result.pdf"}
			checked := app.Execute(t.Context(), req)
			req.Operation = app.Build
			built := app.Execute(t.Context(), req)
			for _, r := range []app.Result{checked, built} {
				if r.Status != "ok" {
					t.Fatal(r.Diagnostics)
				}
				warnings := 0
				for _, d := range r.Diagnostics {
					if d.Code.String() == "source_xref_entry_ignored" {
						warnings++
						if d.File != source || d.PDFLocation == nil || d.PDFLocation.Object != 6 {
							t.Fatal(d)
						}
					}
				}
				want := 1
				if name == "clean" {
					want = 0
				}
				if warnings != want {
					t.Fatal(r.Diagnostics)
				}
				sum := sha256.Sum256(data)
				if r.Parts[0].SHA256 != hex.EncodeToString(sum[:]) {
					t.Fatal("original digest changed")
				}
			}
			path := filepath.Join(dir, "result.pdf")
			if err = tools.Check(path); err != nil {
				t.Fatal(err)
			}
			raw, err := exec.CommandContext(t.Context(), tools.PDFToText, path, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte("Synthetic xref evidence")) {
				t.Fatal(string(raw))
			}
			prefix := filepath.Join(dir, "render")
			if out, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-r", "30", "-singlefile", path, prefix).CombinedOutput(); err != nil {
				t.Fatalf("render: %v %s", err, out)
			}
			pixels, err := os.ReadFile(prefix + ".ppm")
			if err != nil {
				t.Fatal(err)
			}
			if name == "clean" {
				reference = pixels
			} else if !bytes.Equal(reference, pixels) {
				t.Fatal("accepted source changed rendered content")
			}
			if !bytes.Equal(data, mustRead(t, source)) {
				t.Fatal("source bytes changed")
			}
		})
	}
}
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
