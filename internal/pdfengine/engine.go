// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/resoltico/pdfconcat/internal/capture"
)

// Engine performs PDF inspection and assembly with the embedded pdfcpu library, using stateless,
// offline configuration. It is safe for concurrent use: every operation works on a clone of its
// configuration.
type Engine struct {
	conf *model.Configuration
}

var (
	errEncryptedPDF   = errors.New("the PDF is encrypted; encrypted sources are not supported")
	errLibraryFailure = errors.New("the PDF library failed unexpectedly")
)

// New constructs an Engine.
//
// The configuration is stateless (no pdfcpu configuration directory is read or written), offline, and
// relaxed in validation. Merge bookmark creation is off, and the command mode is MERGECREATE, which
// makes pdfcpu reject encrypted files at read time, before decrypting anything.
func New() (*Engine, error) {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		return nil, fmt.Errorf("load stateless pdfcpu configuration: %w", err)
	}

	conf.ValidationMode = model.ValidationRelaxed
	conf.Offline = true
	conf.CreateBookmarks = false
	conf.Cmd = model.MERGECREATE

	return &Engine{conf: conf}, nil
}

// readContext opens path, reads and validates it, and returns the pdfcpu context. The file is closed
// before returning; the context owns its data in memory.
func (e *Engine) readContext(ctx context.Context, source int, path string) (*model.Context, error) {
	if err := canceled(ctx, source, path); err != nil {
		return nil, err
	}
	// The path names a captured snapshot or a file the user chose to read; reading it is the purpose of the call.
	file, err := capture.OpenRegular(path)
	if err != nil {
		return nil, newError(CodeUnreadable, source, path, err)
	}

	pdfContext, readErr := e.readFrom(ctx, file)
	closeErr := file.Close()

	if readErr != nil {
		return nil, classifyRead(ctx, source, path, readErr)
	}

	if closeErr != nil {
		return nil, newError(CodeUnreadable, source, path, closeErr)
	}

	if renderingErr := checkRenderingState(pdfContext); renderingErr != nil {
		return nil, newError(CodeUnsupportedRendering, source, path, renderingErr)
	}

	return pdfContext, nil
}

func (e *Engine) readFrom(ctx context.Context, reader io.ReadSeeker) (*model.Context, error) {
	pdfContext, err := api.ReadContext(ctx, reader, e.conf.Clone())
	if err != nil {
		return nil, fmt.Errorf("read context: %w", err)
	}
	// Validation repairs some dictionaries, including deleting XFA-only AcroForms with no static
	// fields. Capability rejection must see the original catalog before those repairs.
	if _, catalogErr := pdfContext.Catalog(); catalogErr != nil {
		return nil, fmt.Errorf("catalog: %w", catalogErr)
	}

	if renderingErr := checkRenderingState(pdfContext); renderingErr != nil {
		return nil, renderingErr
	}

	if validationErr := api.ValidateContext(ctx, pdfContext); validationErr != nil {
		return nil, fmt.Errorf("validate context: %w", validationErr)
	}

	return pdfContext, nil
}

// classifyRead maps a pdfcpu read failure to the engine's codes.
func classifyRead(ctx context.Context, source int, path string, err error) *Error {
	switch {
	case ctx.Err() != nil:
		return newError(CodeCanceled, source, path, ctx.Err())
	case errors.Is(err, pdfcpu.ErrEncrypted), errors.Is(err, pdfcpu.ErrWrongPassword):
		return newError(CodeEncrypted, source, path, errEncryptedPDF)
	case errors.Is(err, errRenderingState):
		return newError(CodeUnsupportedRendering, source, path, err)
	default:
		return newError(CodeInvalid, source, path, err)
	}
}

// canceled returns a CodeCanceled error when ctx is done, and nil otherwise.
func canceled(ctx context.Context, source int, path string) error {
	if err := ctx.Err(); err != nil {
		return newError(CodeCanceled, source, path, err)
	}

	return nil
}

// guard runs fn and converts a panic inside the PDF library into a failure with the given code.
// pdfcpu parses hostile files and is known to dereference nil on some malformed inputs (its own
// recovery re-panics); a panic must never reach the caller.
func guard(code Code, path string, fn func() error) error {
	var result error

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = newError(code, NoSource, path, fmt.Errorf(causeDetailFormat, errLibraryFailure, recovered))
			}
		}()

		result = fn()
	}()

	return result
}
