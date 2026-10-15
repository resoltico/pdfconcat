// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package inputfile reads named regular inputs without opening known streams/devices.
package inputfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrTooLarge = errors.New("input exceeds its byte limit")

// Read follows input symlinks. A positive limit bounds bytes retained; zero is
// unbounded. The Stat/open/f.Stat checks assume trusted local namespaces.
func Read(c context.Context, path string, limit int64) ([]byte, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if limit > 0 && info.Size() > limit {
		return nil, ErrTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if limit > 0 && info.Size() > limit {
		return nil, ErrTooLarge
	}
	return ReadStream(c, f, limit)
}

// ReadStream bounds retained bytes and checks cancellation between reads.
// A caller using a blocking stream must also arrange to unblock it on cancellation.
func ReadStream(c context.Context, reader io.Reader, limit int64) ([]byte, error) {
	var err error
	var b bytes.Buffer
	chunk := make([]byte, 256<<10)
	for {
		if err = c.Err(); err != nil {
			return nil, err
		}
		next := chunk
		if limit > 0 {
			next = next[:min(int64(len(next)), limit-int64(b.Len())+1)]
		}
		n, e := reader.Read(next)
		b.Write(next[:n])
		if limit > 0 && int64(b.Len()) > limit {
			return nil, ErrTooLarge
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	return b.Bytes(), nil
}
