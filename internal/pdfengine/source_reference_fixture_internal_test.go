// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	sourceSixReference     = "6 0 R"
	sourceEmptyResources   = "/Resources <<>>"
	sourceFiveReference    = "5 0 R"
	sourceMissingReference = "99 0 R"
	sourceTargetOutput     = "output.pdf"
	sourceHexDrawing       = "712051>"
	sourceHexFilter        = "/ASCIIHexDecode"
)

func sourceWrite(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sourceRead(t *testing.T, path string) ([]byte, error) {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open source fixture: %w", err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()

	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("read source fixture: %w", err)
	}

	return data, nil
}

func sourceTableEntry(t *testing.T, pdf *model.Context, number int) *model.XRefTableEntry {
	t.Helper()

	entry, found := pdf.Table[number]
	if !found || entry == nil {
		t.Fatalf("missing source object %d", number)
	}

	return entry
}

func sourceDict(t *testing.T, object types.Object) types.Dict {
	t.Helper()

	dict, valid := object.(types.Dict)
	if !valid {
		t.Fatalf("source object is %T, want dictionary", object)
	}

	return dict
}

func sourceArray(t *testing.T, object types.Object) types.Array {
	t.Helper()

	array, valid := object.(types.Array)
	if !valid {
		t.Fatalf("source object is %T, want array", object)
	}

	return array
}

func sourceStream(t *testing.T, object types.Object) types.StreamDict {
	t.Helper()

	stream, valid := object.(types.StreamDict)
	if !valid {
		t.Fatalf("source object is %T, want stream", object)
	}

	return stream
}

func sourceAssemble(t *testing.T, engine *Engine, source SourceFile, destination string, pages int, target *PageSize) {
	t.Helper()

	request := AssembleRequest{
		FitTarget:     target,
		Sources:       []SourceFile{source},
		Order:         []Run{SourcePages(0, 1, pages)},
		ExpectedPages: pages,
		Destination:   destination,
	}
	if err := engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}
}

func sourceContext(t *testing.T, engine *Engine, path string) *model.Context {
	t.Helper()

	pdf, err := engine.readContext(t.Context(), NoSource, path)
	if err != nil {
		t.Fatal(err)
	}

	return pdf
}

func sourceAssertUnchanged(t *testing.T, path string, data []byte) {
	t.Helper()

	actual, err := sourceRead(t, path)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("source changed: %v", err)
	}
}

func sourceHasContent(t *testing.T, pdf *model.Context, content []byte) bool {
	t.Helper()

	found := false

	for _, entry := range pdf.Table {
		if entry.Free {
			continue
		}

		stream, valid := entry.Object.(types.StreamDict)
		if !valid {
			continue
		}

		if err := stream.DecodeWithContextAndLimit(t.Context(), 1<<20); err != nil {
			t.Fatal(err)
		}

		found = found || bytes.Contains(stream.Content, content)
	}

	return found
}

