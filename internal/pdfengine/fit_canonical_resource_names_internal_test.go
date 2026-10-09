// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"path/filepath"
	"testing"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func canonicalResourceDocument() *pdffixture.Doc {
	resources := "<< /XObject << /X#2342 5 0 R >> /ExtGState << /G#2342 6 0 R >> /Font << /F#2342 7 0 R >> " +
		"/Pattern << /P#2342 9 0 R >> /ColorSpace << /PCS#2342 [/Pattern /DeviceRGB] " +
		"/C#2342 [/CalGray << /WhitePoint [1 1 1] >>] >> /Shading << /S#2342 11 0 R >> >>"
	program := "/S#2342 sh /X#2342 Do /G#2342 gs BT 1 0 0 1 20 20 Tm (A) Tj ET " +
		"BT /F#2342 8 Tf 1 0 0 1 40 20 Tm (A) Tj ET " +
		"/PCS#2342 cs 0 1 0 /P#2342 scn 50 50 20 20 re f " +
		"q 10 0 0 10 70 70 cm BI /W 1 /H 1 /BPC 8 /CS /C#2342 ID " + string([]byte{0}) + " EI Q"

	return rawDoc(
		catalog,
		guardSinglePageTree,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources "+resources+" /Contents 4 0 R >>",
		groupPresenceStream("", program),
		groupPresenceStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources <<>>", "0 0 1 rg 5 5 10 10 re f"),
		"<< /Type /ExtGState /Font [7 0 R 12] >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [.001 0 0 .001 0 0] /Resources <<>> "+
			"/FirstChar 65 /LastChar 65 /Widths [500] /Encoding << /Differences [65 /A#2342] >> "+
			"/CharProcs << /A#2342 8 0 R /AB 10 0 R >> >>",
		groupPresenceStream("", "500 0 d0 0 0 400 700 re f"),
		groupPresenceStream(
			"/Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 5 5] /XStep 5 /YStep 5 /Resources <<>>",
			"0 0 5 5 re f",
		),
		groupPresenceStream("", "500 0 0 0 400 700 d1 1 0 0 rg 0 0 400 700 re f"),
		"<< /ShadingType 2 /ColorSpace [/CalGray << /WhitePoint [1 1 1] >>] /Coords [0 0 100 0] /Extend [true true] "+
			"/Function << /FunctionType 2 /Domain [0 1] /C0 [.9] /C1 [.9] /N 1 >> >>",
	)
}

func TestFitCanonicalLiteralHashResourceNamesPreserveEveryActivePathAndPixels(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)
	for _, target := range []PageSize{{210 * 72 / 25.4, 297 * 72 / 25.4}, {612, 1008}} {
		t.Run(targetName(target), func(t *testing.T) { t.Parallel(); checkCanonicalResourceRendering(t, tools, target) })
	}
}

func checkCanonicalResourceRendering(t *testing.T, tools pdforacle.Tools, target PageSize) {
	t.Helper()

	dir := t.TempDir()

	sourcePath := filepath.Join(dir, fitRenderSource)
	if err := canonicalResourceDocument().WriteFile(sourcePath); err != nil {
		t.Fatal(err)
	}

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	info, err := engine.Inspect(t.Context(), sourcePath, &target)
	if err != nil {
		t.Fatalf("canonical source resource names falsely refused: %v", err)
	}

	output := filepath.Join(dir, fitRenderOutput)

	request := AssembleRequest{
		FitTarget:     &target,
		Sources:       []SourceFile{{Path: sourcePath, Info: info}},
		Order:         []Run{SourcePages(0, 1, 1)},
		ExpectedPages: 1,
		Destination:   output,
	}
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	renderers := []string{fitRendererPoppler}
	if pdforacle.HasMuPDF() {
		renderers = append(renderers, fitRendererMuPDF)
	}

	for _, renderer := range renderers {
		t.Logf("independent literal-hash resource renderer: %s", renderer)
		original := guardRender(t, tools, sourcePath, renderer)
		fitted := guardRender(t, tools, output, renderer)

		checked, wrong := guardPatternAgreement(original, fitted, target)
		if checked < 1000 || wrong*100 > checked {
			t.Fatalf("canonical resource identity changed pixels: compared=%d mismatched=%d", checked, wrong)
		}
	}
}

func TestFitCanonicalNativeNameValuesDoNotAcquireReservedMeaning(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		group  string
		reject bool
	}{
		{name: "genuine-escaped-form", group: "/Subtype /Fo#72m"},
		{name: "literal-hash-form", group: "/Subtype /Fo#2372m", reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := readUnvalidated(t, rawDoc(
				catalog,
				guardSinglePageTree,
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /F 5 0 R >> >> /Contents 4 0 R >>",
				groupPresenceStream(
					"",
					"/F Do",
				),
				groupPresenceStream("/Type /XObject "+test.group+" /BBox [0 0 100 100] /Resources <<>>", ""),
			))

			_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
			if (err != nil) != test.reject {
				t.Fatalf("native subtype literal identity reject=%t: %v", test.reject, err)
			}
		})
	}

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	spaces := types.Dict{guardLiteralDeviceName: types.Array{types.Name(fitCalGray), types.Dict{}}}

	count, err := inspector.colorComponents(t.Context(), types.Dict{fitColorSpace: spaces}, types.Name(guardLiteralDeviceName), 0)
	if err != nil || count != 1 {
		t.Fatalf("canonical color name rebound to DeviceRGB: components=%d err=%v", count, err)
	}

	encoding := simpleencodings.Encoding{}
	if _, err = fitEncodingEntry(types.Name("A#42"), 65, &encoding); err != nil || encoding[65] != "A#42" {
		t.Fatalf("canonical glyph name reinterpreted as AB: %q %v", encoding[65], err)
	}
}
