// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestInlineImageExactSampleBoundary(t *testing.T) {
	const second = " BI /W 1 /H 1 /BPC 8 /CS /G ID B EI Q"
	first := "/Width 4 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray ID " + " EI " + " EI" + second
	budget := int64(1)
	tail, err := inlineTail(t.Context(), first, &budget, 0, false)
	if err != nil || tail != second || budget != 1 {
		t.Fatalf("first exact boundary: tail %q, budget %d, error %v", tail, budget, err)
	}
	tail, err = inlineTail(t.Context(), strings.TrimPrefix(tail, " BI "), &budget, 0, false)
	if err != nil || tail != " Q" {
		t.Fatalf("second exact boundary: tail %q, error %v", tail, err)
	}
	// Two RGB rows of one 4-bit pixel each occupy four bytes, including padding.
	tail, err = inlineTail(t.Context(), "/W 1 /H 2 /BPC 4 /CS /RGB ID abcd EI Q", &budget, 0, false)
	if err != nil || tail != " Q" {
		t.Fatalf("padded sample rows: tail %q, error %v", tail, err)
	}
}

func TestInlineImageRejectsAmbiguousFallbackAndTruncatedExactData(t *testing.T) {
	for _, tc := range []struct{ data, detail string }{
		{"/W 1 /H 1 /BPC 8 /CS /G /F /Fl ID A EI BI /W 1 /H 1 /BPC 8 /CS /G /F /Fl ID B EI Q", "ambiguous"},
		{"/W 20 /H 1 /BPC 8 /CS /G ID A EI Q", "truncated"},
		{"/W 2 /H 1 /BPC 8 /CS /G ID abc EI Q", "declared sample boundary"},
	} {
		budget := int64(8192)
		if _, err := inlineTail(t.Context(), tc.data, &budget, 0, false); err == nil || !strings.Contains(err.Error(), tc.detail) {
			t.Fatalf("%q: wanted %q, got %v", tc.data, tc.detail, err)
		}
	}
}

func TestExactInlineImageCompositionMatchesExternalizedControl(t *testing.T) {
	tools := oracle.RequireTools(t)
	const drawing = "q 32 0 0 8 10 20 cm BI /W 4 /H 1 /BPC 8 /CS /G ID AEIB EI Q\n" +
		"q 8 0 0 8 10 38 cm BI /W 1 /H 1 /BPC 8 /CS /G ID B EI Q"
	doc := pdffixture.EncodedPages("inline samples", []pdffixture.EncodedStream{{Data: []byte(drawing)}})
	dir := t.TempDir()
	input, control := filepath.Join(dir, "inline.pdf"), filepath.Join(dir, "external.pdf")
	if err := doc.WriteFile(input); err != nil {
		t.Fatal(err)
	}
	if text, err := exec.CommandContext(t.Context(), tools.QPDF, "--externalize-inline-images", "--ii-min-bytes=0", input, control).CombinedOutput(); err != nil {
		t.Fatalf("externalize control: %v, %s", err, text)
	}
	output, _ := selectedPacket(t, doc, []packet.Item{&packet.SourceItem{Ref: "inline", Path: input}}, nil)
	raster := func(path string) []byte {
		t.Helper()
		data, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-r", "72", "-singlefile", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if !bytes.Equal(raster(output), raster(control)) {
		t.Fatal("composed inline samples differ from independently externalized samples")
	}
}

func TestPDF20InlineImageExplicitLengthFramesAdjacentImages(t *testing.T) {
	const drawing = "q 8 0 0 8 10 20 cm BI /W 1 /H 1 /BPC 8 /CS /G /L 1 ID A EI Q\n" +
		"q 8 0 0 8 10 38 cm BI /W 1 /H 1 /BPC 8 /CS /G /Length 1 ID B EI Q"
	doc := pdffixture.EncodedPages("explicit samples", []pdffixture.EncodedStream{{Data: []byte(drawing)}})
	doc.Version = "2.0"
	output, _ := selectedPacket(t, doc, []packet.Item{&packet.SourceItem{Ref: "inline"}}, nil)
	if err := Verify(t.Context(), readPDF(t, output), 1); err != nil {
		t.Fatal(err)
	}
	budget := int64(8192)
	if _, err := inlineTail(t.Context(), "/W 1 /H 1 /BPC 8 /CS /G /L 2 ID AB EI", &budget, 0, true); err == nil {
		t.Fatal("explicit Length disagreed with the exact sample count")
	}
}
