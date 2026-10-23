// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
	"github.com/resoltico/pdfconcat/internal/oracle"
)

func boundaryPageBytes(t *testing.T, mode string, page compose.Page) ([]byte, error) {
	t.Helper()
	var data bytes.Buffer
	if mode == "slice" {
		_, err := pdf.Write(t.Context(), &data, []compose.Page{page})
		return data.Bytes(), err
	}
	prepared := map[typeset.Digest]typeset.EmbeddedFont{}

	var placements []*typeset.Placed
	for _, block := range page.Blocks {
		if block.Text != nil && block.Text.Font != nil {
			placements = append(placements, block.Text)
		}
	}
	if len(placements) > 0 {
		var err error
		prepared, err = typeset.PrepareFonts(placements)
		if err != nil {
			return nil, err
		}
	}

	_, err := pdf.WriteRepeated(t.Context(), &data, page, 1, fontResources(prepared))
	return data.Bytes(), err
}

func checkBoundaryPage(t *testing.T, data []byte, width, height float64, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boundary.pdf")
	if err := os.WriteFile(filepath.Clean(path), data, 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	expected := oracle.Expectation{Pages: []oracle.ExpectedPage{{Text: text, Geometry: &oracle.Geometry{MediaBox: []float64{0, 0, width, height}}}}}
	if findings := doc.Verify(expected); len(findings) != 0 {
		t.Fatalf("independent generated-page boundary facts: %+v", findings)
	}
	pages, err := doc.RawTexts()
	if err != nil || len(pages) != 1 || strings.Join(strings.Fields(pages[0]), " ") != text {
		t.Fatalf("actual boundary text: %q %v", pages, err)
	}
}

func TestPageWriterAcceptsInclusiveGeometryAndFontBoundaries(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)
	for _, mode := range []string{"slice", "repeated"} {
		for _, dimensions := range [][2]float64{{1, 200}, {14400, 200}, {300, 1}, {300, 14400}} {
			t.Run(fmt.Sprintf("%s/page-%gx%g", mode, dimensions[0], dimensions[1]), func(t *testing.T) {
				page := compose.Page{Width: dimensions[0], Height: dimensions[1]}
				data, err := boundaryPageBytes(t, mode, page)
				if err != nil {
					t.Fatalf("inclusive page boundary refused: %v", err)
				}
				checkBoundaryPage(t, data, dimensions[0], dimensions[1], "")
			})
		}
		for _, size := range []float64{0.01, 14400} {
			t.Run(fmt.Sprintf("%s/font-%g", mode, size), func(t *testing.T) {
				block := placed(t, font, typeset.Params{
					Text: "A", Size: size, PageWidth: 14400, PageHeight: 14400,
					Anchor: typeset.AnchorCenter, LineSpacing: 1, Overflow: typeset.OverflowAllow,
				})
				page := compose.Page{Width: 14400, Height: 14400, Blocks: []compose.TextBlock{{Text: block}}}
				data, err := boundaryPageBytes(t, mode, page)
				if err != nil {
					t.Fatalf("inclusive font boundary refused: %v", err)
				}
				checkBoundaryPage(t, data, 14400, 14400, "A")
			})
		}
	}
}

func TestPageWriterRejectsImmediateOutOfRangeNeighbors(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)
	valid := textPage(t, font, "A")
	for _, mode := range []string{"slice", "repeated"} {
		for _, dimension := range []string{"width", "height"} {
			for _, value := range []float64{math.Nextafter(1, 0), math.Nextafter(14400, math.Inf(1))} {
				page := compose.Page{Width: 300, Height: 200}
				if dimension == "width" {
					page.Width = value
				} else {
					page.Height = value
				}
				if _, err := boundaryPageBytes(t, mode, page); !errors.Is(err, pdf.ErrInvalidPage) {
					t.Fatalf("%s %s neighbor %g was not a page-size refusal: %v", mode, dimension, value, err)
				}
			}
		}
		for _, size := range []float64{math.Nextafter(0.01, 0), math.Nextafter(14400, math.Inf(1))} {
			block := *valid.Blocks[0].Text
			block.Size = size
			page := valid
			page.Blocks = append([]compose.TextBlock(nil), valid.Blocks...)
			page.Blocks[0].Text = &block
			if _, err := boundaryPageBytes(t, mode, page); !errors.Is(err, pdf.ErrInvalidPage) {
				t.Fatalf("%s font neighbor %g was not a font-size refusal: %v", mode, size, err)
			}
		}
	}
}

func TestPageWriterEmptyTextDoesNotRequireUnusedFont(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"slice", "repeated"} {
		for _, block := range []*typeset.Placed{nil, {}} {
			page := compose.Page{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}
			data, err := boundaryPageBytes(t, mode, page)
			if err != nil {
				t.Fatalf("%s empty text required unused font: %v", mode, err)
			}
			checkBoundaryPage(t, data, 300, 200, "")
		}
	}
}

func TestRepeatedPageLimitAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	canceled, stop := context.WithCancel(ctx)
	stop()
	for _, count := range []int{100000, 100001} {
		_, err := pdf.WriteRepeated(canceled, io.Discard, compose.Page{Width: 100, Height: 100}, count, fontResources(nil))
		if count == 100000 && !errors.Is(err, context.Canceled) {
			t.Fatalf("accepted count lost cancellation: %v", err)
		}
		if count == 100001 && !errors.Is(err, pdf.ErrTooManyPages) {
			t.Fatalf("over-limit count accepted: %v", err)
		}
	}
	page := textPage(t, defaultFont(t), "font")
	if _, err := pdf.WriteRepeated(t.Context(), io.Discard, page, 1, fontResources(nil)); !errors.Is(err, pdf.ErrInvalidPage) {
		t.Fatalf("missing job-wide font accepted: %v", err)
	}
}
