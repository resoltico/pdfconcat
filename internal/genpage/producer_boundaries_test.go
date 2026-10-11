// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package genpage_test

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

	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func boundaryPageBytes(t *testing.T, mode string, page genpage.Page) ([]byte, error) {
	t.Helper()
	var data bytes.Buffer
	if mode == "slice" {
		_, err := genpage.Write(t.Context(), &data, []genpage.Page{page})
		return data.Bytes(), err
	}
	var fonts []*typeset.Font
	if page.Text != nil && page.Text.Font != nil {
		fonts = []*typeset.Font{page.Text.Font}
	}
	_, err := genpage.WriteProduced(t.Context(), &data, 1, fonts, func(int) (genpage.Page, error) { return page, nil })
	return data.Bytes(), err
}

func checkBoundaryPage(t *testing.T, data []byte, width, height float64, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boundary.pdf")
	if err := os.WriteFile(filepath.Clean(path), data, 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := pdforacle.Load(pdforacle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	expected := pdforacle.Expectation{Pages: []pdforacle.ExpectedPage{{Text: text, Geometry: &pdforacle.Geometry{MediaBox: []float64{0, 0, width, height}}}}}
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
	for _, mode := range []string{"slice", "produced"} {
		for _, dimensions := range [][2]float64{{1, 200}, {14400, 200}, {300, 1}, {300, 14400}} {
			t.Run(fmt.Sprintf("%s/page-%gx%g", mode, dimensions[0], dimensions[1]), func(t *testing.T) {
				page := genpage.Page{Width: dimensions[0], Height: dimensions[1]}
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
				page := genpage.Page{Width: 14400, Height: 14400, Text: block}
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
	for _, mode := range []string{"slice", "produced"} {
		for _, dimension := range []string{"width", "height"} {
			for _, value := range []float64{math.Nextafter(1, 0), math.Nextafter(14400, math.Inf(1))} {
				page := genpage.Page{Width: 300, Height: 200}
				if dimension == "width" {
					page.Width = value
				} else {
					page.Height = value
				}
				if _, err := boundaryPageBytes(t, mode, page); !errors.Is(err, genpage.ErrInvalidPage) {
					t.Fatalf("%s %s neighbor %g was not a page-size refusal: %v", mode, dimension, value, err)
				}
			}
		}
		for _, size := range []float64{math.Nextafter(0.01, 0), math.Nextafter(14400, math.Inf(1))} {
			block := *valid.Text
			block.Size = size
			page := valid
			page.Text = &block
			if _, err := boundaryPageBytes(t, mode, page); !errors.Is(err, genpage.ErrInvalidPage) {
				t.Fatalf("%s font neighbor %g was not a font-size refusal: %v", mode, size, err)
			}
		}
	}
}

func TestPageWriterEmptyTextDoesNotRequireUnusedFont(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"slice", "produced"} {
		for _, block := range []*typeset.Placed{nil, {}} {
			page := genpage.Page{Width: 300, Height: 200, Text: block}
			data, err := boundaryPageBytes(t, mode, page)
			if err != nil {
				t.Fatalf("%s empty text required unused font: %v", mode, err)
			}
			checkBoundaryPage(t, data, 300, 200, "")
		}
	}
}

func TestProducedPageLimitReachesFirstProducerWithoutCompletingDocument(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	stopped := errors.New("first producer deliberately stopped")
	for _, count := range []int{1000000, 1000001} {
		calls := 0
		_, err := genpage.WriteProduced(ctx, io.Discard, count, nil, func(index int) (genpage.Page, error) {
			calls++
			if index != 0 {
				t.Fatal("producer advanced beyond its first refusal")
			}
			return genpage.Page{}, stopped
		})
		if count == 1000000 {
			if !errors.Is(err, stopped) || calls != 1 {
				t.Fatalf("accepted count did not reach its first producer: calls=%d err=%v", calls, err)
			}
		} else if !errors.Is(err, genpage.ErrTooManyPages) || calls != 0 {
			t.Fatalf("over-limit count reached a producer: calls=%d err=%v", calls, err)
		}
	}
}
