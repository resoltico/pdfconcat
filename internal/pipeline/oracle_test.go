// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pipeline_test

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pipeline"
)

func TestUnicodeSeparatorExtractionEmbeddingAndSize(t *testing.T) {
	tools := oracle.RequireTools(t)
	dir := t.TempDir()
	source := pdffixture.Plain("SOURCE")
	if err := source.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	text := "Pielikums Ā ļ ķ\nΕλληνικά\nКириллица ā ļ"
	plan, err := json.Marshal(map[string]any{"version": 2, "output": "packet.pdf", "items": []any{"source.pdf", map[string]any{"page": map[string]any{"text": map[string]any{"value": text}}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan.json")
	if err = os.WriteFile(path, plan, 0600); err != nil {
		t.Fatal(err)
	}
	r := pipeline.Run(t.Context(), cli.Command{Name: "build", Plan: path}, dir, strings.NewReader(""))
	if r.Status != "ok" || !r.Output.Published {
		t.Fatalf("packet failed: %+v", r.Diagnostics)
	}
	if *r.Output.Bytes-int64(len(source.Bytes())) >= 30000 {
		t.Fatalf("short separator grew output by %d bytes", *r.Output.Bytes-int64(len(source.Bytes())))
	}
	extracted, err := exec.CommandContext(t.Context(), tools.PDFToText, "-f", "2", "-l", "2", "-raw", r.Output.Path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(string(extracted), "\n\f") != text {
		t.Fatalf("extracted %q, want %q", extracted, text)
	}
	fonts, err := exec.CommandContext(t.Context(), tools.PDFFonts, r.Output.Path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fonts), "yes") {
		t.Fatalf("generated font not embedded: %s", fonts)
	}
	if err = tools.Check(r.Output.Path); err != nil {
		t.Fatal(err)
	}
}
