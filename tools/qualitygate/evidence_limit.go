// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Fuzz progress is small; this also accommodates the largest observed complete
// native test stream (8.56 MB) without permitting runaway retained output.
const fuzzEvidenceLimit int64 = 32 << 20

type evidenceBudget struct {
	mutex     sync.Mutex
	remaining int64
	limit     int64
	cancel    context.CancelFunc
	failure   error
}

type budgetedEvidenceWriter struct {
	budget *evidenceBudget
	target io.Writer
}

func (w budgetedEvidenceWriter) Write(data []byte) (int, error) {
	b := w.budget
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.failure != nil {
		return 0, b.failure
	}

	allowed := min(int64(len(data)), b.remaining)
	written, err := w.target.Write(data[:allowed])
	b.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	if int64(len(data)) > allowed {
		b.failure = fmt.Errorf("%w: infrastructure error: stdout and stderr exceeded the combined %d-byte evidence limit; captured output is partial", errGate, b.limit)
		b.cancel()
		return written, b.failure
	}
	return written, nil
}

func (b *evidenceBudget) err() error {
	if b == nil {
		return nil
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.failure
}
