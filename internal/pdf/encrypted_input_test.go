// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestEncryptedInputPolicyWiring(t *testing.T) {
	conf, err := configuration()
	if err != nil {
		t.Fatal(err)
	}
	if !conf.RejectEncryptedInput {
		t.Fatal("PDFConcat reader policy is disabled")
	}
	d := pdffixture.Plain("unread encryption dictionary")
	d.Objs = append(d.Objs, []byte("<< /Filter /NotASupportedSecurityHandler >>"))
	declaration := fmt.Sprintf("/Root 1 0 R /Encrypt %d 0 R /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>]", len(d.Objs))
	data := bytes.Replace(d.Bytes(), []byte("/Root 1 0 R"), []byte(declaration), 1)
	// The default backend reaches the deliberately unsupported dictionary. The
	// product policy must refuse before that parsing, including output reopening.
	ordinary := model.NewStatelessConfiguration()
	ordinary.ValidationMode = model.ValidationStrict
	_, defaultErr := api.ReadContext(t.Context(), bytes.NewReader(data), ordinary)
	if !errors.Is(defaultErr, pdfcpu.ErrUnsupportedEncryptionFeature) {
		t.Fatal("default control did not reach security handler", defaultErr)
	}
	_, _, _, err = readSource(t.Context(), data, "")
	if err == nil || diagnostic(err, diag.SourceInvalid).Code != diag.SourceEncrypted {
		t.Fatal("typed encrypted-source mapping lost", err)
	}
	if _, err = readOutput(t.Context(), data); !errors.Is(err, pdfcpu.ErrEncrypted) {
		t.Fatal("output reader performed unsupported encryption work", err)
	}
}

func TestEncryptionPolicyUsesParsedMetadata(t *testing.T) {
	for _, null := range []bool{false, true} {
		data := pdffixture.Plain("Literal /Encrypt 99 0 R is ordinary content").Bytes()
		if null {
			data = bytes.Replace(data, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Encrypt null"), 1)
		}
		s, _, _, err := readSource(t.Context(), data, "")
		if err != nil || s == nil || s.Encrypt != nil {
			t.Fatal("inert encryption text or null became encryption", null, err)
		}
	}
}
