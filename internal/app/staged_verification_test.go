// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

func TestFailedStagedVerificationPreservesIncumbent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	if err := os.WriteFile(target, []byte("incumbent"), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := files.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Abort() })
	size := packet.Size{Width: 300, Height: 400}
	assembly := pdf.Assembly{Parts: []pdf.Part{{
		Part: packet.Part{
			Leaf:  packet.Leaf{Kind: packet.PartPage, Ref: "generated", Count: 1},
			Pages: 1, FirstPage: 1, LastPage: 1, Size: size, OutputSize: size,
		},
		Repeat:    1,
		Generated: []compose.Page{{Width: size.Width, Height: size.Height}},
	}}}
	assembled, err := pdf.Assemble(t.Context(), assembly, stage)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := stage.Reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := assembled.VerifyReader(t.Context(), reader, 1, assembly); err != nil {
		t.Fatal(err)
	}
	if err := stage.Truncate(8); err != nil {
		t.Fatal(err)
	}
	roles := files.Roles{Output: target, Overwrite: true, InputsKnown: true}
	published, ds := verifyAndPublishPDF(t.Context(), stage, roles, assembled, 1, assembly)
	if published || stage.Published() || len(ds) != 1 || ds[0].Code != diag.OutputInvalid {
		t.Fatalf("publication = %v, findings = %v", published, ds)
	}
	if err := stage.Abort(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "incumbent" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	if _, err := os.Stat(stage.Path()); !os.IsNotExist(err) {
		t.Fatalf("abort left the stage: %v", err)
	}
}
