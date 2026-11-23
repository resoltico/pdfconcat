// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"io"
	"slices"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func inspectSignatures(t *testing.T, d *pdffixture.Doc, policy SourcePolicies) (SourceFacts, []*OccurrenceFacts, []diag.Diagnostic) {
	t.Helper()
	raw := d.Bytes()
	return InspectReader(t.Context(), bytes.NewReader(raw), sha256.Sum256(raw), int64(len(raw)), policy, []SelectionRequest{{Item: "source"}})
}

func TestDiscardSignatureWidgetProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*pdffixture.Doc)
		want diag.Code
	}{
		{"opaque appearance", nil, diag.SourceSignatureDiscarded},
		{"hidden", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 2"), 1) }, diag.SourceSignatureDiscarded},
		{"invisible widget still paints", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 5"), 1) }, diag.SourceSignatureDiscarded},
		{"screen only", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 0"), 1) }, diag.SourceSignatureDiscardUnsupported},
		{"print only", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 36"), 1) }, diag.SourceSignatureDiscardUnsupported},
		{"NoZoom", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 12"), 1) }, diag.SourceSignatureDiscardUnsupported},
		{"NoRotate", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 20"), 1) }, diag.SourceSignatureDiscardUnsupported},
		{"ToggleNoView", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte("/F 260"), 1) }, diag.SourceSignatureDiscardUnsupported},
		{"ambiguous normal", func(d *pdffixture.Doc) {
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/N 8 0 R"), []byte("/N << /One 8 0 R /Two 8 0 R >>"), 1)
		}, diag.SourceSignatureDiscardUnsupported},
		{"state selected", func(d *pdffixture.Doc) {
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/N 8 0 R"), []byte("/N << /One 8 0 R /Two 8 0 R >>"), 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/AP"), []byte("/AS /One /AP"), 1)
		}, diag.SourceSignatureDiscarded},
		{"no normal appearance", func(d *pdffixture.Doc) { d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/AP << /N 8 0 R >>"), nil, 1) }, diag.SourceSignatureDiscardUnsupported},
		{"transparency group", func(d *pdffixture.Doc) {
			d.Objs[7] = bytes.Replace(d.Objs[7], []byte("/Resources"), []byte("/Group << /S /Transparency >> /Resources"), 1)
		}, diag.SourceSignatureDiscardUnsupported},
		{"wrong page backlink", func(d *pdffixture.Doc) {
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/P 3 0 R"), []byte("/P 2 0 R"), 1)
		}, diag.SourceSignatureDiscardUnsupported},
		{"surviving value reference", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Contents"), []byte("/Retention 7 0 R /Contents"), 1)
		}, diag.SourceSignatureDiscardUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			if tc.edit != nil {
				tc.edit(d)
			}
			_, _, findings := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if !slices.ContainsFunc(findings, func(d diag.Diagnostic) bool { return d.Code == tc.want }) {
				t.Fatalf("want %s, findings %v", tc.want, findings)
			}
			for _, finding := range findings {
				if tc.want == diag.SourceSignatureDiscarded && finding.Severity == diag.Error {
					t.Fatal(findings)
				}
			}
		})
	}
}

func TestDiscardSignatureOwnershipAndMetadata(t *testing.T) {
	testDocument := func(catalog string) *pdffixture.Doc {
		doc := pdffixture.Plain("metadata")
		doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte(">>"), []byte(" "+catalog+" >>"), 1)
		return doc
	}
	for _, tc := range []struct {
		name      string
		make      func() *pdffixture.Doc
		want      diag.Code
		byDefault diag.Code
	}{
		{"permission-only", func() *pdffixture.Doc { return testDocument("/Perms << /UR3 << /Type /Sig >> >>") }, diag.SourceSignatureDiscarded, diag.SourceSigned},
		{"empty permissions", func() *pdffixture.Doc { return testDocument("/Perms << >>") }, diag.SourceSignatureDiscarded, diag.SourceSigned},
		{"unknown permissions", func() *pdffixture.Doc { return testDocument("/Perms << /Unknown true >>") }, diag.SourceSignatureDiscardUnsupported, diag.SourceSigned},
		{"validation-only", func() *pdffixture.Doc {
			doc := testDocument("/DSS << /VRI << /Synthetic << /Cert [6 0 R] >> >> /Certs [6 0 R] >>")
			doc.Objs = append(doc.Objs, []byte("<< /Length 4 >>\nstream\ndata\nendstream"))
			return doc
		}, diag.SourceSignatureDiscarded, diag.Code{}},
		{"flag-only", func() *pdffixture.Doc { return testDocument("/AcroForm << /Fields [] /SigFlags 3 >>") }, diag.SourceSignatureDiscarded, diag.Code{}},
		{"genuinely unsigned", func() *pdffixture.Doc { return pdffixture.EmptySignatureField("empty") }, diag.Code{}, diag.Code{}},
		{"null unsigned value", func() *pdffixture.Doc {
			doc := pdffixture.EmptySignatureField("empty")
			doc.Objs[5] = bytes.Replace(doc.Objs[5], []byte(">>"), []byte("/V 7 0 R >>"), 1)
			doc.Objs = append(doc.Objs, []byte("null"))
			return doc
		}, diag.Code{}, diag.Code{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := tc.make()
			_, _, defaults := inspectSignatures(t, fixture, SourcePolicies{})
			if tc.byDefault != (diag.Code{}) && !slices.ContainsFunc(defaults, func(d diag.Diagnostic) bool { return d.Code == tc.byDefault }) ||
				tc.byDefault == (diag.Code{}) && diag.Outcome(defaults) != "ok" {
				t.Fatalf("default findings: %v", defaults)
			}
			_, _, ds := inspectSignatures(t, fixture, SourcePolicies{Signatures: packet.SignaturesDiscard})
			hasDiscard := slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded })
			switch tc.want {
			case (diag.Code{}):
				if hasDiscard || diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
			case diag.SourceSignatureDiscarded:
				if !hasDiscard || diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
			default:
				if !slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Code == tc.want }) {
					t.Fatal(ds)
				}
			}
		})
	}
}

