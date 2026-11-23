// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const testBareGroup = "<< /Type /OCG /Name (Layer) >>"
const testRectangle = "1 0 0 rg 20 20 60 60 re f"

func layerTestPDF(catalog, resources, program string, extra ...string) *pdffixture.Doc {
	d := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		fmt.Appendf(nil, "<< /Type /Catalog /Pages 2 0 R %s >>", catalog),
		[]byte("<< /Type /Pages /Count 1 /Kids [3 0 R] >>"),
		fmt.Appendf(nil, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << %s >> /Contents 4 0 R >>", resources),
		layerTestStream("", []byte(program)),
	}}
	for _, object := range extra {
		d.Objs = append(d.Objs, []byte(object))
	}
	return d
}

func layerTestStream(entries string, program []byte) []byte {
	body := fmt.Appendf(nil, "<< %s /Length %d >>\nstream\n", entries, len(program))
	body = append(body, program...)
	return append(body, "\nendstream"...)
}

func TestEmptyOptionalContentNormalization(t *testing.T) {
	const direct = "/OCProperties << /OCGs [] /D << /ON [] /Order [] /RBGroups [] >> >>"
	const indirect = "/OCProperties 5 0 R"
	tests := []struct {
		name, catalog, program string
		extra                  []string
		accept                 bool
	}{
		{"direct", direct, testRectangle, nil, true},
		{"fully indirect", indirect, testRectangle, []string{"<< /OCGs 6 0 R /D 7 0 R >>", "[]", "<< /Order 8 0 R >>", "[]"}, true},
		{"empty D", "/OCProperties << /OCGs [] /D << >> >>", testRectangle, nil, true},
		{"empty catalog dictionary", "/OCProperties << >>", testRectangle, nil, false},
		{"missing groups", "/OCProperties << /D << >> >>", testRectangle, nil, false},
		{"null groups", "/OCProperties << /OCGs null /D << >> >>", testRectangle, nil, false},
		{"nonempty groups", "/OCProperties << /OCGs [true] /D << >> >>", testRectangle, nil, false},
		{"missing D", "/OCProperties << /OCGs [] >>", testRectangle, nil, false},
		{"null config array", "/OCProperties << /OCGs [] /D << /ON null >> >>", testRectangle, nil, false},
		{"nonempty config array", "/OCProperties << /OCGs [] /D << /ON [true] >> >>", testRectangle, nil, false},
		{"unknown config", "/OCProperties << /OCGs [] /D << /Private [] >> >>", testRectangle, nil, false},
		{"wrong generation", "/OCProperties 5 1 R", testRectangle, []string{"<< /OCGs [] /D << >> >>"}, false},
		{"external subtree reference", indirect + " /ViewerPreferences << /Private 6 0 R >>", testRectangle, []string{"<< /OCGs 6 0 R /D << >> >>", "[]"}, false},
		{"external reference through indirect value", indirect + " /Private 7 0 R", testRectangle, []string{"<< /OCGs 6 0 R /D << >> >>", "[]", "6 0 R"}, false},
		{"lexical only", direct, "/OC /Missing BDC " + testRectangle + " EMC", nil, false},
		{"unknown stream role", direct + " /Private 5 0 R", testRectangle, []string{string(layerTestStream("", []byte(testRectangle)))}, false},
		{"private action", direct + " /Private << /S /SetOCGState >>", testRectangle, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := layerTestPDF(tc.catalog, "", tc.program, tc.extra...)
			for _, policy := range []packet.OptionalContentPolicy{packet.OptionalContentRefuse, packet.OptionalContentFlattenVisibleContent} {
				s, _, findings, err := readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{OptionalContent: policy})
				if !tc.accept {
					if !optionalContentRefusal(err) || len(findings) != 0 {
						t.Fatalf("expected layer refusal, got %v, %v", err, findings)
					}
					continue
				}
				if err != nil || len(findings) != 1 || findings[0].Code != diag.SourceOptionalContentEmptyRemoved ||
					findings[0].PDFLocation.Path != "Catalog.OCProperties" {
					t.Fatalf("expected an original catalog finding, got %v, %v", err, findings)
				}
				cat, err := s.Catalog()
				if _, present := cat["OCProperties"]; err != nil || present {
					t.Fatal("catalog edge survived", err)
				}
				raw, err := s.Dereference(*types.NewIndirectRef(4, 0))
				stream, ok := raw.(types.StreamDict)
				if err != nil || !ok || string(stream.Raw) != tc.program {
					t.Fatalf("original content changed: %v", err)
				}
				for i, expected := range tc.extra {
					ref := *types.NewIndirectRef(i+5, 0)
					if !liveReference(s, ref) || expected == "[]" && s.Table[i+5].Object.PDFString() != "[]" {
						t.Fatal("catalog removal modified a detached object", ref)
					}
				}
			}
		})
	}
}

