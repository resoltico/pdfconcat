// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Ervins Strauhmanis

package filters

import (
	"context"
	"errors"
	"io"
)

// SkipBounded delegates EOD recognition to the maintained filter skipper while bounding
// encoded consumption, decoded discard work, decoder allocations and cancellation.
func SkipBounded(ctx context.Context, name string, params map[string]int, src io.Reader, encodedLimit, decodedLimit, dimensionLimit int) (int, int, error) {
	if ctx == nil || encodedLimit <= 0 || decodedLimit <= 0 || dimensionLimit <= 0 {
		return 0, 0, errors.New("invalid inline filter limits")
	}
	switch name {
	case "AHx":
		name = ASCIIHex
	case "A85":
		name = ASCII85
	case "LZW":
		name = LZW
	case "Fl":
		name = Flate
	case "RL":
		name = RunLength
	case "CCF":
		name = CCITTFax
	case "DCT":
		name = DCT
	}
	if name == CCITTFax {
		columns := 1728
		if n, ok := params["Columns"]; ok {
			columns = n
		}
		rows := params["Rows"]
		if columns <= 0 || columns > dimensionLimit || rows < 0 || rows > dimensionLimit {
			return 0, 0, errors.New("CCITT dimension limit exceeded")
		}
	}
	s, err := SkipperFromFilter(name, params)
	if err != nil {
		return 0, 0, err
	}
	input := &boundedInput{ctx: ctx, src: src, remaining: encodedLimit, decoded: decodedLimit}
	encoded, err := s.Skip(input)
	return encoded, input.discarded, err
}

type boundedInput struct {
	ctx                context.Context
	src                io.Reader
	remaining, decoded int
	discarded          int
}

func (r *boundedInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining <= 0 {
		return 0, errors.New("inline encoded byte limit exceeded")
	}
	if len(p) > 1024 {
		p = p[:1024]
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.src.Read(p)
	r.remaining -= n
	return n, err
}
func discardSkipped(decoded io.Reader, encoded io.Reader) error {
	if bounded, ok := encoded.(*boundedInput); ok {
		n, err := io.Copy(io.Discard, io.LimitReader(&cancelReader{ctx: bounded.ctx, src: decoded}, int64(bounded.decoded)+1))
		bounded.discarded = int(n)
		if err != nil {
			return err
		}
		if n > int64(bounded.decoded) {
			return errors.New("inline decoded byte limit exceeded")
		}
		return nil
	}
	_, err := io.Copy(io.Discard, decoded)
	return err
}

type cancelReader struct {
	ctx context.Context
	src io.Reader
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 1024 {
		p = p[:1024]
	}
	return r.src.Read(p)
}

type discardByteWriter struct {
	remaining int
	bounded   bool
}

func (w *discardByteWriter) WriteByte(byte) error {
	if w.bounded {
		if w.remaining <= 0 {
			return errors.New("inline decoded byte limit exceeded")
		}
		w.remaining--
	}
	return nil
}
