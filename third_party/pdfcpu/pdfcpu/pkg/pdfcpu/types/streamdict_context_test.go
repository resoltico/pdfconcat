/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package types

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
)

type streamCheckpointContext struct {
	context.Context
	remaining atomic.Int64
}

func (c *streamCheckpointContext) Err() error {
	if c.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

func TestContextFullDecodeBoundsCachedRawAndOpaqueResults(t *testing.T) {
	for _, kind := range []string{"cached", "raw", filter.JPX, filter.JBIG2, filter.DCT} {
		for _, limit := range []int64{3, 4} {
			sd := StreamDict{Raw: []byte("1234")}
			if kind == "cached" {
				sd.Content = []byte("1234")
			}
			if kind != "cached" && kind != "raw" {
				sd.FilterPipeline = []PDFFilter{{Name: kind}}
			}
			err := sd.DecodeWithContextAndLimit(t.Context(), limit)
			if limit == 3 {
				if !errors.Is(err, filter.ErrDecodeLimitExceeded) || kind != "cached" && sd.Content != nil {
					t.Fatalf("%s overflow published Content: %v %v", kind, sd.Content, err)
				}
			} else if err != nil || !bytes.Equal(sd.Content, []byte("1234")) {
				t.Fatalf("%s exact cap: %v %v", kind, sd.Content, err)
			}
			if limit == 4 && kind != "cached" && &sd.Content[0] != &sd.Raw[0] {
				t.Fatal("safe alias path needlessly copied raw bytes")
			}
		}
	}
}

func TestContextFullDecodeBoundsFilterChainAndPreservedTerminalStage(t *testing.T) {
	sd := StreamDict{Raw: []byte("31323334>"), FilterPipeline: []PDFFilter{{Name: filter.ASCIIHex}, {Name: filter.JPX}}}
	if err := sd.DecodeWithContextAndLimit(t.Context(), 4); err != nil || !bytes.Equal(sd.Content, []byte("1234")) {
		t.Fatalf("terminal opaque stage lost decoded bytes: %v %v", sd.Content, err)
	}
	sd = StreamDict{Raw: []byte("034141414180>"), FilterPipeline: []PDFFilter{{Name: filter.ASCIIHex}, {Name: filter.RunLength}}}
	if err := sd.DecodeWithContextAndLimit(t.Context(), 4); !errors.Is(err, filter.ErrDecodeLimitExceeded) || sd.Content != nil {
		t.Fatalf("oversized intermediate stage fed a later filter: %v %v", sd.Content, err)
	}
	sd = StreamDict{Raw: []byte("1"), FilterPipeline: []PDFFilter{{Name: filter.JPX}, {Name: filter.ASCIIHex}}}
	if err := sd.DecodeWithContextAndLimit(t.Context(), 4); !errors.Is(err, filter.ErrUnsupportedFilter) || sd.Content != nil {
		t.Fatalf("nonterminal opaque filter silently dropped later stage: %v", err)
	}
}

func TestContextFullDecodeCancelsBufferedFlateWithoutPublishingContent(t *testing.T) {
	var encoded bytes.Buffer
	writer := zlib.NewWriter(&encoded)
	if _, err := writer.Write(bytes.Repeat([]byte("a"), 1<<20)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := &streamCheckpointContext{Context: t.Context()}
	ctx.remaining.Store(15)
	sd := StreamDict{Raw: encoded.Bytes(), FilterPipeline: []PDFFilter{{Name: filter.Flate}}}
	if err := sd.DecodeWithContextAndLimit(ctx, 1<<20); !errors.Is(err, context.Canceled) || sd.Content != nil {
		t.Fatalf("active decode cancellation lost identity or assigned Content: %v %v", sd.Content, err)
	}
	if err := sd.DecodeWithContextAndLimit(t.Context(), 1<<20); err != nil || len(sd.Content) != 1<<20 {
		t.Fatalf("fresh uncancelled retry failed: %d %v", len(sd.Content), err)
	}
}

func TestContextPartialPrefixSemanticsRemainDistinctFromFullCap(t *testing.T) {
	sd := StreamDict{Raw: []byte("123456")}
	data, err := sd.DecodeLengthWithContextAndLimit(t.Context(), 5, 2)
	if err != nil || string(data) != "12345" {
		t.Fatalf("partial prefix limit precedence changed: %q %v", data, err)
	}
	if err = sd.DecodeWithContextAndLimit(t.Context(), 2); !errors.Is(err, filter.ErrDecodeLimitExceeded) {
		t.Fatalf("cached partial decode bypassed later full cap: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = sd.DecodeWithContextAndLimit(ctx, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached result bypassed cancellation: %v", err)
	}
}