func TestOptionalContentWrapperExactMaskedStreams(t *testing.T) {
	const catalog = "/OCProperties 7 0 R"
	const resources = "/Properties << /Pr12 8 0 R /Keep << /ActualText (preserved) >> >>"
	const open = "/O#43 /Pr#312 % literal /OC and EMC\r\nBDC\n"
	const art = "/Span /Keep BDC\n/Span << /MCID 0 /ActualText (literal /OC) >> BDC\n" + testRectangle +
		"\nEMC % inner tag\nEMC\nBI /W 7 /H 1 /BPC 8 /CS /G ID /OC BDC EI\n"
	const closing = "EMC\n% trailing /OC"
	d := layerTestPDF(catalog, resources, open, "", "", "null", testBareGroup)
	d.Objs[2] = bytes.Replace(d.Objs[2], []byte("4 0 R"), []byte("9 0 R"), 1)
	d.Objs = append(d.Objs, []byte("[4 0 R 5 0 R 6 0 R]"))
	var encoded bytes.Buffer
	w := zlib.NewWriter(&encoded)
	if _, err := w.Write([]byte(open)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	d.Objs[3] = layerTestStream("/Filter /FlateDecode", encoded.Bytes())
	d.Objs[4] = layerTestStream("", []byte(art))
	d.Objs[5] = layerTestStream("", []byte(closing))
	s, origins, ds, err := readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{OptionalContent: packet.OptionalContentFlattenVisibleContent})
	if err != nil || !slices.Equal(origins.wrapperPages, []int{1}) || len(ds) != 1 || ds[0].Code != diag.SourceOptionalContentWrappersRemoved {
		t.Fatalf("admission: %v, %v", err, ds)
	}
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := s.DereferenceArray(pages[0].dict["Contents"])
	if err != nil || len(streams) != 3 || streams[1] != *types.NewIndirectRef(5, 0) {
		t.Fatalf("page content array: %v, %v", streams, err)
	}
	expected := []string{"              % literal /OC and EMC\r\n   \n", art, "   \n% trailing /OC"}
	for i, entry := range streams {
		object, err := s.Dereference(entry)
		if err != nil {
			t.Fatal(err)
		}
		stream, ok := object.(types.StreamDict)
		if !ok {
			t.Fatalf("expected stream %d, got %T", i, object)
		}
		decoded, err := decodeDrawingStream(t.Context(), s, stream)
		if err != nil || string(decoded) != expected[i] {
			t.Fatalf("stream %d got %q, want %q (%v)", i, decoded, expected[i], err)
		}
	}
	_, properties, err := layerPagePropertiesFor(s, pages[0], sourceSite{})
	kept, ok := properties["Keep"].(types.Dict)
	if err != nil || !ok || len(properties) != 1 || kept["ActualText"] != types.StringLiteral("preserved") {
		t.Fatalf("ordinary properties: %v, %v", properties, err)
	}
	original, err := s.Dereference(*types.NewIndirectRef(4, 0))
	originalStream, ok := original.(types.StreamDict)
	if err != nil || !ok || !bytes.Equal(originalStream.Raw, encoded.Bytes()) {
		t.Fatal("normalization modified the original encoded stream", err)
	}

	// The content witness must still detect a byte change outside the edit.
	before, err := capturePageContent(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	s.Table[5].Object = types.StreamDict{Raw: []byte(art + "\n")}
	if err := before.verify(t.Context(), s); err == nil || diagnostic(err, diag.SourceProcessingFailed).Code != diag.SourceDamaged {
		t.Fatalf("normalized content witness accepted a changed unrelated stream: %v", err)
	}
}

func TestOptionalContentWrapperSerializedLexicalGuard(t *testing.T) {
	const residual = "/OC /Missing BDC " + testRectangle + " EMC"
	for _, nested := range []bool{false, true} {
		d := layerTestPDF("", "", residual)
		if nested {
			d = layerTestPDF("", "/XObject << /Fit 5 0 R >>", "q /Fit Do Q", string(layerTestStream(
				"/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << >>", []byte(residual))))
		}
		conf, err := configuration()
		if err != nil {
			t.Fatal(err)
		}
		s, err := readPDFContext(t.Context(), bytes.NewReader(d.Bytes()), conf)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := rawPages(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		if err := scanLayerGraph(t.Context(), s, pages, &optionalContentPlan{}, layerGraphOnly, nil); err != nil {
			t.Fatalf("negative control has graph evidence: %v", err)
		}
		err = verify(t.Context(), bytes.NewReader(d.Bytes()), 1, nil, nil, []int64{1}, nil)
		finding := Diagnostic(err)
		if err == nil || finding.Code != diag.OutputInvalid || finding.PDFLocation == nil {
			t.Fatalf("stage accepted leftover operators (nested=%t): %v", nested, err)
		}
	}
}

func TestOptionalContentWrapperRefusalControls(t *testing.T) {
	const wrapper = "/OC /Pr12 BDC\n" + testRectangle + "\nEMC"
	const props = "/Properties << /Pr12 " + testBareGroup + " >>"
	for _, tc := range []struct {
		name, resources, program string
		mutate                   func(*pdffixture.Doc)
		expected                 diag.Code
	}{
		{"early close", props, wrapper + " q Q", nil, diag.SourceOptionalContent},
		{"unbalanced", props, "/OC /Pr12 BDC BX /Span BMC EX " + testRectangle + " EMC", nil, diag.SourceOptionalContent},
		{"inside compatibility", props, "BX " + wrapper + " EX", nil, diag.SourceOptionalContent},
		{"ordinary BDC also uses the property", props, "/OC /Pr12 BDC /Span /Pr12 BDC " + testRectangle + " EMC % inner tag\nEMC", nil, diag.SourceOptionalContent},
		{"ordinary DP also uses the property", props, "/OC /Pr12 BDC /Span /Pr12 DP " + testRectangle + " EMC", nil, diag.SourceOptionalContent},
		{"missing binding", "", wrapper, nil, diag.SourceOptionalContent},
		{"MCID on the group", "/Properties << /Pr12 << /Type /OCG /Name (Layer) /MCID 0 >> >>", wrapper, nil, diag.SourceOptionalContent},
		{"explicit null extra key", "/Properties << /Pr12 << /Type /OCG /Name (Layer) /Usage null >> >>", wrapper, nil, diag.SourceOptionalContent},
		{"membership", "/Properties << /Pr12 << /Type /OCMD /OCGs [] >> >>", wrapper, nil, diag.SourceOptionalContent},
		{"wrong generation", "/Properties << /Pr12 5 1 R >>", wrapper, func(d *pdffixture.Doc) {
			d.Objs = append(d.Objs, []byte(testBareGroup))
		}, diag.SourceOptionalContent},
		{"pre-layer PDF", props, wrapper, func(d *pdffixture.Doc) { d.Version = "1.4" }, diag.SourceOptionalContent},
		{"unknown sharing", "/Properties 5 0 R", wrapper, func(d *pdffixture.Doc) {
			d.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /Private 5 0 R >>")
			d.Objs = append(d.Objs, []byte("<< /Pr12 "+testBareGroup+" >>"))
		}, diag.SourceOptionalContent},
		{"split lexical token", props, "/O", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("4 0 R"), []byte("[4 0 R 5 0 R]"), 1)
			d.Objs = append(d.Objs, layerTestStream("", []byte("C /Pr12 BDC\n"+testRectangle+"\nEMC")))
		}, diag.SourceOptionalContent},
		{"ambiguous inline image", props, "/OC /Pr12 BDC\nBI /W 1 /H 1 /BPC 8 /CS /G /F /Fl ID A EI BI /W 1 /H 1 /BPC 8 /CS /G /F /Fl ID B EI\nEMC", nil, diag.SourceOptionalContent},
		{"conflicting resource ancestry", props, wrapper, func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Parent 2 0 R"), []byte("/Parent 5 0 R"), 1)
			d.Objs = append(d.Objs, []byte("<< /Type /Pages /Count 0 /Kids [] >>"))
		}, diag.SourceOptionalContent},
		{"signature priority", props, wrapper, func(d *pdffixture.Doc) {
			d.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /Private << /Type /Sig >> >>")
		}, diag.SourceSigned},
		{"broken stream reference priority", props, wrapper, func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("4 0 R"), []byte("4 1 R"), 1)
		}, diag.SourceDamaged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := layerTestPDF("", tc.resources, tc.program)
			if tc.mutate != nil {
				tc.mutate(d)
			}
			_, _, ds, err := readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{OptionalContent: packet.OptionalContentFlattenVisibleContent})
			if err == nil || Diagnostic(err).Code != tc.expected || len(ds) != 0 {
				t.Fatalf("wanted %s without a success warning, got %v, %v", tc.expected, err, ds)
			}
		})
	}
}