func TestDiscardInheritedSignatureWithSharedAppearance(t *testing.T) {
	d := pdffixture.SignatureArtwork()
	d.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /AcroForm 12 0 R >>")
	d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Annots [6 0 R]"), []byte("/Annots 13 0 R"), 1)
	d.Objs[5] = []byte("<< /Type /Annot /Subtype /Widget /Parent 10 0 R /Rect [75 85 175 145] /F 4 /P 3 0 R /AP << /N 8 0 R >> >>")
	d.Objs[6] = []byte("<< /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 2 3] /Contents <00> >>")
	d.Objs = append(d.Objs,
		[]byte("<< /FT /Sig /T (group) /Kids 14 0 R >>"),
		[]byte("<< /Parent 9 0 R /T (approval) /Kids [6 0 R] /V 7 0 R >>"),
		[]byte("<< /Type /Annot /Subtype /Widget /FT /Tx /T (ordinary) /Rect [190 180 210 220] /F 4 /Ff 1 /P 3 0 R /V (kept) /DV (default) /AP << /N 8 0 R >> >>"),
		[]byte("<< /Fields 15 0 R /CO [10 0 R 11 0 R] /DR << /Font << /F1 5 0 R >> >> /DA (/F1 12 Tf 0 g) /SigFlags 3 >>"),
		[]byte("[6 0 R 11 0 R]"),
		[]byte("[10 0 R]"),
		[]byte("[9 0 R 11 0 R]"),
	)
	_, _, defaults := inspectSignatures(t, d, SourcePolicies{})
	if !slices.ContainsFunc(defaults, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSigned }) {
		t.Fatalf("inherited untyped value was missed: %v", defaults)
	}
	s, origins, ds, err := readSourceReader(t.Context(), bytes.NewReader(d.Bytes()), SourcePolicies{Signatures: packet.SignaturesDiscard})
	if err != nil {
		t.Fatalf("admission: %v: %v", err, ds)
	}
	cat, err := s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil || form["SigFlags"] != nil || form["DR"] == nil || form["DA"] == nil {
		t.Fatalf("retained form: %v: %v", form, err)
	}
	nodes, err := formFieldTree(t.Context(), s, form)
	if err != nil || len(nodes) != 1 || nodes[0].name != "ordinary" {
		t.Fatalf("retained fields: %v: %v", nodes, err)
	}
	retained := nodes[0].dict
	if v, err := textValue(s, retained["V"]); err != nil || v != "kept" {
		t.Fatalf("field value: %q: %v", v, err)
	}
	if v, err := textValue(s, retained["DV"]); err != nil || v != "default" {
		t.Fatalf("field default: %q: %v", v, err)
	}
	ap, err := s.DereferenceDict(retained["AP"])
	if err != nil || ap["N"] == nil || retained.IntEntry("Ff") == nil || *retained.IntEntry("Ff") != 1 || len(origins.signatureArtwork) != 1 {
		t.Fatalf("ordinary field or signature artwork changed: %v; %v", retained, err)
	}
	order, err := s.DereferenceArray(form["CO"])
	if err != nil || len(order) != 1 || order[0] != nodes[0].ref {
		t.Fatalf("retained calculation order: %v: %v", order, err)
	}

	// Keep the common ancestor and its ordinary child on a second source.
	// An alias to the same branch cannot acquire the changed field hierarchy.
	d.Objs[10] = bytes.Replace(d.Objs[10], []byte("/FT /Tx"), []byte("/Parent 9 0 R /FT /Tx"), 1)
	d.Objs[13] = []byte("[10 0 R 11 0 R]")
	d.Objs[14] = []byte("[9 0 R]")
	_, _, ds = inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
	if diag.Outcome(ds) != "ok" {
		t.Fatalf("surviving shared hierarchy: %v", ds)
	}
	admissionFinding(t, ds, diag.SourceSignatureDiscarded)
	admissionEntry(d, 3, "/UnrelatedOwner 9 0 R")
	_, _, ds = inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
	admissionFinding(t, ds, diag.SourceSignatureDiscardUnsupported)
}

