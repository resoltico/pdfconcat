// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdforacle

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Raster returns one independent Poppler rendering for batch pixel comparisons.
func (d *Document) Raster(number int) (image.Image, error) { return d.render(number) }

// HasMuPDF reports whether the additional independent renderer is installed.
func HasMuPDF() bool { _, err := exec.LookPath("mutool"); return err == nil }

// MuPDFRaster uses MuPDF's fixed PNG/72dpi interface; it does not affect runtime PDF assembly.
func (d *Document) MuPDFRaster(number int) (image.Image, error) {
	program, err := exec.LookPath("mutool")
	if err != nil {
		return nil, fmt.Errorf("find independent MuPDF: %w", err)
	}

	directory, err := os.MkdirTemp("", "pdforacle-mupdf")
	if err != nil {
		return nil, fmt.Errorf("create MuPDF render directory: %w", err)
	}

	img, renderErr := d.muPDFInto(program, directory, number)

	return img, errors.Join(renderErr, os.RemoveAll(directory))
}

func (d *Document) muPDFInto(program, directory string, number int) (image.Image, error) {
	target := filepath.Join(directory, "page.png")

	_, stderr, err := run(program, "draw", "-r", "72", "-o", target, d.Path, strconv.Itoa(number))
	if err != nil {
		return nil, fmt.Errorf("MuPDF page %d: %w: %s", number, err, stderr)
	}
	// The owned output directory prevents arbitrary-file selection by a renderer response.
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open MuPDF output root: %w", err)
	}

	data, readErr := root.ReadFile("page.png")

	closeErr := root.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read MuPDF image: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode MuPDF image: %w", err)
	}

	return img, nil
}