func TestOptionalContentWrapperBorrowedResourcesAndSharedStreams(t *testing.T) {
	const inherited = "<< /Type /Pages /Count 2 /Kids [3 0 R 5 0 R] /Resources 6 0 R >>"
	const second = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents "
	d := layerTestPDF("", "", "/OC /Pr12 BDC\n"+testRectangle+"\nEMC",
		second+"4 0 R >>", "<< /Properties << /Pr12 "+testBareGroup+" >> >>")
	d.Objs[1] = []byte(inherited)
	d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Resources <<  >>"), nil, 1)
	// Both pages inherit the same property binding and share the stream.
	s, origins, _, err := readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{OptionalContent: packet.OptionalContentFlattenVisibleContent})
	if err != nil || !slices.Equal(origins.wrapperPages, []int{1, 2}) {
		t.Fatalf("shared wrappers: %v, %v", origins.wrapperPages, err)
	}
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		if page.dict["Contents"] == *types.NewIndirectRef(4, 0) {
			t.Fatal("shared source stream was reused instead of replaced locally")
		}
	}

	// The second page shadows the entire inherited resource dictionary.
	d.Objs[4] = []byte(second + "4 0 R /Resources << >> >>")
	_, _, _, err = readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{OptionalContent: packet.OptionalContentFlattenVisibleContent})
	if !optionalContentRefusal(err) {
		t.Fatalf("a page bypassed its local Resources entry: %v", err)
	}

	// The second page uses the inherited name for ordinary marked content.
	d.Objs[4] = []byte(second + "7 0 R >>")
	d.Objs = append(d.Objs, layerTestStream("", []byte("/Span /Pr12 DP "+testRectangle)))
	_, _, _, err = readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{OptionalContent: packet.OptionalContentFlattenVisibleContent})
	if !optionalContentRefusal(err) {
		t.Fatalf("an ordinary inherited binding could be removed: %v", err)
	}
}

