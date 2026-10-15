// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pipeline

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/result"
)

type observedPipe struct {
	*os.File
	pending chan struct{}
	reads   int
}

func (r *observedPipe) Read(data []byte) (int, error) {
	r.reads++
	if r.reads == 2 {
		close(r.pending)
	}
	return r.File.Read(data)
}

func TestStdinCancellationUnblocksPendingNativePipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	pipe := &observedPipe{File: reader, pending: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan result.Result, 1)
	base := t.TempDir()
	go func() { completed <- Run(ctx, cli.Command{Name: "check", Plan: "-"}, base, pipe) }()
	if _, err = writer.Write([]byte(`{"version":2,"items":[`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pipe.pending:
	case <-time.After(5 * time.Second):
		t.Fatal("plan reader never reached its pending read")
	}
	cancel()
	select {
	case r := <-completed:
		if r.Status != "interrupted" || r.Output != nil {
			t.Fatalf("interrupted outcome %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stdin cancellation did not return")
	}
}

type blockedInput struct {
	started, releaseRead, releaseClose chan struct{}
	once                               sync.Once
}

func (r *blockedInput) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.releaseRead
	return 0, io.EOF
}
func (r *blockedInput) Close() error { <-r.releaseClose; return nil }

func TestStdinOutcomeDoesNotWaitForBlockingClose(t *testing.T) {
	reader := &blockedInput{started: make(chan struct{}), releaseRead: make(chan struct{}), releaseClose: make(chan struct{})}
	defer close(reader.releaseRead)
	defer close(reader.releaseClose)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan error, 1)
	go func() { _, err := readStdin(ctx, reader); completed <- err }()
	<-reader.started
	cancel()
	select {
	case err := <-completed:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interruption waited on Close or Read")
	}
}
