// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"context"
	"testing"
	"time"
)

// StageTestContext bounds cooperative fixture copying and production while preserving parent cancellation.
// It is exported only in test builds for the internal and external staging test packages.
func StageTestContext(tb testing.TB) context.Context {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), 2*time.Minute)
	tb.Cleanup(cancel)
	return ctx
}
