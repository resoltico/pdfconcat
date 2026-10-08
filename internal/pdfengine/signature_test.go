// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const preservedSignatureDestination = "KEEP"

func TestSignedSourceRejectsInspectionAndDefensiveImport(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	dir := t.TempDir()
	signed := writeDoc(t, dir, "populated-signature", pdffixture.SignatureValue("SIGNED_STATE"))
	plain, info := inspectDoc(t, engine, "plain", pdffixture.Plain("PLAIN"))
	_, err := engine.Inspect(t.Context(), signed)

	failure := requireFailure(t, err, pdfengine.CodeSignatureUnsupported)
	if failure.Path != signed {
		t.Fatalf("wrong signature source: %+v", failure)
	}

	for _, order := range [][]pdfengine.Run{
		{pdfengine.SourcePages(0, 1, 1)},
		{pdfengine.SourcePages(1, 1, 1), pdfengine.SourcePages(0, 1, 1)},
		{pdfengine.SourcePages(0, 1, 1), pdfengine.SourcePages(0, 1, 1)},
	} {
		destination := filepath.Join(t.TempDir(), outputFilename)
		if writeErr := os.WriteFile(destination, []byte(preservedSignatureDestination), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}

		request := &pdfengine.AssembleRequest{
			Destination:   destination,
			Sources:       []pdfengine.SourceFile{{Path: signed, Info: info}, {Path: plain, Info: info}},
			Order:         order,
			ExpectedPages: len(order),
		}

		failure = requireFailure(t, engine.Assemble(t.Context(), request), pdfengine.CodeSignatureUnsupported)
		if failure.Source != 0 || failure.Path != signed {
			t.Fatalf("wrong imported source: %+v", failure)
		}

		content, readErr := os.ReadFile(filepath.Clean(destination))
		if readErr != nil || string(content) != preservedSignatureDestination {
			t.Fatalf("signature refusal modified destination: %q %v", content, readErr)
		}
	}
}

func TestEmptySignatureFieldRemainsSupported(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	path, info := inspectDoc(t, engine, "empty-signature", pdffixture.EmptySignatureField("UNSIGNED"))

	request := &pdfengine.AssembleRequest{
		Sources:       []pdfengine.SourceFile{{Path: path, Info: info}},
		Order:         []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)},
		ExpectedPages: 1,
		Destination:   filepath.Join(t.TempDir(), outputFilename),
	}
	if err := engine.Assemble(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}
