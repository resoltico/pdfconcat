// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/plan"
)

const (
	inputName = "plan.json"
	baseDir   = "/base"
	pdfA      = "a.pdf"
)

func TestMain(m *testing.M) {
	guardAgainstHangs()

	m.Run() // the test runner exits with its result once TestMain returns

	exectest.Cleanup()
}

func inputFor() plan.Input {
	return plan.Input{Name: inputName, BaseDir: baseDir}
}

func decodeString(tb testing.TB, text string) (*assembly.Job, error) {
	tb.Helper()

	return plan.Decode(context.Background(), inputFor(), strings.NewReader(text))
}

func mustDecode(tb testing.TB, text string) *assembly.Job {
	tb.Helper()

	job, err := decodeString(tb, text)
	if err != nil {
		tb.Fatalf("decode: %v", err)
	}

	return job
}

func errorOf(_ *assembly.Job, err error) error { return err }

// codedError asserts that err is a *plan.Error with the given code and returns it.
func codedError(tb testing.TB, err error, code plan.Code) *plan.Error {
	tb.Helper()

	planErr, ok := errors.AsType[*plan.Error](err)
	if !ok || planErr.Code != code {
		tb.Fatalf("want %s, got %v", code, err)
	}

	return planErr
}

// wantCode asserts that err is a *plan.Error with the given code.
func wantCode(tb testing.TB, err error, code plan.Code) {
	tb.Helper()

	planErr, ok := errors.AsType[*plan.Error](err)
	if !ok || planErr.Code != code {
		tb.Fatalf("want %s, got %v", code, err)
	}
}

// closeWithLog closes c and logs a failure: closing a test pipe cannot meaningfully fail.
func closeWithLog(tb testing.TB, closer io.Closer) {
	tb.Helper()

	err := closer.Close()
	if err != nil {
		tb.Logf("close: %v", err)
	}
}

// repeated builds a plan whose items array holds count copies of item.
func repeated(item string, count int) string {
	return `{"version":1,"items":[` + strings.TrimSuffix(strings.Repeat(item+",", count), ",") + `]}`
}

// nestedGroups builds a plan with depth groups nested around one PDF.
func nestedGroups(depth int) string {
	return `{"version":1,"items":[` + strings.Repeat(`{"dir":"d","items":[`, depth) + `"leaf.pdf"` + strings.Repeat(`]}`, depth) + `]}`
}
