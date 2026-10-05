// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package app orchestrates PDFConcat assembly runs.
package app

import (
	"context"
	"io"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// Engine is the PDF capability surface the application needs.
type Engine interface {
	// Inspect validates a PDF and returns its page count and first/last page sizes.
	Inspect(ctx context.Context, path string) (assembly.DocumentInfo, error)
	// ValidateBlank reports whether the engine can render the blank.
	ValidateBlank(ctx context.Context, spec assembly.BlankSpec) error
	// Assemble writes the layout as one PDF at destination, using workspace for scratch files.
	Assemble(ctx context.Context, layout assembly.Layout, workspace, destination string) error
}

// Streams are the process streams a run reads and writes.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	// Stderr receives non-fatal warnings; nil discards them.
	Stderr io.Writer
	// Progress receives transient status lines; nil disables progress reporting.
	Progress io.Writer
}

// Runner coordinates assembly through a PDF engine and the process streams.
type Runner struct {
	engine  Engine
	streams Streams
}

// New constructs a Runner.
func New(engine Engine, streams Streams) *Runner {
	return &Runner{engine: engine, streams: streams}
}
