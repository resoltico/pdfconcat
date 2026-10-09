/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package pdfcpu_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func classicFreeHeadPDF(head string, missing bool) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 1 /Kids [3 0 R] >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>"}
	offsets := make([]int, len(objects))
	for index, object := range objects {
		offsets[index] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xrefOffset := out.Len()
	if missing {
		out.WriteString("xref\n1 3\n")
	} else {
		fmt.Fprintf(&out, "xref\n0 4\n%s \n", head)
	}
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return out.Bytes()
}

func packedFreeHeadPDF(kind byte) []byte {
	source := packedPolicyPDF("null")
	start := bytes.LastIndex(source, []byte("\nstream\n")) + len("\nstream\n")
	clear(source[start : start+7])
	source[start] = kind
	if kind == 1 {
		binary.BigEndian.PutUint32(source[start+1:start+5], 9)
		binary.BigEndian.PutUint16(source[start+5:start+7], 65535)
	} else {
		binary.BigEndian.PutUint32(source[start+1:start+5], 5)
	}
	return source
}

func TestReadRejectsRetainedInUseFreeHeadAcrossClassicAndStreamEntries(t *testing.T) {
	for name, source := range map[string][]byte{"classic": classicFreeHeadPDF("0000000009 65535 n", false), "stream type1": packedFreeHeadPDF(1), "stream type2": packedFreeHeadPDF(2)} {
		t.Run(name, func(t *testing.T) {
			original := bytes.Clone(source)
			_, err := api.ReadContext(t.Context(), bytes.NewReader(source), nil)
			if err == nil || !strings.Contains(err.Error(), "xref object 0 must be free") {
				t.Fatalf("retained in-use head was not rejected before allocator/body materialization: %v", err)
			}
			if !bytes.Equal(source, original) {
				t.Fatal("reader changed source bytes")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err = api.ReadContext(ctx, bytes.NewReader(source), nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("preexisting cancellation lost: %v", err)
			}
		})
	}
}

func TestReadPreservesKnownClassicFreeHeadNormalizationAndRepairs(t *testing.T) {
	for _, test := range []struct {
		name, head string
		missing    bool
	}{
		{"valid", "0000000000 65535 f", false},
		{"generation repair", "0000000000 00000 f", false},
		{"offset repair", "0000000099 65535 f", false},
		{"zero in-use normalization", "0000000000 65535 n", false},
		{"skipped low in-use offset", "0000000008 65535 n", false},
		{"missing head", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := classicFreeHeadPDF(test.head, test.missing)
			pdf, err := api.ReadContext(t.Context(), bytes.NewReader(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			head, ok := pdf.Find(0)
			if !ok || !head.Free || head.Offset == nil || *head.Offset != 0 || head.Generation == nil || *head.Generation != 65535 {
				t.Fatalf("known repaired free head invalid: %+v", head)
			}
			if _, err = pdf.IndRefForNewObject(types.Integer(1)); err != nil {
				t.Fatalf("repaired source cannot allocate: %v", err)
			}
		})
	}
}

func TestReadPreservesActualRecycledFreeObjectInsertion(t *testing.T) {
	source := classicFreeHeadPDF("0000000004 65535 f", false)
	source = bytes.Replace(source, []byte("xref\n0 4\n"), []byte("xref\n0 5\n"), 1)
	source = bytes.Replace(source, []byte("trailer\n<< /Size 4"), []byte("0000000000 00002 f \ntrailer\n<< /Size 5"), 1)
	pdf, err := api.ReadContext(t.Context(), bytes.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pdf.IndRefForNewObject(types.Integer(99))
	if err != nil || ref.ObjectNumber != 4 {
		t.Fatalf("actual free object was not recycled: %v %v", ref, err)
	}
	head, ok := pdf.Find(0)
	if !ok || !head.Free || head.Offset == nil || *head.Offset != 0 {
		t.Fatalf("recycled free-list head not consumed: %+v", head)
	}
	entry, ok := pdf.Find(4)
	if !ok || entry.Free || entry.Object != types.Integer(99) {
		t.Fatalf("actual recycled object payload invalid: %+v", entry)
	}
}
