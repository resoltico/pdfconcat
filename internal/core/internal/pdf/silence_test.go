// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestLibraryLeavesStandardStreamsSilent(t *testing.T) {
	if os.Getenv("PDFCONCAT_LIBRARY_SILENCE_HELPER") == "1" {
		if err := silentLibraryBuild(t); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestLibraryLeavesStandardStreamsSilent$")
	command.Env = append(os.Environ(), "PDFCONCAT_LIBRARY_SILENCE_HELPER=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err = command.Run(); err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("library stream contract: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func silentLibraryBuild(t *testing.T) error {
	dir, err := os.MkdirTemp("", "pdfconcat-silence-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "source.pdf")
	source := pdffixture.Versioned("version20", "2.0", "")
	if err = source.WriteFile(path); err != nil {
		return err
	}
	facts, _, ds := pdf.Inspect(t.Context(), source.Bytes(), nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		return fmt.Errorf("inspection: %+v", ds)
	}
	layout, ds := resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Path: path, Ref: path}, &packet.SourceItem{Path: path, Ref: path}}), map[string]packet.SourceFacts{path: facts.SourceFacts})
	if len(ds) > 0 {
		return fmt.Errorf("layout: %+v", ds)
	}
	var output bytes.Buffer
	_, err = pdf.Assemble(t.Context(), testAssembly(layout, map[string]pdf.SourceFacts{path: facts}, wholeOccurrences(layout, map[string]pdf.SourceFacts{path: facts}), nil, nil, nil), &output)
	return err
}
