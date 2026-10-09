/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
)

type decodeCheckpointContext struct {
	context.Context
	remaining atomic.Int64
}

func (c *decodeCheckpointContext) Err() error {
	if c.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

type cancelDecodedRead struct {
	reader  io.Reader
	cancel  context.CancelFunc
	largest int
}

func (r *cancelDecodedRead) Read(p []byte) (int, error) {
	r.largest = max(r.largest, len(p))
	n, err := r.reader.Read(p)
	if n > 0 {
		r.cancel()
	}
	return n, err
}

func TestContextDecodedCopyInterruptsBufferedExpansionAndCapsWork(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 1<<20)
	var encoded bytes.Buffer
	writer := zlib.NewWriter(&encoded)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, safe := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		codec, err := zlib.NewReader(ContextReader(ctx, bytes.NewReader(encoded.Bytes())))
		if err != nil {
			t.Fatal(err)
		}
		output := &cancelDecodedRead{reader: codec, cancel: cancel}
		var result *bytes.Buffer
		if safe {
			result, err = (baseFilter{ctx: ctx, maxDecodeBytes: int64(len(payload))}).copyDecoded(output, -1)
		} else {
			result = &bytes.Buffer{}
			_, err = io.Copy(result, output)
		}
		if closeErr := codec.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		cancel()
		if safe {
			if !errors.Is(err, context.Canceled) || result.Len() > DecodeWorkChunk || output.largest > DecodeWorkChunk {
				t.Fatalf("buffered expansion did not stop at decoded work boundary: len=%d largest=%d err=%v", result.Len(), output.largest, err)
			}
		} else if err != nil || result.Len() != len(payload) || ctx.Err() == nil {
			t.Fatalf("raw-only negative control must expand completely after cancellation: %d %v", result.Len(), err)
		}
	}
}

func TestContextCopyNChecksCancellationAtExactReadBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := &cancelDecodedRead{reader: bytes.NewReader([]byte("1234")), cancel: cancel}
	_, err := (baseFilter{ctx: ctx}).copyDecoded(reader, 4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CopyN suppressed terminal cancellation: %v", err)
	}
}

func TestContextPredictorReadFullChecksExactBoundaryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := &cancelDecodedRead{reader: bytes.NewReader([]byte{0, 1, 2, 3}), cancel: cancel}
	_, err := (flate{baseFilter{ctx: ctx, maxDecodeBytes: 3}}).decodePostProcessRows(reader, -1, 4, PredictorNone, 1, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadFull suppressed terminal cancellation: %v", err)
	}
}

func TestContextChecksASCIIAndRunLengthMaterializedScans(t *testing.T) {
	for _, kind := range []string{ASCII85, ASCIIHex, RunLength} {
		ctx := &decodeCheckpointContext{Context: t.Context()}
		ctx.remaining.Store(4)
		codec, err := NewFilterWithContext(ctx, kind, nil, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		data := append(bytes.Repeat([]byte(" "), 1<<20), []byte("z~>")...)
		if kind == ASCIIHex {
			data = append(bytes.Repeat([]byte(" "), 1<<20), []byte("00>")...)
		}
		if kind == RunLength {
			data = bytes.Repeat([]byte{129, 1}, 1<<13)
		}
		_, err = codec.Decode(bytes.NewBuffer(data))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s materialized scan ignored active cancellation: %v", kind, err)
		}
	}
}

func TestContextTIFFChecksLargeInnerColorLoops(t *testing.T) {
	ctx := &decodeCheckpointContext{Context: t.Context()}
	ctx.remaining.Store(4)
	colors := 1 << 18
	_, err := applyHorDiffWithContext(ctx, make([]byte, colors*2), colors)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TIFF inner loop ignored active cancellation: %v", err)
	}
}

type paethMutationContext struct {
	context.Context
	row []byte
}

func (c paethMutationContext) Err() error {
	if c.row[3] != 1 {
		return context.Canceled
	}
	return nil
}

func TestContextPaethWorkCounterChecksStridedInnerLoop(t *testing.T) {
	row := bytes.Repeat([]byte{1}, 1<<20)
	previous := make([]byte, len(row))
	ctx := paethMutationContext{Context: t.Context(), row: row}
	err := filterPaethWithContext(ctx, row, previous, 2)
	if !errors.Is(err, context.Canceled) || row[len(row)-1] != 1 {
		t.Fatalf("strided Paeth column ran through cancellation: err=%v last=%d", err, row[len(row)-1])
	}
}

func TestPredictorExactOutputCapExcludesPNGPrefixAndRejectsNextRowBeforeAppend(t *testing.T) {
	f := flate{baseFilter{ctx: t.Context(), maxDecodeBytes: 3}}
	buffer, err := f.decodePostProcessRows(bytes.NewReader([]byte{0, 1, 2, 3}), -1, 4, PredictorNone, 1, 1)
	if err != nil || !bytes.Equal(buffer.Bytes(), []byte{1, 2, 3}) {
		t.Fatalf("exact output cap wrongly charged row prefix/EOF: %v %v", buffer, err)
	}
	_, err = f.decodePostProcessRows(bytes.NewReader([]byte{0, 1, 2, 3, 0, 4, 5, 6}), -1, 4, PredictorNone, 1, 1)
	if !errors.Is(err, ErrDecodeLimitExceeded) {
		t.Fatalf("next row exceeded remaining output cap: %v", err)
	}
}