func TestOptionalContentChecksOmittedPageAndDrawingPrograms(t *testing.T) {
	const catalog = "/OCProperties << /OCGs [] /D << >> >>"
	const residual = "/OC << /Type /OCG /Name (Unproved) >> BDC " + testRectangle + " EMC"
	for _, tc := range []struct {
		name, resource, page, stream string
	}{
		{"omitted page", "", residual, ""},
		{"form", "/XObject << /X 6 0 R >>", "/X Do", "/Type /XObject /Subtype /Form /BBox [0 0 100 100]"},
		{"pattern", "/Pattern << /P 6 0 R >>", "/Pattern cs /P scn", "/Type /Pattern /PatternType 1"},
		{"Type 3 glyph", "/Font << /F << /Subtype /Type3 /CharProcs << /a 6 0 R >> >> >>", testRectangle, ""},
		{"appearance", "", testRectangle, "/Type /XObject /Subtype /Form /BBox [0 0 100 100]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, declaration := range []string{"", catalog} {
				d := layerTestPDF(declaration, "", testRectangle)
				d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 5 0 R] >>")
				page := fmt.Appendf(nil, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << %s >> /Contents 7 0 R", tc.resource)
				if tc.name == "appearance" {
					page = append(page, " /Annots [8 0 R]"...)
				}
				d.Objs = append(d.Objs, append(page, " >>"...),
					layerTestStream(tc.stream, []byte(residual)), layerTestStream("", []byte(tc.page)))
				if tc.name == "appearance" {
					d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Square /Rect [0 0 20 20] /AP << /N 6 0 R >> >>"))
				}
				raw := d.Bytes()
				_, _, findings := InspectReader(t.Context(), bytes.NewReader(raw), [32]byte{}, int64(len(raw)), SourcePolicies{}, []SelectionRequest{
					{Item: "first only", Pages: []packet.PageRange{{First: 1, Last: 1}}},
				})
				if len(findings) != 1 || findings[0].Code != diag.SourceOptionalContent {
					t.Fatalf("drawing evidence was ignored: %v", findings)
				}
			}
		})
	}
}

