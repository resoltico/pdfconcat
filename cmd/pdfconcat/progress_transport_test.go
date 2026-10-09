// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux || windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	progressTestRecord  = "{\"kind\":\"progress\"}\n"
	progressEmptyRecord = "{}\n"
)

var errProgressCompletedRecord = errors.New("invalid completed JSON record")

func progressPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	reader, writer, err := os.Pipe()
	requireProgressNoError(t, err)
	closeProgressResource(t, reader)
	closeProgressResource(t, writer)

	return reader, writer
}

func progressNativeTransport(t *testing.T, writer *os.File) *progressTransport {
	t.Helper()

	transport, err := newProgressTransport(writer)
	requireProgressNoError(t, err)
	closeProgressResource(t, transport)

	return transport
}

func closeProgressResource(t *testing.T, resource io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if err := resource.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	})
}

func requireProgressNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func assertProgressPoisoned(ctx context.Context, t *testing.T, transport *progressTransport) {
	t.Helper()

	for _, record := range [][]byte{[]byte(progressTestRecord), []byte("{\"kind\":\"exception\",\"message\":\"late cleanup\"}\n")} {
		if err := transport.WriteRecord(ctx, record); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("poisoned followup accepted: %v", err)
		}
	}
}

func progressLongRecord(t *testing.T) []byte {
	t.Helper()
	return progressUnicodeRecord(t, 5000)
}

func progressUnicodeRecord(t *testing.T, repetitions int) []byte {
	t.Helper()

	record, err := json.Marshal(map[string]string{"kind": "exception", "path": strings.Repeat("path/é/\u65e5\u672c/'/line\n", repetitions)})
	requireProgressNoError(t, err)

	return append(record, '\n')
}

func progressValidateStream(data []byte) (int, []byte, error) {
	before, after, ok := bytes.CutLast(data, []byte{'\n'})
	if !ok {
		return 0, data, nil
	}

	count := 0

	for line := range bytes.SplitSeq(before, []byte("\n")) {
		if !utf8.Valid(line) || !json.Valid(line) {
			return count, nil, errProgressCompletedRecord
		}

		count++
	}

	return count, after, nil
}

func assertProgressTerminalStream(t *testing.T, data, record []byte) {
	t.Helper()

	_, tail, err := progressValidateStream(data)
	requireProgressNoError(t, err)

	if !bytes.HasPrefix(record, data) {
		t.Fatal("failed write replayed bytes or appended a later record")
	}

	if len(tail) > 0 && !bytes.HasPrefix(record, tail) {
		t.Fatal("failure tail is not the final attempted record prefix")
	}
}

func TestProgressStreamFailureAllowanceRejectsMalformedCompleteRecords(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{
		[]byte("{}\n{bad}\npartial"),
		[]byte("{bad}\n{}\n"),
		{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}', '\n'},
	} {
		if _, _, err := progressValidateStream(data); err == nil {
			t.Fatalf("malformed completed record accepted: %q", data)
		}
	}

	count, tail, err := progressValidateStream([]byte("{}\n{\"valid\":true}"))
	requireProgressNoError(t, err)

	if count != 1 || len(tail) == 0 {
		t.Fatal("JSON without LF promoted to completed record")
	}
}

func TestProgressTransportHealthyLongUnicodeRecord(t *testing.T) {
	t.Parallel()
	reader, writer := progressPipe(t)
	transport := progressNativeTransport(t, writer)
	record := progressUnicodeRecord(t, 1000)
	observed := make(chan []byte, 1)
	readErrors := make(chan error, 1)

	go func() { data, err := io.ReadAll(reader); observed <- data; readErrors <- err }()

	requireProgressNoError(t, transport.WriteRecord(context.Background(), record))
	requireProgressNoError(t, transport.WriteRecord(context.Background(), []byte(progressTestRecord)))
	requireProgressNoError(t, transport.Close())

	if transport.Interrupted() {
		t.Fatal("normal close reported interrupted telemetry")
	}

	requireProgressNoError(t, writer.Close())

	data := <-observed

	requireProgressNoError(t, <-readErrors)

	expected := append(bytes.Clone(record), []byte(progressTestRecord)...)
	if !bytes.Equal(data, expected) {
		t.Fatal("long Unicode record was changed, replayed or interleaved")
	}

	count, tail, frameErr := progressValidateStream(data)
	requireProgressNoError(t, frameErr)

	if count != 2 || len(tail) != 0 {
		t.Fatal("healthy stream did not finish complete records")
	}
}

