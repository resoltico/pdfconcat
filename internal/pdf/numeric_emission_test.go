// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestContentNumbersUseFiniteRealSyntax(t *testing.T) {
	for _, value := range []float64{1, -1, 1e20, -1e20, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		token := number(value)
		decoded, err := strconv.ParseFloat(token, 64)
		if err != nil || decoded != value || !strings.ContainsRune(token, '.') || strings.ContainsAny(token, "eE") {
			t.Fatalf("invalid content real %g: %q", value, token)
		}
	}
	if number(math.Copysign(0, -1)) != "0" {
		t.Fatal("noncanonical zero")
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("nonfinite content number emitted")
				}
			}()
			number(value)
		}()
	}
}
func TestLargeRealContentOperatorsPassIndependentParserAndRaster(t *testing.T) {
	tools := oracle.RequireTools(t)
	var rendered []any
	for _, largeTranslations := range []bool{false, true} {
		source := pdffixture.Plain("numeric operators").Bytes()
		s, _, _, err := readSource(t.Context(), source, "")
		if err != nil {
			t.Fatal(err)
		}
		program := "q\n"
		if largeTranslations {
			// Cancellation happens between two matrices, before ordinary drawing
			// coordinates are mapped; the picture has an independent identity control.
			program += fmt.Sprintf("1 0 0 1 %s 0 cm\n1 0 0 1 %s 0 cm\n", number(-1e20), number(1e20))
		}
		program += "0 0 1 rg\n20 30 60 40 re f\nQ\n"
		stream, err := addStream(s, []byte(program), nil)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := rawPages(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		pages[0].dict["Contents"] = *stream
		var output bytes.Buffer
		if err = serialize(t.Context(), s, &output); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "output.pdf")
		if err = os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--check", path).CombinedOutput(); err != nil {
			t.Fatalf("qpdf: %v %s", err, raw)
		}
		prefix := filepath.Join(t.TempDir(), "page")
		if raw, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-f", "1", "-l", "1", "-singlefile", "-r", "72", "-png", path, prefix).CombinedOutput(); err != nil {
			t.Fatalf("Poppler: %v %s", err, raw)
		}
		data, err := os.ReadFile(prefix + ".png")
		if err != nil {
			t.Fatal(err)
		}
		image, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		rendered = append(rendered, image)
	}
	if !reflect.DeepEqual(rendered[0], rendered[1]) {
		t.Fatal("large real matrices changed the identity picture")
	}
}