func TestSignatureDiscardKeepsIndependentAdmissionSafeguards(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func() *pdffixture.Doc
		want diag.Code
	}{
		{"original widget layer", func() *pdffixture.Doc {
			d := pdffixture.SignatureValue("hidden layer")
			return admissionEntry(d, 6, "/OC << /Type /OCG /Name (signature) >>")
		}, diag.SourceOptionalContent},
		{"dynamic form", func() *pdffixture.Doc {
			return admissionEntry(pdffixture.SignatureValue("dynamic"), 1, "/NeedsRendering true")
		}, diag.SourceDynamicForm},
		{"original page contents identity", func() *pdffixture.Doc {
			d := pdffixture.SignatureValue("bad stream")
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Contents 4 0 R"), []byte("/Contents 4 1 R"), 1)
			return d
		}, diag.SourceDamaged},
		{"annotation appearance identity", func() *pdffixture.Doc {
			d := pdffixture.SignatureValue("bad appearance")
			return admissionEntry(d, 6, "/AP << /N 4 1 R >>")
		}, diag.SourceSignatureDiscardUnsupported},
		{"named action targets signature", func() *pdffixture.Doc {
			return admissionEntry(pdffixture.SignatureValue("hidden"), 1, "/OpenAction << /S /Hide /T (approval) >>")
		}, diag.SourceSignatureDiscardUnsupported},
		{"competing widget", func() *pdffixture.Doc {
			d := pdffixture.SignatureArtwork()
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Annots [6 0 R]"), []byte("/Annots [6 0 R 9 0 R]"), 1)
			d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [100 100 195 195] /Border [0 0 0] /A << /S /URI /URI (https://example.invalid/) >> >>"))
			return d
		}, diag.SourceSignatureDiscardUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ds := inspectSignatures(t, tc.make(), SourcePolicies{Signatures: packet.SignaturesDiscard})
			if !slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Code == tc.want }) ||
				slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded }) {
				t.Fatalf("want %s without discard warning: %v", tc.want, ds)
			}
		})
	}
}

func TestSignatureDiscardAndStructureZeroOffsetProbe(t *testing.T) {
	data := zeroReferenceFixture("<< /Type /StructTreeRoot /ParentTree 6 0 R >>", "", "", "/StructTreeRoot 7 0 R /Perms << >>")
	inspect := func(policies SourcePolicies) []diag.Diagnostic {
		_, _, ds := InspectReader(t.Context(), bytes.NewReader(data), sha256.Sum256(data), int64(len(data)), policies, nil)
		return ds
	}
	hint := func(ds []diag.Diagnostic) bool {
		return slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return strings.Contains(d.Message, `"structure": "discard"`) })
	}
	if ds := inspect(SourcePolicies{}); hint(ds) {
		t.Fatalf("probe silently opted into signature discard: %v", ds)
	}
	if ds := inspect(SourcePolicies{Signatures: packet.SignaturesDiscard}); !hint(ds) {
		t.Fatalf("probe lost the authorized signature policy: %v", ds)
	}
	ds := inspect(SourcePolicies{Signatures: packet.SignaturesDiscard, Structure: packet.StructureDiscard})
	if diag.Outcome(ds) != "ok" {
		t.Fatal(ds)
	}
	admissionFinding(t, ds, diag.SourceSignatureDiscarded)
	admissionFinding(t, ds, diag.SourceXRefStructureReferenceDiscarded)
}

func TestSignatureDiscardAndTagOwnership(t *testing.T) {
	d := pdffixture.Tagged("tagged page")
	admissionEntry(d, 1, "/AcroForm << /Fields [9 0 R] >>")
	admissionEntry(d, 3, "/Annots [9 0 R]")
	d.Objs[6] = bytes.Replace(d.Objs[6], []byte("/K 0"), []byte("/K [0 10 0 R]"), 1)
	d.Objs[7] = bytes.Replace(d.Objs[7], []byte("[7 0 R]"), []byte("[7 0 R] 1 7 0 R"), 1)
	d.Objs = append(d.Objs,
		[]byte("<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /F 2 /Rect [0 0 0 0] /P 3 0 R /StructParent 1 /V << /Type /Sig >> >>"),
		[]byte("<< /Type /OBJR /Pg 3 0 R /Obj 9 0 R >>"))
	_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard, Structure: packet.StructurePreserve})
	if !slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscardUnsupported }) {
		t.Fatalf("preserve hid original structure ownership: %v", ds)
	}
	_, _, ds = inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard, Structure: packet.StructureDiscard})
	if diag.Outcome(ds) != "ok" {
		t.Fatal(ds)
	}
	admissionFinding(t, ds, diag.SourceSignatureDiscarded)
	admissionFinding(t, ds, diag.SourceStructureRemoved)
}

func TestSignatureDiscardCancellationAndMetadataDepth(t *testing.T) {
	d := pdffixture.SignatureValue("depth")
	data := d.Bytes()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, findings := InspectReader(ctx, bytes.NewReader(data), sha256.Sum256(data), int64(len(data)),
		SourcePolicies{Signatures: packet.SignaturesDiscard}, nil)
	if !slices.ContainsFunc(findings, func(d diag.Diagnostic) bool { return d.Code == diag.Interrupted }) {
		t.Fatal(findings)
	}

	admissionEntry(d, 6, "/Lock << /Private "+strings.Repeat("[", 65)+"0"+strings.Repeat("]", 65)+" >>")
	_, _, findings = inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
	admissionFinding(t, findings, diag.SourceSignatureDiscardUnsupported)
}

