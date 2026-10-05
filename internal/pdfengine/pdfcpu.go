// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package pdfengine isolates PDF operations from application/domain code.
// It is the only package that imports the pdfcpu library.
package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// blankFilePermissions restricts generated scratch files to the current user.
const blankFilePermissions = 0o600

// PDFCPU performs PDF inspection and assembly with the embedded pdfcpu library
// using stateless, offline configuration.
type PDFCPU struct {
	conf *model.Configuration
}

// NewPDFCPU constructs the stateless, offline pdfcpu-backed engine.
func NewPDFCPU() (*PDFCPU, error) {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{
		Mode: api.ConfigurationModeStateless,
	})
	if err != nil {
		return nil, fmt.Errorf("load stateless pdfcpu configuration: %w", err)
	}

	conf.ValidationMode = model.ValidationRelaxed
	conf.Offline = true
	conf.CreateBookmarks = false

	return &PDFCPU{conf: conf}, nil
}

// Inspect validates one PDF and returns its page count and the displayed
// size of its first and last pages, in a single read pass.
func (e *PDFCPU) Inspect(ctx context.Context, path string) (assembly.DocumentInfo, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return assembly.DocumentInfo{}, err
	}

	info, inspectErr := e.inspectReader(ctx, file, path)

	closeErr := file.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close %q: %w", path, closeErr)
	}

	joined := errors.Join(inspectErr, closeErr)
	if joined != nil {
		return assembly.DocumentInfo{}, joined
	}

	return info, nil
}

// ValidateBlank reports whether the engine can render the blank.
func (e *PDFCPU) ValidateBlank(ctx context.Context, spec assembly.BlankSpec) error {
	_, err := renderBlank(ctx, spec)

	return err
}

// Assemble writes the layout's parts, in order, as one PDF at destination.
// Distinct generated blanks are rendered once into workspace and reused, and
// all sources are merged in a single pass.
func (e *PDFCPU) Assemble(ctx context.Context, layout assembly.Layout, workspace, destination string) error {
	renderedBlanks := make(map[assembly.BlankSpec]string)
	inputs := make([]string, 0, len(layout.Parts))

	for index := range layout.Parts {
		part := &layout.Parts[index]

		switch part.Kind {
		case assembly.PDF:
			inputs = append(inputs, part.Path)
		case assembly.Blank:
			path, found := renderedBlanks[part.Blank]
			if !found {
				var err error

				path, err = writeBlank(ctx, part.Blank, workspace, len(renderedBlanks))
				if err != nil {
					return err
				}

				renderedBlanks[part.Blank] = path
			}

			for range part.Pages {
				inputs = append(inputs, path)
			}
		default:
			return fmt.Errorf("layout part %d has an unknown kind", index+1)
		}
	}

	err := api.MergeCreateFile(ctx, inputs, destination, false, e.conf)
	if err != nil {
		return fmt.Errorf("merge PDFs: %w", err)
	}

	return nil
}

func (e *PDFCPU) inspectReader(ctx context.Context, file *os.File, path string) (assembly.DocumentInfo, error) {
	conf := e.conf.Clone()
	conf.Cmd = model.VALIDATE

	pdfContext, err := api.ReadAndValidate(ctx, file, conf)
	if err != nil {
		return assembly.DocumentInfo{}, fmt.Errorf("validate %q: %w", path, err)
	}

	pages := pdfContext.PageCount
	if pages <= 0 {
		return assembly.DocumentInfo{}, fmt.Errorf("input %q contains no pages", path)
	}

	boundaries, err := pdfContext.PageBoundaries(ctx, types.IntSet{1: true, pages: true})
	if err != nil {
		return assembly.DocumentInfo{}, fmt.Errorf("read page sizes of %q: %w", path, err)
	}

	first, err := displayedSize(&boundaries[0])
	if err != nil {
		return assembly.DocumentInfo{}, fmt.Errorf("first page of %q: %w", path, err)
	}

	last, err := displayedSize(&boundaries[pages-1])
	if err != nil {
		return assembly.DocumentInfo{}, fmt.Errorf("last page of %q: %w", path, err)
	}

	return assembly.DocumentInfo{Pages: pages, FirstPage: first, LastPage: last}, nil
}

// halfTurn is the rotation, in degrees, that leaves a page's width and height unswapped.
const halfTurn = 180

// displayedSize returns the page MediaBox size with /Rotate applied.
func displayedSize(boundaries *model.PageBoundaries) (assembly.PageDim, error) {
	dim := boundaries.MediaBox().Dimensions()
	if boundaries.Rot%halfTurn != 0 {
		dim.Width, dim.Height = dim.Height, dim.Width
	}

	if dim.Width <= 0 || dim.Height <= 0 {
		return assembly.PageDim{}, errors.New("page has an empty MediaBox")
	}

	return assembly.PageDim{Width: assembly.Length(dim.Width), Height: assembly.Length(dim.Height)}, nil
}

// openRegularFile opens path after checking, both before and after opening, that it is a regular file.
func openRegularFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %q: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input %q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}

	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("input %q is not a regular file", path), file.Close())
	}

	return file, nil
}

func writeBlank(ctx context.Context, spec assembly.BlankSpec, workspace string, ordinal int) (string, error) {
	rendered, err := renderBlank(ctx, spec)
	if err != nil {
		return "", err
	}

	path := filepath.Join(workspace, "blank-"+strconv.Itoa(ordinal)+".pdf")

	err = os.WriteFile(path, rendered, blankFilePermissions)
	if err != nil {
		return "", fmt.Errorf("write generated blank page: %w", err)
	}

	return path, nil
}
