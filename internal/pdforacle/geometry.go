// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdforacle

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
)

const channel8To16 = 257

var (
	// errRenderOutput reports a rendering that did not produce exactly one image.
	errRenderOutput = errors.New("pdftoppm did not write exactly one image")

	// errPixelOutside reports a pixel coordinate outside the rendered page.
	errPixelOutside = errors.New("pixel is outside the rendering")

	// errPixelModel reports a pixel that cannot be converted to 8-bit RGB.
	errPixelModel = errors.New("pixel is not convertible to RGBA")
)

// render rasterizes page number (1-based) at 72 dpi with pdftoppm, honoring the CropBox (clipped to the
// MediaBox) and the page rotation. A pixel is a point; poppler rounds sizes up and ignores UserUnit.
func (d *Document) render(number int) (image.Image, error) {
	dir, err := os.MkdirTemp("", "pdforacle")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}

	img, renderErr := d.renderInto(dir, number)

	return img, errors.Join(renderErr, os.RemoveAll(dir))
}

// renderInto rasterizes page number into dir and decodes the image.
func (d *Document) renderInto(dir string, number int) (image.Image, error) {
	prefix := filepath.Join(dir, "page")
	pageArg := strconv.Itoa(number)

	_, stderr, err := run(d.tools.PDFToPPM, "-r", "72", "-cropbox", "-f", pageArg, "-l", pageArg, "-png", d.Path, prefix)
	if err != nil {
		return nil, fmt.Errorf("pdftoppm page %d: %w: %s", number, err, stderr)
	}

	files, err := filepath.Glob(prefix + "*.png")
	if err != nil {
		return nil, fmt.Errorf("find rendering of page %d: %w", number, err)
	}

	if len(files) != 1 {
		return nil, fmt.Errorf("%w: page %d wrote %d", errRenderOutput, number, len(files))
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		return nil, fmt.Errorf("read rendering: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode rendering: %w", err)
	}

	return img, nil
}

// RenderedSize returns the pixel width and height of page number (1-based) rendered at 72 dpi with the
// CropBox and rotation applied. Poppler rounds sizes up and does not apply UserUnit; multiply by UserUnit
// for the physical size.
func (d *Document) RenderedSize(number int) (int, int, error) {
	img, err := d.render(number)
	if err != nil {
		return 0, 0, err
	}

	bounds := img.Bounds()

	return bounds.Dx(), bounds.Dy(), nil
}

// PixelColor returns the 8-bit red, green and blue of the pixel at (x, y), counted from the top-left
// corner, in the 72 dpi rendering of page number (1-based).
func (d *Document) PixelColor(number, x, y int) ([3]uint8, error) {
	img, err := d.render(number)
	if err != nil {
		return [3]uint8{}, err
	}

	if !image.Pt(x, y).In(img.Bounds()) {
		return [3]uint8{}, fmt.Errorf("%w: (%d, %d) in %v", errPixelOutside, x, y, img.Bounds())
	}

	pixel, isRGBA := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	if !isRGBA {
		return [3]uint8{}, fmt.Errorf("%w: pixel (%d, %d)", errPixelModel, x, y)
	}

	return [3]uint8{pixel.R, pixel.G, pixel.B}, nil
}

// AppearanceDifference compares independent full-page rasterizations. The result is the fraction
// of pixels with a channel differing by more than tolerance; positions and image marks matter.
func (d *Document) AppearanceDifference(page int, source *Document, sourcePage int, tolerance uint8) (float64, error) {
	actual, err := d.render(page)
	if err != nil {
		return 0, err
	}

	expected, err := source.render(sourcePage)
	if err != nil {
		return 0, err
	}

	if actual.Bounds() != expected.Bounds() {
		return 1, nil
	}

	bounds := actual.Bounds()
	mismatches := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ar, ag, ab, _ := actual.At(x, y).RGBA()

			er, eg, eb, _ := expected.At(x, y).RGBA()
			limit := uint32(tolerance) * channel8To16
			different := channelDifference(ar, er) > limit || channelDifference(ag, eg) > limit

			different = different || channelDifference(ab, eb) > limit
			if different {
				mismatches++
			}
		}
	}

	return float64(mismatches) / float64(bounds.Dx()*bounds.Dy()), nil
}

func channelDifference(a, b uint32) uint32 {
	if a > b {
		return a - b
	}

	return b - a
}

// UserUnit returns the /UserUnit of page number (1-based), or 1 when the page has none.
func (d *Document) UserUnit(number int) float64 {
	if number < 1 || number > len(d.pages) {
		return 0
	}

	if units, isNumber := d.deref(d.dict(d.pages[number-1])["/UserUnit"]).(float64); isNumber {
		return units
	}

	return 1
}