func TestSignatureDiscardRefusesInvalidOptionalMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, catalog, flags string
	}{
		{"wrong flag generation", "", " /SigFlags 7 1 R"},
		{"noninteger flags", "", " /SigFlags << >>"},
		{"wrong permission generation", "/Perms 7 1 R", ""},
		{"non-dictionary permission", "/Perms 3", ""},
		{"undefined timestamp", "/DocTimeStamp 99 0 R", ""},
		{"undefined validation store", "/DSS 99 0 R", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.EmptySignatureField("unsigned")
			d.Objs = append(d.Objs, []byte("null"))
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/Fields [6 0 R]"), []byte("/Fields [6 0 R]"+tc.flags), 1)
			if tc.catalog != "" {
				admissionEntry(d, 1, tc.catalog)
			}
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			admissionFinding(t, ds, diag.SourceSignatureDiscardUnsupported)
		})
	}
}

func TestDegenerateSignatureWidgetPrintBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, flags, appearance string
		want                    diag.Code
	}{
		{"printable", "/F 4", "/AP << /N 8 0 R >>", diag.SourceSignatureDiscardUnsupported},
		{"printable state appearance", "/F 4", "/AP << /N << /On 8 0 R >> >>", diag.SourceSignatureDiscardUnsupported},
		{"printable rollover only", "/F 4", "/AP << /R 8 0 R >>", diag.SourceSignatureDiscardUnsupported},
		{"printable down only", "/F 4", "/AP << /D 8 0 R >>", diag.SourceSignatureDiscardUnsupported},
		{"printable rollover with indirect null normal", "/F 4", "/AP << /N 9 0 R /R 8 0 R >>", diag.SourceSignatureDiscardUnsupported},
		{"hidden rollover", "/F 6", "/AP << /R 8 0 R >>", diag.SourceSignatureDiscarded},
		{"unprinted down", "/F 0", "/AP << /D 8 0 R >>", diag.SourceSignatureDiscarded},
		{"hidden", "/F 6", "/AP << /N 8 0 R >>", diag.SourceSignatureDiscarded},
		{"unprinted", "/F 0", "/AP << /N 8 0 R >>", diag.SourceSignatureDiscarded},
		{"no appearance", "/F 4", "", diag.SourceSignatureDiscarded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			d.Objs = append(d.Objs, []byte("null"))
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[75 85 75 145]"), 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/F 4"), []byte(tc.flags), 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/AP << /N 8 0 R >>"), []byte(tc.appearance), 1)
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			admissionFinding(t, ds, tc.want)
			if tc.want == diag.SourceSignatureDiscarded {
				if diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
				return
			}
			detail := admissionError(t, ds)
			if !strings.Contains(detail.Message, "decoded appearance") || detail.PDFLocation == nil ||
				detail.PDFLocation.Page != 1 || detail.PDFLocation.Object != 6 || !strings.HasPrefix(detail.PDFLocation.Path, "Object.AP.") {
				t.Fatalf("missing original widget location or print reason: %v", detail)
			}
		})
	}
}

func TestXFAAdmissionRequiresBoundVisibleDataWidget(t *testing.T) {
	for _, tc := range []struct {
		name, kind, flags, rect, owner, annots, xfa string
		want                                        diag.Code
	}{
		{"visible", "Tx", "4", "10 20 100 40", "3 0 R", "7 0 R", "6 0 R", diag.SourceXfaRemoved},
		{"signature", "Sig", "4", "10 20 100 40", "3 0 R", "7 0 R", "6 0 R", diag.SourceDynamicForm},
		{"hidden", "Tx", "2", "10 20 100 40", "3 0 R", "7 0 R", "6 0 R", diag.SourceDynamicForm},
		{"degenerate", "Tx", "4", "10 20 10 40", "3 0 R", "7 0 R", "6 0 R", diag.SourceDynamicForm},
		{"outside crop", "Tx", "4", "310 310 350 350", "3 0 R", "7 0 R", "6 0 R", diag.SourceDynamicForm},
		{"detached", "Tx", "4", "10 20 100 40", "3 0 R", "", "6 0 R", diag.SourceDynamicForm},
		{"conflicting owner", "Tx", "4", "10 20 100 40", "2 0 R", "7 0 R", "6 0 R", diag.SourceDynamicForm},
		{"wrong XFA generation", "Tx", "4", "10 20 100 40", "3 0 R", "7 0 R", "6 1 R", diag.SourceInvalid},
		{"undefined XFA", "Tx", "4", "10 20 100 40", "3 0 R", "7 0 R", "99 0 R", diag.SourceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.Plain("Static fallback")
			admissionEntry(d, 1, fmt.Sprintf("/AcroForm << /XFA %s /Fields [7 0 R] /DA (/F1 12 Tf 0 g) /DR << /Font << /F1 5 0 R >> >> >>", tc.xfa))
			admissionEntry(d, 3, fmt.Sprintf("/CropBox [0 0 300 300] /Annots [%s]", tc.annots))
			d.Objs = append(d.Objs, admissionStream("", "<template/>"),
				fmt.Appendf(nil, "<< /Type /Annot /Subtype /Widget /FT /%s /F %s /T (sample) /P %s /Rect [%s] >>", tc.kind, tc.flags, tc.owner, tc.rect))
			for _, policy := range []packet.SignaturePolicy{packet.SignaturesRefuse, packet.SignaturesDiscard} {
				_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: policy})
				admissionFinding(t, ds, tc.want)
				if tc.want == diag.SourceXfaRemoved {
					if diag.Outcome(ds) != "ok" {
						t.Fatal(ds)
					}
					continue
				}
				finding := admissionError(t, ds)
				if finding.Code != tc.want || finding.PDFLocation.Object != 1 || finding.PDFLocation.Path != "Catalog.AcroForm.XFA" ||
					tc.want == diag.SourceInvalid && !strings.Contains(finding.Message, "undefined PDF object") {
					t.Fatalf("XFA refusal: %+v", finding)
				}
			}
		})
	}
}

