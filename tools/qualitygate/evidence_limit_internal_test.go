// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestEvidenceBudgetIncludesBothStreamsAndAcceptsExactBoundary(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	budget := &evidenceBudget{remaining: 5, limit: 5, cancel: cancel}
	var stdout, stderr bytes.Buffer
	out := budgetedEvidenceWriter{budget: budget, target: &stdout}
	errOut := budgetedEvidenceWriter{budget: budget, target: &stderr}
	if _, err := out.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := errOut.Write([]byte("de")); err != nil || ctx.Err() != nil {
		t.Fatalf("exact combined boundary refused: %v/%v", err, ctx.Err())
	}
	if n, err := errOut.Write([]byte("f")); n != 0 || !errors.Is(err, errGate) || !strings.Contains(err.Error(), "partial") {
		t.Fatalf("excess byte accepted: %d/%v", n, err)
	}
	if ctx.Err() == nil || stdout.String() != "abc" || stderr.String() != "de" || budget.err() == nil {
		t.Fatalf("limit did not cancel while preserving exact bounded prefix: %v/%q/%q", ctx.Err(), stdout.String(), stderr.String())
	}
	if n, err := out.Write([]byte("more")); n != 0 || err == nil || stdout.String() != "abc" {
		t.Fatalf("writes resumed after budget failure: %d/%v/%q", n, err, stdout.String())
	}
}

func TestEvidenceBudgetRetainsAllowedPrefixOfCrossingWrite(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	budget := &evidenceBudget{remaining: 3, limit: 3, cancel: cancel}
	var retained bytes.Buffer
	n, err := (budgetedEvidenceWriter{budget: budget, target: &retained}).Write([]byte("abcdef"))
	if n != 3 || err == nil || retained.String() != "abc" || ctx.Err() == nil {
		t.Fatalf("crossing write: %d/%v/%q/%v", n, err, retained.String(), ctx.Err())
	}
}

func TestEvidenceBudgetBoundsConcurrentStdoutAndStderr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	budget := &evidenceBudget{remaining: 2500, limit: 2500, cancel: cancel}
	var stdout, stderr bytes.Buffer
	writers := []budgetedEvidenceWriter{{budget: budget, target: &stdout}, {budget: budget, target: &stderr}}
	var joined sync.WaitGroup
	for i := range 8 {
		joined.Go(func() {
			if _, err := writers[i%2].Write(bytes.Repeat([]byte("x"), 1024)); err != nil && !errors.Is(err, errGate) {
				t.Error(err)
			}
		})
	}
	joined.Wait()
	if stdout.Len()+stderr.Len() != 2500 || budget.err() == nil || ctx.Err() == nil {
		t.Fatalf("concurrent streams escaped combined limit: %d/%d/%v/%v", stdout.Len(), stderr.Len(), budget.err(), ctx.Err())
	}
}
