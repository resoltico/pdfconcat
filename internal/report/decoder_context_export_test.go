// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"context"
	"testing"
	"time"
)

// DecoderTestContext bounds a fixture decode and preserves an earlier parent cancellation or deadline.
// It is exported only in test builds to share this bound across both codec test packages.
func DecoderTestContext(parent context.Context, tb testing.TB) context.Context {
	tb.Helper()

	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	tb.Cleanup(cancel)

	return ctx
}