func TestXFAWidgetOwnershipAndInheritedFlagsResolveLiveNull(t *testing.T) {
	for _, tc := range []struct {
		name, field, widget, nullValue string
		want                           diag.Code
		path                           string
	}{
		{"root null parent", "7 0 R", "/FT /Tx /Parent 9 0 R /P 3 0 R", "null", diag.SourceXfaRemoved, "Catalog.AcroForm.XFA"},
		{"widget null page", "7 0 R", "/FT /Tx /P 9 0 R", "null", diag.SourceXfaRemoved, "Catalog.AcroForm.XFA"},
		{"wrong parent generation", "7 0 R", "/FT /Tx /Parent 9 1 R /P 3 0 R", "null", diag.SourceInvalid, "Parent"},
		{"wrong page generation", "7 0 R", "/FT /Tx /P 9 1 R", "null", diag.SourceInvalid, "P"},
		{"inherited pushbutton", "8 0 R", "/Parent 8 0 R /P 3 0 R", "null", diag.SourceDynamicForm, "Catalog.AcroForm.XFA"},
		{"null flags inherit pushbutton", "8 0 R", "/Parent 8 0 R /P 3 0 R /Ff 9 0 R", "null", diag.SourceDynamicForm, "Catalog.AcroForm.XFA"},
		{"zero flags override pushbutton", "8 0 R", "/Parent 8 0 R /P 3 0 R /Ff 9 0 R", "0", diag.SourceXfaRemoved, "Catalog.AcroForm.XFA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.Plain("Static fallback")
			admissionEntry(d, 1, fmt.Sprintf("/AcroForm << /XFA 6 0 R /Fields [%s] /DA (/F1 12 Tf 0 g) /DR << /Font << /F1 5 0 R >> >> >>", tc.field))
			admissionEntry(d, 3, "/Annots [7 0 R]")
			d.Objs = append(d.Objs, admissionStream("", "<template/>"),
				fmt.Appendf(nil, "<< /Type /Annot /Subtype /Widget /F 4 /T (sample) /Rect [10 20 100 40] %s >>", tc.widget),
				[]byte("<< /FT /Btn /Ff 65536 /T (parent) /Kids [7 0 R] >>"), []byte(tc.nullValue))
			if tc.field == "7 0 R" {
				d.Objs[7] = []byte("null")
			}
			_, _, ds := inspectSignatures(t, d, SourcePolicies{})
			admissionFinding(t, ds, tc.want)
			if !slices.ContainsFunc(ds, func(d diag.Diagnostic) bool {
				return d.Code == tc.want && d.PDFLocation != nil && d.PDFLocation.Path == tc.path
			}) {
				t.Fatalf("unexpected source location: %v", ds)
			}
			if tc.want == diag.SourceXfaRemoved && diag.Outcome(ds) != "ok" {
				t.Fatal(ds)
			}
		})
	}
}

// Two stored DEFLATE blocks split a single PDF comment. The second
// block's inverted length ends with LF, immediately before "endstream".
func storedAppearanceComment(t *testing.T) []byte {
	t.Helper()
	left := []byte("%")
	const secondLength = 0xf500
	right := []byte("endstream " + strings.Repeat("X", secondLength-len("endstream ")))
	block := func(data []byte, last byte, length uint16, payload []byte) []byte {
		data = append(data, last)
		data = binary.LittleEndian.AppendUint16(data, length)
		data = binary.LittleEndian.AppendUint16(data, ^length)
		return append(data, payload...)
	}
	content := append(bytes.Clone(left), right...)
	raw := block([]byte{0x78, 0x01}, 0, 1, left)
	raw = block(raw, 1, secondLength, right)
	raw = binary.BigEndian.AppendUint32(raw, adler32.Checksum(content))
	reader, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	decoded, readErr := io.ReadAll(reader)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if readErr != nil || !bytes.Equal(decoded, content) || !bytes.Contains(raw, []byte("\nendstream ")) {
		t.Fatalf("fixture must decode into a single comment and contain a physical token collision: %v", readErr)
	}
	return raw
}

