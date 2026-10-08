// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"context"
	"fmt"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/plan"
)

// stagedSource serves its chunks in order, announces each call to Read before answering it, and holds back
// the last chunk until the test releases it.
type stagedSource struct {
	reads   chan int
	release chan struct{}
	chunks  [][]byte
	served  int
}

// readAheadLabel is the pprof label that marks the read-ahead goroutine of a test.
const readAheadLabel = "read_ahead_of_test"

func (s *stagedSource) Read(p []byte) (int, error) {
	s.reads <- s.served

	if s.served == len(s.chunks)-1 {
		<-s.release
	}

	chunk := s.chunks[s.served]
	s.served++

	return copy(p, chunk), nil
}

// labeledGoroutines is how many goroutines run with the pprof label key=value. A goroutine inherits the
// labels of the goroutine that starts it, which identifies the one goroutine a test started.
func labeledGoroutines(key, value string) int {
	var profile strings.Builder

	err := pprof.Lookup("goroutine").WriteTo(&profile, 1)
	if err != nil {
		return 0
	}

	return strings.Count(profile.String(), fmt.Sprintf("%q:%q", key, value))
}

// TestCanceledChunkReaderStopsReadingAheadEvenWhenItsOutputIsFull holds the reader in the state where its
// read-ahead has a chunk to hand over and nobody takes it, then cancels: the read-ahead must end instead of
// waiting for a consumer that is gone. The state is reached in a fixed order, so no timing is involved: the
// consumer takes one byte of the first chunk, which keeps that buffer; the second chunk fills the hand-over
// slot; and the read-ahead only reaches its third read once the consumer has given the first buffer back.
func TestCanceledChunkReaderStopsReadingAheadEvenWhenItsOutputIsFull(t *testing.T) {
	t.Parallel()

	source := &stagedSource{
		chunks:  [][]byte{[]byte("first"), []byte("second"), []byte("third")},
		reads:   make(chan int, 8),
		release: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	reader := plan.ChunkReaderOver(ctx, source)

	// The first Read starts the read-ahead goroutine, which inherits this label and so can be told apart
	// from the read-ahead of every other test.
	var (
		one = make([]byte, 1)
		got int
		err error
	)

	pprof.Do(ctx, pprof.Labels(readAheadLabel, t.Name()), func(context.Context) { got, err = reader.Read(one) })

	if got != 1 || err != nil || one[0] != 'f' {
		t.Fatalf("the first byte: %d %v %q", got, err, one)
	}

	// The check that makes the wait below meaningful: the read-ahead goroutine carries the label.
	if labeledGoroutines(readAheadLabel, t.Name()) != 1 {
		t.Fatal("the read-ahead goroutine does not carry the label of the test")
	}

	// The second chunk is read ahead and handed over; the third read waits for a free buffer.
	for want := range 2 {
		waitForSourceRead(t, source, want)
	}

	rest := make([]byte, 16)

	got, err = reader.Read(rest)
	if string(rest[:got]) != "irst" || err != nil {
		t.Fatalf("the rest of the first chunk: %q %v", rest[:got], err)
	}

	// The first buffer is free again, so the read-ahead reads the third chunk, which blocks in the source
	// while the second chunk still occupies the hand-over slot.
	waitForSourceRead(t, source, 2)
	cancel()
	close(source.release)

	deadline := time.Now().Add(10 * time.Second)

	for labeledGoroutines(readAheadLabel, t.Name()) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the read-ahead goroutine did not end after cancellation")
		}

		time.Sleep(time.Millisecond)
	}

	_, err = reader.Read(rest)
	if err == nil || err.Error() != context.Canceled.Error() {
		t.Errorf("reading after cancellation: %v", err)
	}
}

func waitForSourceRead(t *testing.T, source *stagedSource, want int) {
	t.Helper()

	select {
	case got := <-source.reads:
		if got != want {
			t.Fatalf("source read %d, want %d", got, want)
		}
	case <-time.After(startupDeadline):
		t.Fatalf("the source was not read for chunk %d", want)
	}
}