func sourceAssertNullAnnotations(t *testing.T, engine *Engine, path string) {
	t.Helper()
	pdf := sourceContext(t, engine, path)

	root, err := pdf.PagesContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, page types.Dict, _ inheritedAttrs) error {
		annotations, decodeErr := pdf.DereferenceArrayContext(t.Context(), page["Annots"])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}

		if len(annotations) != 0 {
			t.Fatalf("null annotations changed: %v", annotations)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func sourceNewEngine(t *testing.T) *Engine {
	t.Helper()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	return engine
}

func sourceInspect(t *testing.T, engine *Engine, path string, target *PageSize) SourceInfo {
	t.Helper()

	info, err := engine.Inspect(t.Context(), path, target)
	if err != nil {
		t.Fatal(err)
	}

	return info
}

func sourceNullRoundTrip(t *testing.T, engine *Engine, path string, data []byte, target *PageSize) {
	t.Helper()
	info := sourceInspect(t, engine, path, target)
	destination := filepath.Join(filepath.Dir(path), sourceTargetOutput)
	sourceWrite(t, destination, []byte("retain destination until valid output is ready"))
	sourceAssemble(t, engine, SourceFile{Path: path, Info: info}, destination, 2, target)

	output, err := engine.Inspect(t.Context(), destination, target)
	if err != nil || output.Pages != 2 {
		t.Fatalf("output: %+v %v", output, err)
	}

	sourceAssertUnchanged(t, path, data)
	sourceAssertNullAnnotations(t, engine, destination)
}

func sourceGenerationRoundTrip(
	t *testing.T,
	engine *Engine,
	path string,
	target *PageSize,
	content string,
	generation, referenceGeneration int,
) {
	t.Helper()
	info := sourceInspect(t, engine, path, target)
	destination := filepath.Join(filepath.Dir(path), sourceTargetOutput)
	sourceAssemble(t, engine, SourceFile{Path: path, Info: info}, destination, 1, target)
	pdf := sourceContext(t, engine, destination)

	found := sourceHasContent(t, pdf, []byte(content))

	matching := generation == referenceGeneration
	if found != matching {
		t.Fatalf("generation content: found=%t matching=%t", found, matching)
	}
}

func sourceDecodedContent(t *testing.T, object types.Object) []byte {
	t.Helper()

	stream := sourceStream(t, object)
	if err := stream.DecodeWithContextAndLimit(t.Context(), 1<<20); err != nil {
		t.Fatal(err)
	}

	return stream.Content
}

func sourcePredictorRoundTrip(t *testing.T, engine *Engine, path string, data []byte, target *PageSize) {
	t.Helper()
	info := sourceInspect(t, engine, path, target)
	destination := filepath.Join(filepath.Dir(path), sourceTargetOutput)
	sourceAssemble(t, engine, SourceFile{Path: path, Info: info}, destination, 1, target)

	output := sourceContext(t, engine, destination)
	if !sourceHasContent(t, output, []byte("PREDICTOR PRESERVED")) {
		t.Fatal("assembled output lost predictor text")
	}

	sourceAssertUnchanged(t, path, data)
}

func (test sourceCatalogVersionCase) verifyMode(t *testing.T, engine *Engine, path string, target *PageSize) {
	t.Helper()

	info, err := engine.Inspect(t.Context(), path, target)
	if test.refuse {
		if err == nil {
			t.Fatal("invalid nonnull catalog Version accepted")
		}

		return
	}

	if err != nil {
		t.Fatal(err)
	}

	expected := Version17
	if test.override {
		expected = Version20
	}

	if info.Version != expected {
		t.Fatalf("effective version: %+v, want %+v", info.Version, expected)
	}

	destination := filepath.Join(filepath.Dir(path), sourceTargetOutput)
	sourceAssemble(t, engine, SourceFile{Path: path, Info: info}, destination, 1, target)

	if _, inspectErr := engine.Inspect(t.Context(), destination, target); inspectErr != nil {
		t.Fatal(inspectErr)
	}
}

func sourceIncrementalPDF(t *testing.T, content string) []byte {
	t.Helper()

	bodies := []string{
		catalog,
		guardSinglePageTree,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 4 1 R >>",
		string(fitFixtureStream("", []byte(emptyAppearanceDrawing))),
	}
	data := sourceIdentityPDF(fitCatalogSourceVersion, "", bodies, nil, nil)
	tail := data[bytes.LastIndex(data, []byte("startxref\n"))+len("startxref\n"):]

	previous, err := strconv.Atoi(strings.SplitN(string(tail), "\n", 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	// Delete object 4, then reuse it at generation 1 in a third xref revision.
	freedXRef := len(data)
	data = fmt.Appendf(
		data,
		"xref\n0 1\n0000000004 65535 f \n4 1\n0000000000 00001 f \ntrailer\n<< /Size 5 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		previous,
		freedXRef,
	)
	streamOffset := len(data)
	data = append(data, []byte("4 1 obj\n")...)
	data = append(data, fitFixtureStream("", []byte(content))...)
	data = append(data, []byte("\nendobj\n")...)
	currentXRef := len(data)
	data = fmt.Appendf(
		data,
		"xref\n0 1\n0000000000 65535 f \n4 1\n%010d 00001 n \ntrailer\n<< /Size 5 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		streamOffset,
		freedXRef,
		currentXRef,
	)

	return data
}

func sourceCurrentGeneration(t *testing.T, engine *Engine, path string, target *PageSize, content string) {
	t.Helper()

	importer := pool{engine: engine}

	pdf, err := importer.readImport(t.Context(), 0, path, target)
	if err != nil {
		t.Fatal(err)
	}

	entry, found := pdf.FindTableEntry(4, 1)
	if !found || entry.Free {
		t.Fatal("matching incremental generation lost")
	}

	if _, found = pdf.FindTableEntry(4, 0); found {
		t.Fatal("obsolete generation became current")
	}

	stream := sourceStream(t, entry.Object)
	if err = stream.DecodeWithContextAndLimit(t.Context(), 1<<20); err != nil {
		t.Fatal(err)
	}

	if string(stream.Content) != content {
		t.Fatalf("current content changed: %q", stream.Content)
	}
}

func sourcePredictorBytes(t *testing.T, encoded []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer

	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return compressed.Bytes()
}

func sourceDifferentialPairs(program []byte) []byte {
	encoded := bytes.Clone(program)
	for index := 1; index < len(encoded); index += 2 {
		encoded[index] = program[index] - program[index-1]
	}

	return encoded
}

func sourceRequireLiveArray(t *testing.T, values types.Array) {
	t.Helper()

	if len(values) != 3 || values[0] != nil || values[1] != *types.NewIndirectRef(4, 7) || values[2] != nil {
		t.Fatalf("identity/cardinality: %#v", values)
	}
}
