// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	fitInsertionOutput    = "insertion-output.pdf"
	fitFreeHeadDiagnostic = "xref object 0 must be free"
	fitClassicXRef        = "classic"
)

func TestReadRejectsAllocatorInvalidSourceBeforeInspectionAndImport(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	for _, encoding := range []string{fitClassicXRef, "stream type1", "stream type2"} {
		for _, target := range []*PageSize{nil, {595, 842}, {612, 1008}} {
			verifyAllocatorInvalidSourceRefusal(t, engine, encoding, target)
		}
	}
}

func verifyAllocatorInvalidSourceRefusal(t *testing.T, engine *Engine, encoding string, target *PageSize) {
	t.Helper()
	root, source, info := fitInvalidFreeHeadSource(t, engine, encoding, target)
	path := filepath.Join(root.Name(), fitRenderSource)

	seed := []byte("preserve existing output after free-head refusal")
	if err := root.WriteFile(fitInsertionOutput, seed, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := engine.Inspect(t.Context(), path, target)
	assertFitFreeHeadRefusal(t, err)

	request := AssembleRequest{
		FitTarget:     target,
		Sources:       []SourceFile{{Path: path, Info: info}},
		Order:         []Run{SourcePages(0, 1, 1)},
		ExpectedPages: 1,
		Destination:   filepath.Join(root.Name(), fitInsertionOutput),
	}
	assertFitFreeHeadRefusal(t, engine.Assemble(t.Context(), &request))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = engine.Inspect(ctx, path, target)
	if CodeOf(err) != CodeCanceled {
		t.Fatalf("known cancellation lost during invalid-source inspection: %v", err)
	}

	if err = engine.Assemble(ctx, &request); CodeOf(err) != CodeCanceled {
		t.Fatalf("known cancellation lost during invalid-source import: %v", err)
	}

	assertFitSourceAndOutputUnchanged(t, root, source, seed)
}

func assertFitFreeHeadRefusal(t *testing.T, err error) {
	t.Helper()

	if CodeOf(err) != CodeInvalid || !strings.Contains(err.Error(), fitFreeHeadDiagnostic) {
		t.Fatalf("retained in-use free head did not fail shared native reading: %v", err)
	}
}

func assertFitSourceAndOutputUnchanged(t *testing.T, root *os.Root, source, seed []byte) {
	t.Helper()

	for name, expected := range map[string][]byte{fitRenderSource: source, fitInsertionOutput: seed} {
		actual, err := root.ReadFile(name)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("free-head refusal changed source or seeded output %s: %v", name, err)
		}
	}
}

func fitInvalidFreeHeadSource(t *testing.T, engine *Engine, encoding string, target *PageSize) (*os.Root, []byte, SourceInfo) {
	t.Helper()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	source := pdffixture.Pages("free-list insertion", 1).Bytes()
	if encoding != fitClassicXRef {
		source = fitPackedSource(sourceEmptyResources, emptyAppearanceDrawing, nullPDFObject)
	}

	if err = root.WriteFile(fitRenderSource, source, 0o600); err != nil {
		t.Fatal(err)
	}
	// Captured facts come from successful public inspection of the real, valid file.
	// Replacing its head before import exercises the defensive read without forged SourceInfo.
	info, err := engine.Inspect(t.Context(), filepath.Join(root.Name(), fitRenderSource), target)
	if err != nil || info.Pages != 1 {
		t.Fatalf("valid source before head replacement refused: %v", err)
	}

	source = fitReplaceFreeHead(t, source, encoding)
	if err = root.WriteFile(fitRenderSource, source, 0o600); err != nil {
		t.Fatal(err)
	}

	return root, source, info
}

func fitReplaceFreeHead(t *testing.T, source []byte, encoding string) []byte {
	t.Helper()

	if encoding == fitClassicXRef {
		head := []byte("0000000000 65535 f")
		if bytes.Count(source, head) != 1 {
			t.Fatal("classic source has no unique free-list head")
		}

		return bytes.Replace(source, head, []byte("0000000009 65535 n"), 1)
	}

	marker := []byte("\nstream\n")

	index := bytes.LastIndex(source, marker)
	if index < 0 || index+len(marker)+7 > len(source) {
		t.Fatal("source has no binary xref head")
	}

	start := index + len(marker)
	clear(source[start : start+7])

	if encoding == "stream type1" {
		source[start] = 1
		binary.BigEndian.PutUint32(source[start+1:start+5], 9)
		binary.BigEndian.PutUint16(source[start+5:start+7], 65535)
	} else {
		source[start] = 2
		binary.BigEndian.PutUint32(source[start+1:start+5], 5)
	}

	return source
}
