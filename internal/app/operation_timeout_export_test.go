// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"testing"
	"time"
)

// OperationTestTimeout bounds one test operation without extending an earlier caller deadline.
// It is exported only in test builds for shared internal and external app fixture helpers.
const OperationTestTimeout = 2 * time.Minute

// OperationTestContext bounds direct fixture calls while preserving earlier parent cancellation.
func OperationTestContext(parent context.Context, tb testing.TB) context.Context {
	tb.Helper()

	ctx, cancel := context.WithTimeout(parent, OperationTestTimeout)
	tb.Cleanup(cancel)

	return ctx
}