func TestDegenerateSignatureEmptyAppearanceProof(t *testing.T) {
	// An exact eight-byte zlib member for the empty program.
	emptyFlate := []byte{0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01}
	badChecksum := bytes.Clone(emptyFlate)
	badChecksum[len(badChecksum)-1] ^= 1
	const form = "/Type /XObject /Subtype /Form /BBox [0 0 0 0] /Matrix [1 0 0 1 0 0] /Resources << >>"
	const flate = " /Filter /FlateDecode"
	const paint = "0 1 0 rg 10 10 50 30 re f\n"
	for _, tc := range []struct {
		name, attributes, program, entries string
		alternate                          []byte
		accept                             bool
	}{
		{"raw empty", form, "", "/N 8 0 R", nil, true},
		{"opaque Flate token collision", form + flate, string(storedAppearanceComment(t)), "/N 8 0 R", nil, true},
		{"PDF trivia in a nonzero box", strings.Replace(form, "[0 0 0 0]", "[0 0 100 60]", 1), "\x00\t\f\r\n% q Q\r% EOF comment", "/N 8 0 R", nil, true},
		{"empty Flate with explicit defaults", form + flate + " /DecodeParms << /Predictor 1 /Colors 1 /Columns 1 /BitsPerComponent 8 >>", string(emptyFlate), "/N 8 0 R", nil, true},
		{"live null", form, "", "/N 9 0 R", []byte("null"), true},
		{"shared trivia in all states", form, "% blank", "/N << /Off 8 0 R /On 8 0 R >> /R 8 0 R /D 8 0 R", nil, true},
		{"paint after comment", form, "% note\r" + paint, "/N 8 0 R", nil, false},
		{"operand only", form, "42", "/N 8 0 R", nil, false},
		{"operator only", form, "q Q", "/N 8 0 R", nil, false},
		{"truncated Flate", form + flate, string(emptyFlate[:len(emptyFlate)-1]), "/N 8 0 R", nil, false},
		{"bad checksum", form + flate, string(badChecksum), "/N 8 0 R", nil, false},
		{"trailing Flate member", form + flate, string(emptyFlate) + string(emptyFlate), "/N 8 0 R", nil, false},
		{"trailing byte", form + flate, string(emptyFlate) + "\x00", "/N 8 0 R", nil, false},
		{"unsupported filter", form + " /Filter /ASCIIHexDecode", ">", "/N 8 0 R", nil, false},
		{"nondefault Flate parameters", form + flate + " /DecodeParms << /Predictor 2 >>", string(emptyFlate), "/N 8 0 R", nil, false},
		{"nonlocal Form", form + " /Ref << /F (external.pdf) /Page 0 >>", "", "/N 8 0 R", nil, false},
		{"appearance Form's own group", form + " /Group << /S /Transparency >>", "", "/N 8 0 R", nil, false},
		{"external appearance stream", form + " /F (external.pdf)", "", "/N 8 0 R", nil, false},
		{"painted rollover", form, "", "/N 8 0 R /R 9 0 R", admissionStream(form, paint), false},
		{"painted down appearance", form, "", "/N 8 0 R /D 9 0 R", admissionStream(form, paint), false},
		{"unselected painted state", form, "", "/N << /Off 8 0 R /On 9 0 R >>", admissionStream(form, paint), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			admissionEntry(d, 3, "/Group << /S /Transparency /CS /DeviceRGB >>")
			admissionEntry(d, 6, "/BM /Multiply /CA 0.5 /ca 0.25")
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/AP << /N 8 0 R >>"), []byte("/AS /Off /AP << "+tc.entries+" >>"), 1)
			d.Objs[7] = admissionStream(tc.attributes, tc.program)
			if tc.alternate != nil {
				d.Objs = append(d.Objs, tc.alternate)
			}
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if tc.accept {
				if diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
				admissionFinding(t, ds, diag.SourceSignatureDiscarded)
				return
			}
			finding := admissionError(t, ds)
			if finding.Code != diag.SourceSignatureDiscardUnsupported || finding.PDFLocation == nil ||
				finding.PDFLocation.Object != 6 || finding.PDFLocation.Page != 1 || !strings.HasPrefix(finding.PDFLocation.Path, "Object.AP.") {
				t.Fatalf("expected located print refusal: %+v", finding)
			}
			switch tc.name {
			case "paint after comment":
				if !strings.Contains(finding.Message, "decoded appearance") || finding.PDFLocation.Path != "Object.AP.N" {
					t.Fatalf("paint should fail the stream proof: %+v", finding)
				}
			case "bad checksum":
				if !strings.Contains(finding.Message, "damaged encoding") || finding.PDFLocation.Path != "Object.AP.N" {
					t.Fatalf("encoding should fail the stream proof: %+v", finding)
				}
			}
		})
	}
}