func TestOptionalContentSerializedBudgetIsPerProgram(t *testing.T) {
	const limit = 64
	program := []byte(testRectangle + " q Q q Q q Q q Q q Q q Q q Q")
	d := pdffixture.EncodedPages("scope",
		[]pdffixture.EncodedStream{{Data: program}},
		[]pdffixture.EncodedStream{{Data: program}},
		[]pdffixture.EncodedStream{{Data: program}},
		[]pdffixture.EncodedStream{{Data: program}},
	)
	conf, err := configuration()
	if err != nil {
		t.Fatal(err)
	}
	conf.Limits.MaxDecodeBytes = limit
	s, err := readPDFContext(t.Context(), bytes.NewReader(d.Bytes()), conf)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	// The final occurrence shares the entire serialized stream sequence.
	pages[3].dict["Contents"] = pages[0].dict["Contents"]
	content, err := observePageContent(t.Context(), s, pages)
	if err != nil {
		t.Fatal(err)
	}
	evidence := newLayerProgramEvidence(s)
	programAllowance := evidence.program(s, layerVerify).remaining
	if len(program) >= limit || 3*int64(len(program)) <= programAllowance {
		t.Fatal("fixture must exceed the single-program allowance in aggregate")
	}
	if err := verifySerializedWrapperPages(t.Context(), s, pages, content, []int64{1, 2, 3, 4}, evidence); err != nil {
		t.Fatalf("a packet of individually bounded programs failed: %v", err)
	}
	if len(evidence.pages) != 3 {
		t.Fatalf("distinct serialized programs proved = %d, want 3", len(evidence.pages))
	}
}

func TestOptionalContentDetectionDistinguishesOperatorsFromPayload(t *testing.T) {
	const inline = "/O#43 << /Type /OCG /Name (Layer) /Usage << /View << /ViewState /OFF >> >> >> BDC "
	for _, tc := range []struct {
		name, program string
		refused       bool
	}{
		{"direct group", inline + testRectangle + " EMC", true},
		{"missing name", "/OC /Missing BDC " + testRectangle + " EMC", true},
		{"group reference point", "/OC << /Type /OCG /Name (Layer) >> DP " + testRectangle, true},
		{"missing reference point", "/OC /Missing DP " + testRectangle, true},
		{"ordinary reference point", "/OC << /ActualText (ordinary point) >> DP " + testRectangle, false},
		{"inline membership property", "/OC << /Type /OCMD /OCGs [] >> BDC " + testRectangle, true},
		{"control strings comments and image", "% /OC /Missing BDC\n/Span << /ActualText (/OC BDC) >> BDC " +
			testRectangle + " EMC\nBI /W 7 /H 1 /BPC 8 /CS /G ID /OC BDC EI", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := layerTestPDF("", "", tc.program).Bytes()
			for _, policy := range []packet.OptionalContentPolicy{packet.OptionalContentRefuse, packet.OptionalContentFlattenVisibleContent} {
				_, _, _, err := readSourceReader(t.Context(), bytes.NewReader(input), SourcePolicies{OptionalContent: policy})
				if tc.refused && !optionalContentRefusal(err) || !tc.refused && err != nil {
					t.Fatalf("admission with %s: %v", policy, err)
				}
			}
			if err := Verify(t.Context(), input, 1); tc.refused && (err == nil || Diagnostic(err).Code != diag.OutputInvalid) || !tc.refused && err != nil {
				t.Fatalf("serialized verification: %v", err)
			}
		})
	}
}

func TestOptionalContentSharedProgramChecksEachResourceScope(t *testing.T) {
	program := []byte("/OC /Normal BDC " + testRectangle + " EMC")
	d := pdffixture.EncodedPages("scopes",
		[]pdffixture.EncodedStream{{Data: program}},
		[]pdffixture.EncodedStream{{Data: program}},
	)
	d.Objs[3] = bytes.Replace(d.Objs[3], []byte("/Font"), []byte("/Properties << /Normal << /ActualText (ordinary) >> >> /Font"), 1)
	conf, err := configuration()
	if err != nil {
		t.Fatal(err)
	}
	s, err := readPDFContext(t.Context(), bytes.NewReader(d.Bytes()), conf)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	pages[1].dict["Contents"] = pages[0].dict["Contents"]
	content, err := observePageContent(t.Context(), s, pages)
	if err != nil {
		t.Fatal(err)
	}
	evidence := newLayerProgramEvidence(s)
	err = verifySerializedWrapperPages(t.Context(), s, pages, content, []int64{1, 2}, evidence)
	if !optionalContentRefusal(err) || Diagnostic(err).PDFLocation.Page != 2 || len(evidence.pages) != 1 {
		t.Fatalf("reused lexical proof hid an unresolved second-page property: %v", err)
	}
}

