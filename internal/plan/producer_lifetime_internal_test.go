// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

func TestDecodeEarlyFailureReleasesIdleReadProducers(t *testing.T) {
	const label = "plan_decode_producer"
	pprof.Do(context.Background(), pprof.Labels(label, t.Name()), func(ctx context.Context) {
		for range 10 {
			_, err := Decode(ctx, Input{}, strings.NewReader(strings.Repeat("@", 3*readChunkBytes)))
			var failure *Error
			if !errors.As(err, &failure) {
				t.Fatalf("invalid input did not fail: %v", err)
			}
		}
	})
	deadline := time.Now().Add(pumpWaitLimit)
	for hasReadProducerLabel(t, label) {
		if time.Now().After(deadline) {
			t.Fatal("completed decodes retain idle read producers")
		}
		time.Sleep(time.Millisecond)
	}
}

func hasReadProducerLabel(t *testing.T, label string) bool {
	t.Helper()
	var output bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&output, 1); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("goroutine observation was empty")
	}
	return strings.Contains(output.String(), fmt.Sprintf("%q:%q", label, t.Name()))
}
