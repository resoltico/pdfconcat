// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"context"
	"io"
)

func readStream(c context.Context, reader io.Reader, limit int64) ([]byte, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(reader, limit+1))
}
func readStdin(c context.Context, reader io.Reader, limit int64) ([]byte, error) {
	closer, ok := reader.(io.Closer)
	if !ok {
		return readStream(c, reader, limit)
	}
	type outcome struct {
		data []byte
		err  error
	}
	completed := make(chan outcome, 1)
	go func() {
		data, err := readStream(c, reader, limit)
		completed <- outcome{data: data, err: err}
	}()
	select {
	case <-c.Done():
		// Closing a synchronous Windows console read can itself block. Never wait
		// for it on the outcome path; the CLI exits after rendering interruption.
		go func() { _ = closer.Close() }()
		return nil, c.Err()
	case value := <-completed:
		if err := c.Err(); err != nil {
			return nil, err
		}
		return value.data, value.err
	}
}
