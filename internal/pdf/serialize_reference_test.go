// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestSerializeRejectsInvalidIndirectObjectIdentityBeforeWriting(t *testing.T) {
	for _, state := range []string{"missing", "free", "wrong-generation"} {
		t.Run(state, func(t *testing.T) {
			s := parsedFixture(t, pdffixture.Plain("reference control"))
			pages, err := rawPages(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := s.IndRefForNewObject(types.Integer(42))
			if err != nil {
				t.Fatal(err)
			}
			number := ref.ObjectNumber.Value()
			switch state {
			case "missing":
				delete(s.Table, number)
			case "free":
				if err := s.FreeObject(number); err != nil {
					t.Fatal(err)
				}
				// Use the free entry's actual generation to isolate its status.
				ref.GenerationNumber = types.Integer(generation(s.Table[number]))
			case "wrong-generation":
				ref.GenerationNumber++
			}
			pages[0].dict["PrivateReference"] = *ref
			var output bytes.Buffer
			err = serialize(t.Context(), s, &output)
			if err == nil || !strings.Contains(err.Error(), "dangling output object "+ref.PDFString()) || output.Len() != 0 {
				t.Fatalf("serialize returned %v and wrote %d bytes", err, output.Len())
			}
		})
	}
}
