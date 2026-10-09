// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"time"
)

const (
	progressWriteTimeout           = 250 * time.Millisecond
	progressPollMilliseconds       = 10
	progressMinimumOwnedDescriptor = 3
)

// Interrupted reports an abandoned write, retired channel or unavailable sink;
// cancellation before admission and normal Close do not set it.
func (transport *progressTransport) Interrupted() bool {
	return transport.failed.Load()
}

func (transport *progressTransport) poison() {
	transport.failed.Store(true)
	transport.closedOnce.Do(func() { close(transport.closed) })
}
