// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

type (
	foreignContextReader struct {
		cancellation func() error
		source       io.Reader
	}
	foreignDecodedStream struct {
		cancellation func() error
		source       io.Reader
		consumed     int64
		tail         [foreignTarFooterBytes]byte
	}
)

const (
	foreignTarBlockBytes  = 512
	foreignTarFooterBytes = 2 * foreignTarBlockBytes
)

func (reader *foreignContextReader) Read(data []byte) (int, error) {
	if err := reader.cancellation(); err != nil {
		return 0, fmt.Errorf("foreign input canceled: %w", err)
	}

	n, err := reader.source.Read(data)
	if cancellation := reader.cancellation(); cancellation != nil {
		return n, errors.Join(err, fmt.Errorf("foreign input canceled: %w", cancellation))
	}

	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("read foreign input stream: %w", err)
	}

	return n, err
}

func (stream *foreignDecodedStream) Read(data []byte) (int, error) {
	if err := stream.cancellation(); err != nil {
		return 0, fmt.Errorf("foreign archive canceled: %w", err)
	}

	remaining := maxForeignTestDecoded - stream.consumed
	if remaining < 0 {
		return 0, fmt.Errorf("%w: foreign decoded archive exceeds bound", errGate)
	}

	data = data[:min(int64(len(data)), remaining+1)]

	count, err := stream.source.Read(data)
	if count >= foreignTarFooterBytes {
		copy(stream.tail[:], data[count-foreignTarFooterBytes:count])
	} else {
		copy(stream.tail[:], stream.tail[count:])
		copy(stream.tail[foreignTarFooterBytes-count:], data[:count])
	}

	stream.consumed += int64(count)
	if stream.consumed > maxForeignTestDecoded {
		return count, fmt.Errorf("%w: foreign decoded archive exceeds bound", errGate)
	}

	if cancellation := stream.cancellation(); cancellation != nil {
		return count, errors.Join(err, fmt.Errorf("foreign archive canceled: %w", cancellation))
	}

	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("read foreign input stream: %w", err)
	}

	return count, err
}

// One opened descriptor and one compressed digest own the entire observed stream.
// gzip must stop at its first member; raw EOF rules out even an empty second member.
func validatedForeignStream(
	ctx context.Context,
	artifact, pin string,
	visit func(*tar.Reader, *foreignDecodedStream) error,
) (result error) {
	root, err := os.OpenRoot(filepath.Dir(artifact))
	if err != nil {
		return fmt.Errorf("open fixture archive directory: %w", err)
	}
	defer closeLogged(root)

	file, err := root.Open(filepath.Base(artifact))
	if err != nil {
		return fmt.Errorf("open fixture archive: %w", err)
	}

	defer func() { result = errors.Join(result, file.Close()) }()

	if shapeErr := validateOpenedForeignArtifact(file); shapeErr != nil {
		return shapeErr
	}

	digest := sha256.New()
	compressed := &io.LimitedReader{R: file, N: maxForeignTestArchive + 1}
	raw := bufio.NewReader(&foreignContextReader{cancellation: ctx.Err, source: io.TeeReader(compressed, digest)})

	decoded, err := gzip.NewReader(raw)
	if err != nil {
		return fmt.Errorf("open fixture gzip: %w", err)
	}

	defer func() { result = errors.Join(result, decoded.Close()) }()

	decoded.Multistream(false)

	stream := &foreignDecodedStream{cancellation: ctx.Err, source: decoded}
	if visitErr := visit(tar.NewReader(stream), stream); visitErr != nil {
		return visitErr
	}

	if paddingErr := stream.finishPadding(); paddingErr != nil {
		return paddingErr
	}

	if endErr := validateForeignCompressedEnd(raw, compressed, digest, pin); endErr != nil {
		return endErr
	}

	if err = ctx.Err(); err != nil {
		return fmt.Errorf("fixture handoff canceled: %w", err)
	}

	return nil
}

func (stream *foreignDecodedStream) finishPadding() error {
	var buffer [foreignTarFooterBytes]byte
	for {
		n, err := stream.Read(buffer[:])
		for _, value := range buffer[:n] {
			if value != 0 {
				return fmt.Errorf("%w: nonzero decoded data after tar footer", errGate)
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("finish fixture gzip/trailer: %w", err)
		}
	}
}

func (stream *foreignDecodedStream) footer(before, padding int64) error {
	if stream.consumed-before != padding+foreignTarFooterBytes {
		return fmt.Errorf("%w: missing/dangling tar footer boundary", errGate)
	}

	for _, value := range &stream.tail {
		if value != 0 {
			return fmt.Errorf("%w: nonzero tar footer", errGate)
		}
	}

	return nil
}

func validateOpenedForeignArtifact(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened fixture archive: %w", err)
	}

	if !info.Mode().IsRegular() || info.Size() > maxForeignTestArchive {
		return fmt.Errorf("%w: fixture archive is not a bounded regular descriptor", errGate)
	}

	return nil
}

func validateForeignCompressedEnd(raw *bufio.Reader, compressed *io.LimitedReader, digest hash.Hash, pin string) error {
	_, err := raw.ReadByte()
	if err == nil {
		return fmt.Errorf("%w: fixture archive has extra gzip/raw content", errGate)
	}

	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("finish compressed fixture stream: %w", err)
	}

	if compressed.N <= 0 || hex.EncodeToString(digest.Sum(nil)) != pin {
		return fmt.Errorf("%w: observed compressed fixture stream differs from pin/bound", errGate)
	}

	return nil
}
