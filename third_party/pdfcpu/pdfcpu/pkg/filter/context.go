/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package filter

import (
	"bytes"
	"context"
	"io"
)

// DecodeWorkChunk bounds one cooperative reader/scanner work interval, not PDF size or memory.
const DecodeWorkChunk = 32 << 10

type contextReader struct {
	reader io.Reader
	ctx    context.Context
}

// ContextReader deliberately exposes only Read: neither WriterTo nor an embedded reader can bypass
// context checks or allow a growing Buffer.ReadFrom request to become unbounded codec work.
func ContextReader(c context.Context, r io.Reader) io.Reader { return contextReader{reader: r, ctx: c} }
func (r contextReader) Read(p []byte) (int, error) {
	if r.ctx != nil && r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	if len(p) > DecodeWorkChunk {
		p = p[:DecodeWorkChunk]
	}
	n, err := r.reader.Read(p)
	if r.ctx != nil && r.ctx.Err() != nil {
		return n, r.ctx.Err()
	}
	return n, err
}

// FullDecodeLimit normalizes the maintained full-decode rule; a negative value remains unlimited.
func FullDecodeLimit(limit int64) int64 {
	if limit == 0 {
		return DefaultMaxDecodeBytes
	}
	return limit
}
func (f baseFilter) contextError() error {
	if f.ctx != nil {
		return f.ctx.Err()
	}
	return nil
}
func (f baseFilter) readerBytes(r io.Reader) ([]byte, error) {
	if err := f.contextError(); err != nil {
		return nil, err
	}
	// Keep the original bytes.Buffer alias rather than wrapping it into a materializing copy.
	if _, buffer := r.(*bytes.Buffer); !buffer {
		r = ContextReader(f.ctx, r)
	}
	data, err := getReaderBytes(r)
	if err != nil {
		return nil, err
	}
	if err = f.contextError(); err != nil {
		return nil, err
	}
	return data, nil
}

// decodeContextWriter prevents oversized materialized results before append and checks encoder writes.
type decodeContextWriter struct {
	writer    io.Writer
	filter    baseFilter
	remaining int64
}

func (w *decodeContextWriter) Write(p []byte) (int, error) {
	if err := w.filter.contextError(); err != nil {
		return 0, err
	}
	if w.remaining >= 0 && int64(len(p)) > w.remaining {
		return 0, ErrDecodeLimitExceeded
	}
	total := 0
	for len(p) > 0 {
		chunk := p[:min(len(p), DecodeWorkChunk)]
		n, err := w.writer.Write(chunk)
		total += n
		if w.remaining >= 0 {
			w.remaining -= int64(n)
		}
		if err != nil {
			return total, err
		}
		if cancelErr := w.filter.contextError(); cancelErr != nil {
			return total, cancelErr
		}
		if n != len(chunk) {
			return total, io.ErrShortWrite
		}
		p = p[n:]
	}
	return total, nil
}
