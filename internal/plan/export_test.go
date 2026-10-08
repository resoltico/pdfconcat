// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"context"
	"errors"
	"io"
)

// IntegerFailure classifies why a number is not an exact int64.
type IntegerFailure int

// The classes of IntegerFailure.
const (
	NoFailure IntegerFailure = iota
	NotIntegral
	OutOfRange
)

// ParseExactInt exposes the exact integer conversion to the external tests.
func ParseExactInt(text string) (int64, error) { return exactInt64(text) }

// ClassifyInteger reports why exactInt64 rejects text, or NoFailure.
func ClassifyInteger(text string) IntegerFailure {
	_, err := exactInt64(text)

	switch {
	case err == nil:
		return NoFailure
	case errors.Is(err, errNotIntegral):
		return NotIntegral
	default:
		return OutOfRange
	}
}

// ChunkReaderOver exposes the cancellable chunk reader to the external tests.
func ChunkReaderOver(ctx context.Context, source io.Reader) io.Reader {
	return newChunkReader(cancellationOf(ctx), source)
}

// LimitedSourceOver exposes the reader that feeds the decoder (byte order mark stripped, size limited) to the
// external tests.
func LimitedSourceOver(ctx context.Context, name string, source io.Reader) io.Reader {
	return newLimitedSource(cancellationOf(ctx), name, source)
}

// SaturatedExponent exposes the saturating parse of the digits after the exponent marker.
func SaturatedExponent(tail string) int64 { return scanExponent(tail) }