func TestPrintableDegenerateSignatureRegeneration(t *testing.T) {
	for _, tc := range []struct {
		name, setting, appearance, flags string
		want                             bool
	}{
		{"absent setting and AP", "", "", "4", true},
		{"false setting and null AP", "/NeedAppearances false", "/AP 9 0 R", "4", true},
		{"live null setting and state", "/NeedAppearances 9 0 R", "/AP << /N << /Off 9 0 R >> >>", "4", true},
		{"active with a live AP", "/NeedAppearances true", "/AP << /N 8 0 R >>", "4", false},
		{"active without AP", "/NeedAppearances true", "", "4", false},
		{"active with all states null", "/NeedAppearances true", "/AP << /N << /Off 9 0 R >> /R 9 0 R /D 9 0 R >>", "4", false},
		{"wrong setting generation", "/NeedAppearances 9 1 R", "", "4", false},
		{"unproved setting", "/NeedAppearances (unknown)", "", "4", false},
		{"active with hidden widget", "/NeedAppearances true", "", "6", true},
		{"active with nonprint widget", "/NeedAppearances true", "", "0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			d.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /AcroForm 10 0 R >>")
			d.Objs[5] = fmt.Appendf(nil, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (sample) /V 7 0 R /P 3 0 R /Rect [0 0 0 0] /F %s %s >>", tc.flags, tc.appearance)
			d.Objs[7] = admissionStream("/Type /XObject /Subtype /Form /BBox [0 0 0 0]", "% blank")
			d.Objs = append(d.Objs, []byte("null"), fmt.Appendf(nil, "<< /Fields [6 0 R] %s >>", tc.setting))
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if tc.want {
				admissionFinding(t, ds, diag.SourceSignatureDiscarded)
				if diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
				return
			}
			finding := admissionError(t, ds)
			if finding.Code != diag.SourceSignatureDiscardUnsupported || !strings.Contains(finding.Message, "regeneration") ||
				finding.PDFLocation == nil || *finding.PDFLocation != (diag.PDFLocation{Object: 10, Path: "NeedAppearances"}) {
				t.Fatalf("expected form-level regeneration refusal: %+v", finding)
			}
		})
	}
}

func TestPrintableDegenerateSignatureTransparencySettings(t *testing.T) {
	for _, tc := range []struct {
		name, entries, indirect, badKey string
	}{
		{"direct settings", "/BM /Multiply /CA 0.5 /ca 1", "", ""},
		{"indirect blend mode", "/BM 9 0 R", "/Screen", ""},
		{"indirect alpha", "/CA 9 0 R /ca 9 0 R", "0.25", ""},
		{"direct null settings", "/BM null /CA null /ca null", "", ""},
		{"indirect null settings", "/BM 9 0 R /CA 9 0 R /ca 9 0 R", "null", ""},
		{"blend number", "/BM 42", "", "BM"},
		{"blend array", "/BM [/Multiply /Normal]", "", "BM"},
		{"indirect blend number", "/BM 9 0 R", "42", "BM"},
		{"stroking alpha string", "/CA (wrong)", "", "CA"},
		{"indirect stroking alpha string", "/CA 9 0 R", "(wrong)", "CA"},
		{"nonstroking alpha string", "/ca (wrong)", "", "ca"},
		{"indirect nonstroking alpha string", "/ca 9 0 R", "(wrong)", "ca"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
			d.Objs[7] = admissionStream("/Type /XObject /Subtype /Form /BBox [0 0 0 0]", "% blank")
			admissionEntry(d, 6, tc.entries)
			if tc.indirect != "" {
				d.Objs = append(d.Objs, []byte(tc.indirect))
			}
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if tc.badKey == "" {
				admissionFinding(t, ds, diag.SourceSignatureDiscarded)
				if diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
				return
			}
			want := diag.PDFLocation{Object: 6, Page: 1, Path: "Object." + tc.badKey}
			if diag.Outcome(ds) != "invalid" ||
				!slices.ContainsFunc(ds, func(d diag.Diagnostic) bool {
					return d.Code == diag.SourceSignatureDiscardUnsupported && d.PDFLocation != nil && *d.PDFLocation == want &&
						strings.Contains(d.Message, "malformed")
				}) || slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded }) {
				t.Fatalf("expected malformed setting refusal at %+v, got %+v", want, ds)
			}
		})
	}
}

func TestSignatureCompositingAndRetainedGroupAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, entry, path string
		object            int
		degenerate        bool
		want              diag.Code
	}{
		{"visible page group", "/Group << /S /Transparency /CS /DeviceRGB >>", "Object.Group", 3, false, diag.SourceSignatureDiscardUnsupported},
		{"visible blend mode", "/BM /Multiply", "Object.BM", 6, false, diag.SourceSignatureDiscardUnsupported},
		{"visible stroking alpha", "/CA 0.5", "Object.CA", 6, false, diag.SourceSignatureDiscardUnsupported},
		{"visible nonstroking alpha", "/ca 0.5", "Object.ca", 6, false, diag.SourceSignatureDiscardUnsupported},
		{"invalid retained page group", "/Group << /S /Unknown >>", "", 3, true, diag.SourceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			admissionEntry(d, tc.object, tc.entry)
			if tc.degenerate {
				d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
				d.Objs[7] = admissionStream("/Type /XObject /Subtype /Form /BBox [0 0 0 0]", "")
			}
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			finding := admissionError(t, ds)
			if finding.Code != tc.want || tc.path != "" &&
				(finding.PDFLocation == nil || *finding.PDFLocation != (diag.PDFLocation{Object: tc.object, Page: 1, Path: tc.path})) {
				t.Fatalf("unexpected compositing decision: %+v", finding)
			}
		})
	}
}

