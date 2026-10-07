// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	// cancelCheckInterval is how many page clones pass between context checks while compiling a long
	// repeated run, so cancellation stays prompt without a check per page.
	cancelCheckInterval = 4096

	// outputPermissions is the mode requested for the assembled PDF; the process umask narrows it, so the
	// published deliverable has the permissions an ordinary tool would give it. The file is staged inside the
	// job's private (0700) workspace, so it is unreadable by others until it is published.
	outputPermissions = 0o666
)

var errOutputPageCount = errors.New("the output page count differs from the request")

// Assemble writes the request's pages, in order, as one PDF at req.Destination and verifies the result.
//
// The sequence, each step once per call:
//
//  1. validate the request and enforce the source policy (no file is read);
//  2. import the resource document and the source documents into one pool, merging their object
//     graphs. A source with page-local objects, forms or named destinations is imported anew for every
//     run that uses it; every other source is imported once and its page dictionaries are cloned for
//     repeated pages while all other objects stay shared;
//  3. reorder the pool's page tree in place to the compiled order and reduce the catalog to the
//     entries the policy keeps;
//  4. optimize (merging duplicate fonts and images) and write the file once;
//  5. validate the written file and compare its page count with req.ExpectedPages.
//
// The pool holds every imported object graph in memory until the single write, so memory grows with
// the total size of the imported documents, not with the number of output pages; there is no bounded
// or streaming mode. The growing output is never rewritten.
//
// The context is checked between imports, while repeated pages are compiled, and before the write.
// Failures are *Error values with stable codes naming the source where one is responsible.
func (e *Engine) Assemble(ctx context.Context, req *AssembleRequest) error {
	if req != nil {
		req.OutputDigest = ""
		req.writtenDigest = ""
	}

	if err := req.check(); err != nil {
		return err
	}

	err := guard(CodeAssemblyFailed, req.Destination, func() error { return e.writePool(ctx, req) })
	if err != nil {
		return err
	}

	err = guard(CodeOutputInvalid, req.Destination, func() error { return e.verifyOutput(ctx, req) })
	if err == nil {
		req.OutputDigest = req.writtenDigest
	}

	return err
}

// writePool builds the pool, reorders it, and writes req.Destination. The pool is garbage once it returns.
func (e *Engine) writePool(ctx context.Context, req *AssembleRequest) error {
	built := pool{engine: e}

	final, err := built.compile(ctx, req)
	if err != nil {
		return err
	}

	if reorderErr := built.reorder(final); reorderErr != nil {
		return assemblyError(ctx, NoSource, req.Destination, reorderErr)
	}

	if cancelErr := canceled(ctx, NoSource, req.Destination); cancelErr != nil {
		return cancelErr
	}

	digest, writeErr := built.write(ctx, req.Destination)
	if writeErr != nil {
		return assemblyError(ctx, NoSource, req.Destination, writeErr)
	}

	req.writtenDigest = digest

	return nil
}

// compile imports every document the order needs and returns the output pages in order.
func (p *pool) compile(ctx context.Context, req *AssembleRequest) ([]*poolPage, error) {
	generated, err := p.importGenerated(ctx, req)
	if err != nil {
		return nil, err
	}

	whole := make(map[int][]*poolPage)
	final := make([]*poolPage, 0, req.ExpectedPages)

	for _, run := range req.Order {
		var pages []*poolPage

		if run.Generated {
			pages = generated[run.Index : run.Index+1]
		} else {
			pages, err = p.sourcePages(ctx, req, run, whole)
			if err != nil {
				return nil, err
			}
		}

		final, err = p.place(ctx, req.Destination, final, pages, run.Count)
		if err != nil {
			return nil, err
		}
	}

	return final, nil
}

// importGenerated imports the resource document when the order uses it, and returns its pages.
func (p *pool) importGenerated(ctx context.Context, req *AssembleRequest) ([]*poolPage, error) {
	if !slices.ContainsFunc(req.Order, func(run Run) bool { return run.Generated }) {
		return nil, nil
	}

	return p.importDocument(ctx, NoSource, req.Resource.Path, req.Resource.Pages)
}

// sourcePages returns the pool pages a source run draws from. A source is imported once and shared by its
// runs unless it must be imported anew for every occurrence; whole caches the shared imports by source index.
func (p *pool) sourcePages(ctx context.Context, req *AssembleRequest, run Run, whole map[int][]*poolPage) ([]*poolPage, error) {
	source := &req.Sources[run.Index]

	imported, seen := whole[run.Index]
	if !seen || source.Info.ImportPerOccurrence() {
		var err error

		imported, err = p.importDocument(ctx, run.Index, source.Path, source.Info.Pages)
		if err != nil {
			return nil, err
		}

		whole[run.Index] = imported
	}

	return imported[run.Start-1 : run.Start-1+run.Count], nil
}

// place appends count output pages drawn from pages to final. A generated run repeats its one page
// count times; a source run uses its count pages once each.
func (p *pool) place(ctx context.Context, destination string, final, pages []*poolPage, count int) ([]*poolPage, error) {
	for repeat := range count {
		if (len(final)+1)%cancelCheckInterval == 0 {
			if err := canceled(ctx, NoSource, destination); err != nil {
				return nil, err
			}
		}

		final = append(final, p.use(pages[repeat%len(pages)]))
	}

	return final, nil
}

// verifyOutput validates the written file with pdfcpu, inspects it like a source (page tree, first and
// last page geometry), and compares its page count with the request.
func (e *Engine) verifyOutput(ctx context.Context, req *AssembleRequest) error {
	output, err := e.readContext(ctx, NoSource, req.Destination)
	if err != nil {
		return asOutputFailure(err)
	}

	info, err := inspectContext(ctx, output, req.Destination)
	if err != nil {
		return asOutputFailure(err)
	}

	if info.Pages != req.ExpectedPages {
		return newError(CodeOutputPageCount, NoSource, req.Destination,
			fmt.Errorf("%w: %d pages, expected %d", errOutputPageCount, info.Pages, req.ExpectedPages))
	}

	return nil
}

// asOutputFailure reports a document failure found in the written output as CodeOutputInvalid. File access
// and cancellation failures keep their codes.
func asOutputFailure(err error) error {
	failure, found := errors.AsType[*Error](err)
	if !found {
		return err
	}

	if failure.Code == CodeUnreadable || failure.Code == CodeCanceled {
		return failure
	}

	return newError(CodeOutputInvalid, NoSource, failure.Path, failure.Err)
}

// write optimizes the pool and writes it to path in one pass.
func (p *pool) write(ctx context.Context, path string) (string, error) {
	if err := api.OptimizeContext(ctx, p.pdf); err != nil {
		return "", fmt.Errorf("optimize: %w", err)
	}

	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, outputPermissions)
	if err != nil {
		return "", fmt.Errorf("create output: %w", err)
	}

	digest := sha256.New()
	writeErr := api.WriteContext(ctx, p.pdf, io.MultiWriter(file, digest))
	closeErr := file.Close()

	return hex.EncodeToString(digest.Sum(nil)), errors.Join(writeErr, closeErr)
}
