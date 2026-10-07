// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package genpage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// bitmap is a rendered page: 8-bit gray pixels, or interleaved RGB when read from a PPM file.
	bitmap struct {
		pix  []byte
		w, h int
	}

	// extent is the box of pixels that carry ink, both corners included.
	extent struct {
		left, top, right, bottom int
	}
)

// run runs an independent QA tool and returns its standard output.
const (
	qpdfTool          = "qpdf"
	pdfToPPMTool      = "pdftoppm"
	firstPageText     = "one"
	secondPageText    = "two"
	fontFileReference = "/FontFile2"
)

func run(tb testing.TB, tool string, args ...string) []byte {
	tb.Helper()

	tools := pdforacle.RequireTools(tb)
	programs := map[string]string{qpdfTool: tools.QPDF, "pdftotext": tools.PDFToText, pdfToPPMTool: tools.PDFToPPM}

	program, known := programs[tool]
	if !known {
		tb.Fatalf("unknown QA tool %q", tool)
	}

	var stderr bytes.Buffer

	command := exectest.Command(tb.Context(), program, args...)
	command.Stderr = &stderr

	out, err := command.Output()
	if err != nil {
		tb.Fatalf("%s %s: %v\n%s", tool, strings.Join(args, " "), err, stderr.String())
	}

	return out
}

func defaultFont(tb testing.TB) *typeset.Font {
	tb.Helper()

	font, err := typeset.LoadDefaultFont()
	if err != nil {
		tb.Fatal(err)
	}

	return font
}

func params(text string) typeset.Params {
	return typeset.Params{
		Text: text, Size: 20, PageWidth: 300, PageHeight: 200,
		Anchor: typeset.AnchorTopLeft, OffsetX: 10, OffsetY: -10, LineSpacing: 1,
	}
}

func placed(tb testing.TB, font *typeset.Font, spec typeset.Params) *typeset.Placed {
	tb.Helper()

	var shaper typeset.Shaper

	result, err := shaper.Place(font, spec)
	if err != nil {
		tb.Fatal(err)
	}

	return result
}

func textPage(tb testing.TB, font *typeset.Font, text string) genpage.Page {
	tb.Helper()

	return genpage.Page{Width: 300, Height: 200, Text: placed(tb, font, params(text))}
}

// writePDF writes the pages to a file of a private directory and returns the file's path and bytes.
func writePDF(tb testing.TB, pages []genpage.Page) (string, []byte) {
	tb.Helper()

	var buffer bytes.Buffer

	count, err := genpage.Write(context.Background(), &buffer, pages)
	if err != nil {
		tb.Fatal(err)
	}

	if count != int64(buffer.Len()) {
		tb.Fatalf("reported %d bytes, wrote %d", count, buffer.Len())
	}

	path := filepath.Join(tb.TempDir(), "resource.pdf")

	err = os.WriteFile(path, buffer.Bytes(), 0o600)
	if err != nil {
		tb.Fatal(err)
	}

	return path, buffer.Bytes()
}

// qpdfCheck fails the test unless qpdf accepts the file, and returns its page count.
func qpdfCheck(tb testing.TB, path string) int {
	tb.Helper()

	out := run(tb, qpdfTool, "--check", path)
	if !bytes.Contains(out, []byte("No syntax or stream encoding errors found")) {
		tb.Fatalf("qpdf --check: %s", out)
	}

	pages, err := strconv.Atoi(strings.TrimSpace(string(run(tb, qpdfTool, "--show-npages", path))))
	if err != nil {
		tb.Fatal(err)
	}

	return pages
}

// extractRaw returns pdftotext -raw output split into pages.
func extractRaw(tb testing.TB, path string) []string {
	tb.Helper()

	out := string(run(tb, "pdftotext", "-raw", "-enc", "UTF-8", path, "-"))
	pages := strings.Split(out, "\f")

	return pages[:len(pages)-1]
}

// renderGray rasterizes page 1 at 288 dpi with pdftoppm.
func renderGray(tb testing.TB, path string) bitmap {
	tb.Helper()

	root := filepath.Join(tb.TempDir(), "page")
	run(tb, pdfToPPMTool, "-gray", "-r", "288", "-f", "1", "-l", "1", "-singlefile", path, root)

	return readPNM(tb, root+".pgm")
}

// renderRGB rasterizes page 1 as 8-bit RGB.
func renderRGB(tb testing.TB, path string, dpi int) bitmap {
	tb.Helper()

	root := filepath.Join(tb.TempDir(), "page")
	run(tb, pdfToPPMTool, "-r", strconv.Itoa(dpi), "-f", "1", "-l", "1", "-singlefile", path, root)

	return readPNM(tb, root+".ppm")
}

// readPNM reads a file that pdftoppm wrote into the test's own temporary directory.
func readPNM(tb testing.TB, path string) bitmap {
	tb.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		tb.Fatal(err)
	}

	fields := strings.Fields(string(data[:min(len(data), 40)]))
	if len(fields) < 4 || fields[3] != "255" {
		tb.Fatalf("bad PNM header %q", data[:20])
	}

	magic := fields[0]

	width, err := strconv.Atoi(fields[1])
	if err != nil {
		tb.Fatal(err)
	}

	height, err := strconv.Atoi(fields[2])
	if err != nil {
		tb.Fatal(err)
	}

	// The single whitespace byte after maxval precedes the pixels.
	pix := data[len(data)-width*height*map[string]int{"P5": 1, "P6": 3}[magic]:]

	return bitmap{w: width, h: height, pix: pix}
}

// ink returns the box of gray pixels darker than threshold, and whether there are any.
func (b bitmap) ink(threshold byte) (extent, bool) {
	box := extent{left: b.w, top: b.h, right: -1, bottom: -1}
	found := false

	for y := range b.h {
		for x := range b.w {
			if b.pix[y*b.w+x] < threshold {
				box = extent{left: min(box.left, x), top: min(box.top, y), right: max(box.right, x), bottom: max(box.bottom, y)}
				found = true
			}
		}
	}

	return box, found
}

func (b bitmap) sumAbsDiff(other bitmap) int {
	if b.w != other.w || b.h != other.h {
		return 1 << 60
	}

	total := 0

	for index := range b.pix {
		difference := int(b.pix[index]) - int(other.pix[index])
		total += max(difference, -difference)
	}

	return total
}
