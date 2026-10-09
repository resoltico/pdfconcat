/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package model

import (
	"reflect"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestNativeParserAndWriterPreserveCanonicalDictionaryIdentity(t *testing.T) {
	for _, mode := range []int{ValidationStrict, ValidationRelaxed} {
		body := "<< /Cr#236fpBox [20 20 80 80] /CropBox [0 0 100 100] /unfinished#23 (private) >>"
		parsed, err := ParseObjectWithPolicy(t.Context(), &body, 0, mode)
		if err != nil {
			t.Fatal(err)
		}
		d, ok := parsed.Object.(types.Dict)
		if !ok {
			t.Fatal("dictionary not parsed")
		}
		if len(d) != 3 || d["Cr#6fpBox"] == nil || d["CropBox"] == nil || d["unfinished#"] == nil {
			t.Fatalf("canonical name identities lost: %v", d)
		}
		reencoded := d.PDFString()
		again, err := ParseObjectWithPolicy(t.Context(), &reencoded, 0, mode)
		if err != nil || !reflect.DeepEqual(again.Object, d) {
			t.Fatalf("canonical literal-hash roundtrip: %v %v", again.Object, err)
		}
		for _, duplicate := range []string{"<< /Cr#6fpBox null /CropBox [0 0 1 1] >>", "<< /A#42 1 /AB 2 >>"} {
			if _, err = ParseObjectWithPolicy(t.Context(), &duplicate, 0, mode); err == nil {
				t.Fatal("true canonical duplicate accepted")
			}
		}
	}
}