func TestProgressTransportLargeStoppedPipe(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		want error
		name string
	}{{name: "deadline", want: context.DeadlineExceeded}, {name: "cancellation", want: context.Canceled}}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			reader, writer := progressPipe(t)
			transport := progressNativeTransport(t, writer)
			record := progressLongRecord(t)
			ctx, cancel := context.WithCancel(context.Background())
			prefix := make(chan []byte, 1)

			probeErrors := make(chan error, 1)
			go func() {
				data := make([]byte, 1)

				_, err := io.ReadFull(reader, data)
				prefix <- data

				probeErrors <- err

				if errors.Is(scenario.want, context.Canceled) {
					cancel()
				}
			}()

			writeErr := transport.WriteRecord(ctx, record)

			cancel()

			if !errors.Is(writeErr, scenario.want) {
				t.Fatalf("stalled large write: %v", writeErr)
			}

			if !transport.Interrupted() {
				t.Fatal("abandoned large write did not poison sink")
			}

			head := <-prefix

			requireProgressNoError(t, <-probeErrors)

			observed := make(chan []byte, 1)
			readErrors := make(chan error, 1)

			go func() { data, err := io.ReadAll(reader); observed <- data; readErrors <- err }()

			assertProgressPoisoned(t.Context(), t, transport)
			requireProgressNoError(t, transport.Close())
			requireProgressNoError(t, writer.Close())

			data := concatProgressData(head, <-observed)

			requireProgressNoError(t, <-readErrors)
			assertProgressTerminalStream(t, data, record)
		})
	}
}

func TestProgressTransportUnsentCancellationKeepsExceptionCapability(t *testing.T) {
	t.Parallel()
	reader, writer := progressPipe(t)
	transport := progressNativeTransport(t, writer)
	record := progressLongRecord(t)
	prefix := make(chan []byte, 1)
	resume := make(chan struct{})
	observed := make(chan []byte, 1)

	readErrors := make(chan error, 1)
	go func() {
		head := make([]byte, 1)

		_, headErr := io.ReadFull(reader, head)
		prefix <- head

		<-resume

		rest, readErr := io.ReadAll(reader)
		observed <- concatProgressData(head, rest)

		readErrors <- errors.Join(headErr, readErr)
	}()

	active := make(chan error, 1)
	go func() { active <- transport.WriteRecord(context.Background(), record) }()

	<-prefix // Actual native bytes establish that the record owner is occupied.

	unsent, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err := transport.WriteRecord(unsent, []byte(progressTestRecord))

	cancel()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unsent producer: %v", err)
	}

	if transport.Interrupted() {
		t.Fatal("pre-admission timeout poisoned active healthy channel")
	}

	close(resume)
	requireProgressNoError(t, <-active)

	exceptional := []byte("{\"kind\":\"exception\",\"message\":\"actual cleanup\"}\n")
	requireProgressNoError(t, transport.WriteRecord(context.Background(), exceptional))
	requireProgressNoError(t, transport.Close())
	requireProgressNoError(t, writer.Close())

	data := <-observed

	requireProgressNoError(t, <-readErrors)

	if !bytes.Equal(data, append(bytes.Clone(record), exceptional...)) {
		t.Fatal("unsent cancellation changed later exception stream")
	}
}

func concatProgressData(head, rest []byte) []byte {
	combined := make([]byte, 0, len(head)+len(rest))
	combined = append(combined, head...)

	return append(combined, rest...)
}