func TestDegenerateSignatureAppearancePhysicalBoundary(t *testing.T) {
	empty := string([]byte{0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01})
	const paint = "0 1 0 rg 10 10 50 30 re f\n"
	for _, tc := range []struct {
		name, length, start, data, ending string
		flate, accept                     bool
	}{
		{"raw empty without extra EOL", "0", "\n", "", "endstream", false, true},
		{"raw includes EOL", "10", "\n", "% comment\n", "endstream", false, true},
		{"Flate with CRLF", "8", "\r\n", empty, "\r\nendstream", true, true},
		{"Flate with live indirect length", "9 0 R", "\n", empty, "endstream", true, true},
		{"short raw hides paint", "1", "\n", " " + paint, "\nendstream", false, false},
		{"zero length repaired to trivia", "0", "\n", "% ignored", "\nendstream", false, false},
		{"short Flate hides member", "8", "\n", empty + empty, "\nendstream", true, false},
		{"incomplete endstream token", "8", "\n", empty, "\nendstreamJ", true, false},
		{"junk after endstream", "8", "\n", empty, "\nendstream\nignored", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
			encoding := ""
			if tc.flate {
				encoding = " /Filter /FlateDecode"
			}
			d.Objs[7] = fmt.Appendf(nil, "<< /Type /XObject /Subtype /Form /BBox [0 0 100 60] /Length %s%s >>\nstream%s%s%s",
				tc.length, encoding, tc.start, tc.data, tc.ending)
			if tc.length == "9 0 R" {
				d.Objs = append(d.Objs, []byte("8"))
			}
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if tc.accept {
				if diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
				admissionFinding(t, ds, diag.SourceSignatureDiscarded)
				return
			}
			finding := admissionError(t, ds)
			if finding.Code != diag.SourceSignatureDiscardUnsupported || finding.PDFLocation == nil ||
				finding.PDFLocation.Object != 6 || finding.PDFLocation.Page != 1 || !strings.HasPrefix(finding.PDFLocation.Path, "Object.AP.") {
				t.Fatalf("expected located print refusal: %+v", finding)
			}
		})
	}
}

func TestDegenerateSignatureAppearanceIndirectSubtype(t *testing.T) {
	for _, tc := range []struct {
		subtype, reference string
		accept             bool
	}{
		{"/Form", "9 0 R", true},
		{"/Form", "9 1 R", false},
		{"/Image", "9 0 R", false},
		{"(Form)", "9 0 R", false},
	} {
		t.Run(tc.subtype+" "+tc.reference, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
			d.Objs[7] = admissionStream("/Type /XObject /Subtype "+tc.reference+" /BBox [0 0 0 0]", "")
			d.Objs = append(d.Objs, []byte(tc.subtype))
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if tc.accept {
				if diag.Outcome(ds) != "ok" {
					t.Fatal(ds)
				}
				admissionFinding(t, ds, diag.SourceSignatureDiscarded)
			} else {
				finding := admissionError(t, ds)
				if finding.PDFLocation == nil {
					t.Fatalf("expected an original source location: %+v", finding)
				}
			}
		})
	}
}

func TestDegenerateSignatureAppearanceRefusalReasons(t *testing.T) {
	empty := string([]byte{0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01})
	const form = "/Type /XObject /Subtype /Form /BBox [0 0 100 60]"
	for _, tc := range []struct {
		name, attributes, program, closing string
		message                            string
	}{
		{"decoded content", form, "0 0 20 20 re f", "endstream", "decoded appearance contains more than PDF whitespace and comments"},
		{"damaged encoding", form + " /Filter /FlateDecode", empty[:len(empty)-1] + "\x02", "endstream", "damaged encoding"},
		{"closing scan limit", form + " /Filter /FlateDecode", empty, "endstream %" + strings.Repeat("x", diag.SignatureAppearanceClosingLookaheadBytes) + "\n", "4096-byte closing-token lookahead limit"},
		{"malformed closing token", form + " /Filter /FlateDecode", empty, "endstreamJ %" + strings.Repeat("x", diag.SignatureAppearanceClosingLookaheadBytes) + "\n", "malformed closing tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := pdffixture.SignatureArtwork()
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/AP << /N 8 0 R >>"), []byte("/AS /Proof /AP << /N << /Proof 8 0 R >> >>"), 1)
			d.Objs[7] = fmt.Appendf(nil, "<< %s /Length %d >>\nstream\n%s\n%s", tc.attributes, len(tc.program), tc.program, tc.closing)
			_, _, ds := inspectSignatures(t, d, SourcePolicies{Signatures: packet.SignaturesDiscard})
			finding := admissionError(t, ds)
			if finding.Code != diag.SourceSignatureDiscardUnsupported || !strings.Contains(finding.Message, tc.message) ||
				finding.PDFLocation == nil || finding.PDFLocation.Object != 6 || finding.PDFLocation.Page != 1 ||
				finding.PDFLocation.Path != `Object.AP.N["Proof"]` || !strings.Contains(finding.Detail, "PDF object 8 with generation 0") {
				t.Fatalf("unexpected reason or appearance site: %+v", finding)
			}
		})
	}
}
