// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"fmt"
	"io"
)

type storageReader struct {
	source io.Reader
}

// ReaderRequiringStorage models a legal finite fixture reader that needs storage to make progress.
// It is exported only in test builds so internal and external codec tests share the same contract.
func ReaderRequiringStorage(source io.Reader) io.Reader {
	return &storageReader{source: source}
}

func (r *storageReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, io.ErrNoProgress
	}

	n, err := r.source.Read(buffer)
	if err != nil {
		return n, fmt.Errorf("fixture source: %w", err)
	}

	return n, nil
}
