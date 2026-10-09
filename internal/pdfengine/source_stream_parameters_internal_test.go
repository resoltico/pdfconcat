// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type (
	sourceStreamMetadataCase struct {
		name       string
		entries    string
		body       string
		target     string
		want       string
		generation int
		refuse     bool
	}

	sourcePredictorCase struct {
		name, entry, target string
		referenceGeneration int
		predicted, refuse   bool
	}
)

func TestSourceStreamMetadataResolvesExactGenerationsBeforeDecoding(t *testing.T) {
	t.Parallel()

	for _, test := range sourceStreamMetadataCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			bodies := []string{
				catalog,
				guardSinglePageTree,
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 4 0 R >>",
				string(fitFixtureStream(test.entries, []byte(test.body))),
				test.target,
			}
			data := sourceIdentityPDF(fitCatalogSourceVersion, "", bodies, map[int]int{5: test.generation}, nil)

			pdf, err := api.ReadContext(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration())
			if test.refuse {
				if err == nil {
					t.Fatal("null required filter element admitted")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			decoded := sourceDecodedContent(t, sourceTableEntry(t, pdf, 4).Object)
			if string(decoded) != test.want {
				t.Fatalf("decoded %q, want %q", decoded, test.want)
			}
		})
	}
}

func TestSourceDecodeParameterArraysPreservePredictorPrograms(t *testing.T) {
	t.Parallel()

	program := []byte("BT /F1 12 Tf 10 50 Td (PREDICTOR PRESERVED) Tj ET\n")
	if len(program)%2 != 0 {
		program = append(program, ' ')
	}

	parameters := "[<< /Predictor 2 /Colors 1 /BitsPerComponent 8 /Columns 2 >>]"
	for _, test := range sourcePredictorCases(parameters) {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			test.verifyPredictor(t, program)
		})
	}
}

func sourcePredictorCases(parameters string) []sourcePredictorCase {
	return []sourcePredictorCase{
		{name: "direct array", entry: parameters, target: parameters, predicted: true},
		{name: "indirect generation zero", entry: sourceSixReference, target: parameters, referenceGeneration: 0, predicted: true},
		{name: "indirect nonzero generation", entry: "6 7 R", target: parameters, referenceGeneration: 7, predicted: true},
		{name: "wrong generation is null", entry: "6 8 R", target: parameters, referenceGeneration: 7},
		{name: "missing array is null", entry: sourceMissingReference, target: parameters},
		{name: "explicit null", entry: sourceSixReference, target: nullPDFObject},
		{name: "invalid nonarray parameters", entry: sourceSixReference, target: "<< /Predictor 2 >>", refuse: true},
		{name: "invalid array length", entry: sourceSixReference, target: "[]", refuse: true},
	}
}

func sourceStreamMetadataCases() []sourceStreamMetadataCase {
	return []sourceStreamMetadataCase{
		{
			name: "matching filter", entries: "/Filter 5 7 R",
			body: sourceHexDrawing, target: sourceHexFilter,
			generation: 7, want: emptyAppearanceDrawing,
		},
		{
			name: "mismatching filter", entries: "/Filter 5 8 R",
			body: emptyAppearanceDrawing, target: sourceHexFilter,
			generation: 7, want: emptyAppearanceDrawing,
		},
		{
			name: "mismatching filter element", entries: "/Filter [5 8 R]",
			body: emptyAppearanceDrawing, target: sourceHexFilter,
			generation: 7, refuse: true,
		},
		{
			name: "null decode parameters", entries: "/Filter /ASCIIHexDecode /DecodeParms 5 8 R",
			body: sourceHexDrawing, target: "99",
			generation: 7, want: emptyAppearanceDrawing,
		},
		{
			name: "explicit-null decode parameters", entries: "/Filter /ASCIIHexDecode /DecodeParms 5 7 R",
			body: sourceHexDrawing, target: nullPDFObject,
			generation: 7, want: emptyAppearanceDrawing,
		},
		{
			name: "explicit-null filter", entries: "/Filter 5 7 R",
			body: emptyAppearanceDrawing, target: nullPDFObject,
			generation: 7, want: emptyAppearanceDrawing,
		},
		{
			name: "null array decode parameters", entries: "/Filter [/ASCIIHexDecode] /DecodeParms [5 8 R]",
			body: sourceHexDrawing, target: "99",
			generation: 7, want: emptyAppearanceDrawing,
		},
		{
			name: "null single-filter array decode parameters", entries: "/Filter /ASCIIHexDecode /DecodeParms [5 8 R]",
			body: sourceHexDrawing, target: "99",
			generation: 7, want: emptyAppearanceDrawing,
		},
	}
}

func (test sourcePredictorCase) verifyPredictor(t *testing.T, program []byte) {
	t.Helper()

	encoded := program
	if test.predicted {
		encoded = sourceDifferentialPairs(program)
	}

	compressed := sourcePredictorBytes(t, encoded)
	bodies := []string{
		catalog,
		guardSinglePageTree,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		string(fitFixtureStream("/Filter [/FlateDecode] /DecodeParms "+test.entry, compressed)),
		test.target,
	}
	data := sourceIdentityPDF(fitCatalogSourceVersion, "", bodies, map[int]int{6: test.referenceGeneration}, nil)

	pdf, err := api.ReadContext(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if test.refuse {
		if err == nil {
			t.Fatal("invalid parameter array accepted")
		}

		return
	}

	if err != nil {
		t.Fatal(err)
	}

	stream := sourceStream(t, sourceTableEntry(t, pdf, 5).Object)
	if err = stream.DecodeWithContextAndLimit(t.Context(), 1<<20); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(stream.Content, program) {
		t.Fatalf("predictor program changed: %q", stream.Content)
	}

	engine := sourceNewEngine(t)

	dir := t.TempDir()

	path := filepath.Join(dir, "predictor.pdf")
	sourceWrite(t, path, data)

	for _, target := range []*PageSize{nil, {595, 842}, {612, 1008}} {
		sourcePredictorRoundTrip(t, engine, path, data, target)
	}
}
