// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

type (
	// cancellation is the part of a context the decoder needs: a channel that closes and the reason.
	cancellation struct {
		done <-chan struct{}
		err  func() error
	}

	// chunkReader owns the underlying reader on a dedicated goroutine so that a Read blocked in the kernel
	// (an open stdin pipe) never blocks the consumer: Read returns as soon as the context is done. After
	// cancellation the goroutine is abandoned until its pending Read returns or the process exits, and the
	// underlying reader must not be used again.
	chunkReader struct {
		source    io.Reader
		stop      cancellation
		full      chan chunk
		free      chan []byte
		err       error
		buffer    []byte // buffer currently being drained
		remaining []byte
		startOnce sync.Once
	}

	chunk struct {
		err    error
		buffer []byte
		length int
	}

	// limitedSource feeds the decoder: cancellable, size-limited, byte-order-mark-stripped, and retaining
	// the original bytes in doc for provenance.
	limitedSource struct {
		reader  io.Reader
		err     error
		stop    cancellation
		doc     *document
		pending []byte // bytes read while looking for the byte order mark
		started bool
	}
)

const (
	// MaxPlanBytes is the plan byte limit, a leading byte order mark included.
	MaxPlanBytes = 64 << 20

	readChunkBytes = 64 << 10
	chunkBuffers   = 2
	bomLength      = 3
	// initialNodes is the arena capacity reserved before the first item is read.
	initialNodes = 1024
)

func cancellationOf(ctx context.Context) cancellation {
	return cancellation{done: ctx.Done(), err: ctx.Err}
}

func newChunkReader(stop cancellation, source io.Reader) *chunkReader {
	reader := &chunkReader{stop: stop, source: source, full: make(chan chunk, 1), free: make(chan []byte, chunkBuffers)}
	for range chunkBuffers {
		reader.free <- make([]byte, readChunkBytes)
	}

	return reader
}

// Read copies pumped bytes, or reports the context's error once it is done.
func (r *chunkReader) Read(destination []byte) (int, error) {
	r.startOnce.Do(func() { go r.pump() })

	for {
		err := r.stop.err()
		if err != nil {
			return 0, err
		}

		if len(r.remaining) > 0 {
			copied := copy(destination, r.remaining)

			r.remaining = r.remaining[copied:]
			if len(r.remaining) == 0 {
				r.free <- r.buffer
			}

			return copied, nil
		}

		if r.err != nil {
			return 0, r.err
		}

		select {
		case received := <-r.full:
			r.buffer, r.remaining, r.err = received.buffer, received.buffer[:received.length], received.err
			if received.length == 0 {
				r.free <- received.buffer
			}
		case <-r.stop.done:
			return 0, r.stop.err()
		}
	}
}

func (r *chunkReader) pump() {
	for {
		var buffer []byte

		select {
		case buffer = <-r.free:
		case <-r.stop.done:
			return
		}

		length, err := r.source.Read(buffer)

		select {
		case r.full <- chunk{buffer: buffer, length: length, err: err}:
		case <-r.stop.done:
			return
		}

		if err != nil {
			return
		}
	}
}

func newLimitedSource(stop cancellation, name string, reader io.Reader) *limitedSource {
	return &limitedSource{
		stop:   stop,
		reader: newChunkReader(stop, reader),
		doc:    &document{name: name, nodes: make([]node, 1, initialNodes)},
	}
}

// Read returns the plan bytes after the byte order mark.
func (s *limitedSource) Read(destination []byte) (int, error) {
	if !s.started {
		s.started = true

		err := s.sniffByteOrderMark()
		if err != nil {
			return 0, err
		}
	}

	if len(s.pending) > 0 {
		copied := copy(destination, s.pending)
		s.pending = s.pending[copied:]

		return copied, nil
	}

	return s.readRaw(destination)
}

// fail converts a read error into the plan diagnostic and remembers it.
func (s *limitedSource) fail(err error) error {
	location := assembly.Location{Source: s.doc.name, Offset: int64(len(s.doc.data))}

	cause := s.stop.err()
	if cause != nil {
		s.err = &Error{
			Code:     CodeInterrupted,
			Stage:    StageRead,
			Location: location,
			Message:  "interrupted while reading the plan",
			Err:      cause,
		}
	} else {
		s.err = &Error{
			Code:     CodeReadFailed,
			Stage:    StageRead,
			Location: location,
			Message:  "cannot read the plan: " + err.Error(),
			Err:      err,
		}
	}

	return s.err
}

// readRaw reads, retains, and counts bytes. The decoder compares the end of input with ==, so [io.EOF] is
// returned as itself; every other failure becomes a located *Error.
func (s *limitedSource) readRaw(destination []byte) (int, error) {
	length, err := s.reader.Read(destination)
	if length > 0 {
		room := MaxPlanBytes - len(s.doc.data)
		if length > room {
			s.doc.data = append(s.doc.data, destination[:room]...)
			s.err = &Error{
				Code: CodeTooLarge, Stage: StageLimit,
				Location: assembly.Location{Source: s.doc.name, Offset: MaxPlanBytes},
				Message:  "plan exceeds the 67108864-byte (64 MiB) limit",
			}

			return 0, s.err
		}

		s.doc.data = append(s.doc.data, destination[:length]...)
	}

	switch {
	case err == nil:
		return length, nil
	case errors.Is(err, io.EOF):
		return length, io.EOF
	default:
		return length, s.fail(err)
	}
}

// sniffByteOrderMark reads up to three bytes, strips a UTF-8 byte order mark, and keeps any other bytes
// pending. A nil error with nothing pending means end of input.
func (s *limitedSource) sniffByteOrderMark() error {
	var head [bomLength]byte

	got := 0

	var err error
	for got < bomLength && err == nil {
		var length int

		length, err = s.readRaw(head[got:])
		got += length
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	rest := head[:got]
	if bytes.HasPrefix(rest, []byte{0xEF, 0xBB, 0xBF}) {
		s.doc.bomLen = bomLength
		rest = nil
	}

	s.pending = append([]byte(nil), rest...)

	return nil
}