func TestOptionalContentWrapperAdmitsOnlyProvedPrelude(t *testing.T) {
	const binding = "/Properties << /Pr12 " + testBareGroup + " >>"
	const intent = "/ExtGState << /GS << /Type /ExtGState /RI /RelativeColorimetric >> >>"
	const prelude = "1.0000 0 0 1 0 0 cm\n/GS gs\n"
	const artwork = testRectangle + "\n"
	const wrapper = "/OC /Pr12 BDC\n" + artwork + "EMC"
	for _, tc := range []struct {
		name, resources, drawing, detail string
	}{
		{"identity and intent", binding + " " + intent, prelude + wrapper, ""},
		{"unchanged mark-first", binding, wrapper, ""},
		{"unproved near identity", binding, "1.000000000000000000001 0 0 1 0 0 cm\n" + wrapper, "must be an exact identity"},
		{"transformed", binding, "1 0 0 1 1 0 cm\n" + wrapper, "must be an exact identity"},
		{"painting", binding, "20 20 60 60 re f\n" + wrapper, "prelude operator \"re\""},
		{"state-bearing gs", binding + " /ExtGState << /GS << /RI /Perceptual /ca 0.5 >> >>", prelude + wrapper, "contains an entry other than /RI or /Type"},
		{"explicit null", binding + " /ExtGState << /GS << /RI /Perceptual /ca null >> >>", prelude + wrapper, "explicit null entry"},
		{"invalid intent", binding + " /ExtGState << /GS << /RI /Unknown >> >>", prelude + wrapper, "requires a valid /RI rendering intent"},
		{"invalid type", binding + " /ExtGState << /GS << /Type /Group /RI /Perceptual >> >>", prelude + wrapper, "/Type must be /ExtGState"},
		{"missing state", binding, prelude + wrapper, "resource could not be resolved"},
		{"repeat", binding + " " + intent, prelude + "/GS gs\n" + wrapper, "can appear once"},
		{"matrix after intent", binding + " " + intent, "/GS gs\n1 0 0 1 0 0 cm\n" + wrapper, "with the matrix before"},
		{"suffix", binding + " " + intent, prelude + wrapper + "\nq Q", "decoded byte"},
		{"second mark", binding + " " + intent, prelude + wrapper + "\n" + wrapper, "decoded byte"},
		{"group used by ordinary mark", binding + " " + intent, prelude + "/OC /Pr12 BDC /Span /Pr12 DP " + artwork + "EMC", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := layerTestPDF("", tc.resources, tc.drawing).Bytes()
			accepted := tc.name == "identity and intent" || tc.name == "unchanged mark-first"
			_, _, _, err := readSourceReader(t.Context(), bytes.NewReader(data), SourcePolicies{OptionalContent: packet.OptionalContentRefuse})
			if !optionalContentRefusal(err) || strings.Contains(err.Error(), "flatten-visible-content") != accepted {
				t.Fatalf("default proof/hint: %v", err)
			}
			s, _, findings, err := readSourceReader(t.Context(), bytes.NewReader(data), SourcePolicies{OptionalContent: packet.OptionalContentFlattenVisibleContent})
			if !accepted {
				if !optionalContentRefusal(err) || !strings.Contains(err.Error(), tc.detail) || len(findings) != 0 {
					t.Fatalf("opt-in refusal: %v, %v", err, findings)
				}
				return
			}
			if err != nil || len(findings) != 1 || findings[0].Code != diag.SourceOptionalContentWrappersRemoved {
				t.Fatalf("opt-in admission: %v, %v", err, findings)
			}
			pages, err := rawPages(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			program, err := s.PageContent(pages[0].dict, 1)
			expected := strings.TrimSuffix(tc.drawing, wrapper) + "             \n" + artwork + "   "
			if err != nil || string(program) != expected {
				t.Fatalf("mask altered prelude or artwork: got %q, want %q (%v)", program, expected, err)
			}
		})
	}
}
