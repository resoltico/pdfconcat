// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	sourceEmptyPageTree = "<< /Type /Pages /Kids [] /Count 0 >>"
	sourceReplayMarker  = "INSPECTED SOURCE"
	sourceReplaySeed    = "preserve destination after changed source"
)

func TestFittedImportRejectsSourceChangedAfterRealInspection(t *testing.T) {
	t.Parallel()

	for _, target := range []PageSize{{595, 842}, {612, 1008}} {
		t.Run(targetName(target), func(t *testing.T) {
			t.Parallel()
			verifyChangedSourceImport(t, target)
		})
	}
}

func verifyChangedSourceImport(t *testing.T, target PageSize) {
	t.Helper()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()

	path := filepath.Join(directory, fitRenderSource)
	if err = pdffixture.Plain(sourceReplayMarker).WriteFile(path); err != nil {
		t.Fatal(err)
	}

	info, err := engine.Inspect(t.Context(), path, &target)
	if err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(directory, fitInsertionOutput)

	request := AssembleRequest{
		FitTarget: &target, Sources: []SourceFile{{Path: path, Info: info}},
		Order: []Run{SourcePages(0, 1, 1)}, ExpectedPages: 1, Destination: output,
	}
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatalf("unchanged inspected source failed: %v", err)
	}

	changed := sourceIdentityPDF(fitCatalogSourceVersion, "", []string{catalog, sourceEmptyPageTree}, nil, nil)
	sourceWrite(t, path, changed)
	sourceWrite(t, output, []byte(sourceReplaySeed))

	if _, err = engine.Inspect(t.Context(), path, &target); CodeOf(err) != CodeNoPages {
		t.Fatalf("fresh inspection accepted empty replacement: %v", err)
	}

	err = engine.Assemble(t.Context(), &request)
	if CodeOf(err) != CodeFitUnsupported || !strings.Contains(err.Error(), "page count changed after fit inspection") {
		t.Fatalf("real inspected-source drift was not refused at fit replay: %v", err)
	}

	ordinary := request

	ordinary.FitTarget = nil
	if err = engine.Assemble(t.Context(), &ordinary); err == nil || !strings.Contains(err.Error(), errImportedPageCount.Error()) {
		t.Fatalf("ordinary import failed to reject genuine page-count drift: %v", err)
	}

	assertReinspectionFilesPreserved(t, directory, changed)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err = engine.Assemble(ctx, &request); CodeOf(err) != CodeCanceled {
		t.Fatalf("cancellation lost during changed-source import: %v", err)
	}
}

func assertReinspectionFilesPreserved(t *testing.T, directory string, changed []byte) {
	t.Helper()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()

	content, err := root.ReadFile(fitInsertionOutput)
	if err != nil || !bytes.Equal(content, []byte(sourceReplaySeed)) {
		t.Fatalf("changed-source failure modified destination: %q %v", content, err)
	}

	content, err = root.ReadFile(fitRenderSource)
	if err != nil || !bytes.Equal(content, changed) {
		t.Fatalf("import modified replacement input: %v", err)
	}
}
