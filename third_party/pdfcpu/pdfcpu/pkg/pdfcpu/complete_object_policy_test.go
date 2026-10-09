/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package pdfcpu_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func packedPolicyPDF(body string) []byte {
	var output bytes.Buffer
	output.WriteString("%PDF-1.7\n")
	offsets := make([]int, 8)
	add := func(number int, value string) {
		offsets[number] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", number, value)
	}
	add(1, "<< /Type /Catalog /Pages 2 0 R /Probe 6 0 R >>")
	add(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	add(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << >> >>")
	add(4, "null")
	stream := "6 0 " + body
	add(5, fmt.Sprintf("<< /Type /ObjStm /N 1 /First 4 /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	offsets[7] = output.Len()
	var xref bytes.Buffer
	for number := 0; number < 8; number++ {
		var entry [7]byte
		switch number {
		case 0:
			binary.BigEndian.PutUint16(entry[5:], 65535)
		case 6:
			entry[0] = 2
			binary.BigEndian.PutUint32(entry[1:5], 5)
		default:
			entry[0] = 1
			binary.BigEndian.PutUint32(entry[1:5], uint32(offsets[number]))
		}
		xref.Write(entry[:])
	}
	fmt.Fprintf(&output, "7 0 obj\n<< /Type /XRef /Size 8 /Root 1 0 R /W [1 4 2] /Length %d >>\nstream\n", xref.Len())
	output.Write(xref.Bytes())
	fmt.Fprintf(&output, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", offsets[7])
	return output.Bytes()
}

func readPolicyObject(t *testing.T, body string, mode int, packed bool) (types.Object, *model.Context, error) {
	t.Helper()
	configuration := model.NewDefaultConfiguration()
	configuration.ValidationMode = mode
	if packed {
		pdf, err := api.ReadContext(t.Context(), bytes.NewReader(packedPolicyPDF(body)), configuration)
		if err != nil {
			return nil, pdf, err
		}
		value, err := pdf.DereferenceContext(t.Context(), *types.NewIndirectRef(6, 0))
		return value, pdf, err
	}
	pdf, err := model.NewContext(bytes.NewReader([]byte("1 0 obj\n"+body+"\nendobj\n")), configuration)
	if err != nil {
		return nil, pdf, err
	}
	value, err := pdfcpu.ParseObject(t.Context(), pdf, 0, 1, 0)
	return value, pdf, err
}

func TestCompleteSourceBodiesRejectMaterialSuffixAcrossPackedAndOrdinaryReaders(t *testing.T) {
	for _, body := range []string{"1.0 junk", "1.0 1,5", "1 0 Rgarbage", "[1] false", "<< /Value 1 >> /extra"} {
		for _, mode := range []int{model.ValidationStrict, model.ValidationRelaxed} {
			for _, packed := range []bool{false, true} {
				if _, _, err := readPolicyObject(t, body, mode, packed); err == nil || !strings.Contains(err.Error(), "material trailing data") {
					t.Fatalf("complete body suffix accepted mode%d packed%t %q: %v", mode, packed, body, err)
				}
			}
		}
	}
}

func TestCompleteSourceBodiesRetainCommentSeparatedReferencesAndCommentTails(t *testing.T) {
	for _, body := range []string{"<< /Value 1%first\n0 R >>", "<< /Value 1 0%second\nR >>", "[1 0%second\nR]", "1.0 \t%comment with junk /key 1,5"} {
		for _, mode := range []int{model.ValidationStrict, model.ValidationRelaxed} {
			for _, packed := range []bool{false, true} {
				value, pdf, err := readPolicyObject(t, body, mode, packed)
				if err != nil || value == nil || len(pdf.ValidationReport().Notices()) != 0 {
					t.Fatalf("valid comment tokenization changed mode%d packed%t %q: %v %v", mode, packed, body, value, err)
				}
				if strings.HasPrefix(body, "<<") {
					dict, ok := value.(types.Dict)
					if !ok || dict["Value"] != *types.NewIndirectRef(1, 0) {
						t.Fatalf("reference identity lost: %v", value)
					}
				}
			}
		}
	}
}

func TestPackedDictionaryPolicyMatchesStrictRefusalAndClassifiedRelaxedNotice(t *testing.T) {
	for _, packed := range []bool{false, true} {
		if _, _, err := readPolicyObject(t, "<< /Value 1 0 Rgarbage >>", model.ValidationStrict, packed); err == nil {
			t.Fatalf("strict packed%t accepted malformed dictionary without policy", packed)
		}
		value, pdf, err := readPolicyObject(t, "<< /Value 1 0 Rgarbage >>", model.ValidationRelaxed, packed)
		if err != nil || value == nil || len(pdf.ValidationReport().Notices()) != 1 {
			t.Fatalf("relaxed packed%t lost classified notice: %v %v", packed, value, err)
		}
	}
}

func TestMaintainedPrefixParserStillReturnsRemainingMaterial(t *testing.T) {
	source := "1.0 remaining"
	value, err := model.ParseObject(t.Context(), &source, 0)
	if err != nil || value != types.Float(1) || strings.TrimSpace(source) != "remaining" {
		t.Fatalf("complete-source check changed maintained prefix API: %v %q %v", value, source, err)
	}
}
