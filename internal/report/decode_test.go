// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// decodeFaultCase is a broken document and where and how the decoder must locate its first fault.
	decodeFaultCase struct {
		name    string
		doc     string
		code    report.Code
		stage   report.Stage
		status  report.Status
		pointer string
		at      string // text where the fault is located, "|" marking the exact byte; "" for none
	}

	// cancelAfterRead cancels its context while delivering the last bytes.
	cancelAfterRead struct {
		cancel context.CancelFunc
		data   []byte
	}
)

func TestSamplesDecodeAndRoundTrip(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]string{"complete": completeCheck, "incomplete": failedCheck, "rich": richFailure} {
		first := mustDecode(t, doc)
		written := encodeReport(t, first)

		var before, after any
		if err := json.Unmarshal([]byte(doc), &before); err != nil {
			t.Fatal(err)
		}

		if err := json.Unmarshal([]byte(written), &after); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s: re-encoding changed the document:\n%s\n%s", name, doc, written)
		}

		if again := mustDecode(t, written); encodeReport(t, again) != written {
			t.Errorf("%s: second round trip differs", name)
		}
	}
}

func (c *cancelAfterRead) Read(p []byte) (int, error) {
	n := copy(p, c.data)
	c.data = c.data[n:]
	c.cancel()

	return n, io.EOF
}

func decodeFaultCases(tb testing.TB) []decodeFaultCase {
	tb.Helper()

	return []decodeFaultCase{
		{"empty", "", report.CodeEmpty, report.StageSyntax, report.StatusInvalid, "", ""},
		{"blank", " \n ", report.CodeEmpty, report.StageSyntax, report.StatusInvalid, "", ""},
		{"array", `[]`, report.CodeNotObject, report.StageShape, report.StatusInvalid, "", "["},
		{"string", `"x"`, report.CodeNotObject, report.StageShape, report.StatusInvalid, "", `"x"`},
		{"truncated", completeCheck[:40], report.CodeSyntax, report.StageSyntax, report.StatusInvalid, "", ""},
		{"trailing", completeCheck + ` x`, report.CodeTrailingData, report.StageSyntax, report.StatusInvalid, "", ""},
		{"second object", completeCheck + completeCheck, report.CodeTrailingData, report.StageSyntax, report.StatusInvalid, "", ""},
		{
			"dangling font", replaceOnce(tb, completeCheck, memberFontZeroEnd, `"font":4}`), report.CodeDanglingReference,
			report.StageShape, report.StatusInvalid, "/styles/0/text/font", `"font":4`,
		},
		{
			"overlapping range", replaceOnce(tb, completeCheck, `"start":4`, `"start":3`), report.CodeInvalidRange,
			report.StageShape, report.StatusInvalid, "/parts/1/range", `"range":{"start":3`,
		},
		{
			"missing member", replaceOnce(tb, completeCheck, `"kind":"report",`, ``), report.CodeMissingMember,
			report.StageShape, report.StatusInvalid, "", "{",
		},
		{
			nullToken, replaceOnce(tb, completeCheck, memberStatusOK, memberStatusNull), report.CodeNull,
			report.StageShape, report.StatusInvalid, pointerStatus, nullToken,
		},
		{
			"unknown member", replaceOnce(tb, completeCheck, `"diagnostics":[],`, `"diagnostics":[],"extra":1,`),
			report.CodeUnknownMember, report.StageShape, report.StatusInvalid, "/extra", `"extra"`,
		},
		{
			"duplicate member", replaceOnce(tb, completeCheck, `"kind":"report",`, duplicatedKindMembers),
			report.CodeDuplicateMember, report.StageSyntax, report.StatusInvalid, "/kind", `"kind":"report",|"kind"`,
		},
	}
}

func TestDecodeFaultsAreLocated(t *testing.T) {
	t.Parallel()

	for _, tc := range decodeFaultCases(t) {
		_, err := decodeText(t, tc.doc)

		found, ok := report.AsError(err)
		if !ok {
			t.Errorf("%s: %v is not a report error", tc.name, err)

			continue
		}

		checkFaultClass(t, &tc, found)

		if found.Diagnostic.Location == nil || found.Diagnostic.Location.File != savedReport {
			t.Errorf("%s: location %+v", tc.name, found.Diagnostic.Location)

			continue
		}

		checkFaultLocation(t, &tc, found.Diagnostic.Location)
	}
}

func checkFaultClass(t *testing.T, tc *decodeFaultCase, found *report.Error) {
	t.Helper()

	diagnostic := found.Diagnostic
	if diagnostic.Code != tc.code || diagnostic.Stage != tc.stage || found.Status() != tc.status {
		t.Errorf("%s: code %s stage %s status %s", tc.name, diagnostic.Code, diagnostic.Stage, found.Status())
	}

	if found.Error() == "" {
		t.Errorf("%s: empty error text", tc.name)
	}
}

func checkFaultLocation(t *testing.T, tc *decodeFaultCase, location *report.Location) {
	t.Helper()

	if tc.pointer != "" && location.Pointer != tc.pointer {
		t.Errorf("%s: pointer %q, want %q", tc.name, location.Pointer, tc.pointer)
	}

	if tc.at == "" {
		return
	}

	marked := strings.Index(tc.at, "|")
	want := int64(strings.Index(tc.doc, strings.Replace(tc.at, "|", "", 1)) + max(marked, 0))

	if location.Offset == nil || *location.Offset != want {
		t.Errorf("%s: offset %v, want %d", tc.name, location, want)
	}
}

func TestLineAndColumnCountBytes(t *testing.T) {
	t.Parallel()

	doc := strings.ReplaceAll(completeCheck, memberStatusOK, "\n  \"status\":null")

	_, err := decodeText(t, doc)

	found, _ := report.AsError(err)
	if loc := found.Diagnostic.Location; loc.Line != 2 || loc.Column != 12 {
		t.Errorf("line %d column %d, want 2:12 (%s)", loc.Line, loc.Column, loc)
	}
}

func TestByteOrderMarkIsAcceptedAndCountedInOffsets(t *testing.T) {
	t.Parallel()

	bom := "\xef\xbb\xbf"
	mustDecode(t, bom+completeCheck)

	doc := bom + replaceOnce(t, completeCheck, memberStatusOK, memberStatusNull)

	_, err := decodeText(t, doc)

	found, _ := report.AsError(err)
	if loc := found.Diagnostic.Location; *loc.Offset != int64(strings.Index(doc, nullToken)) ||
		loc.Column != strings.Index(doc, nullToken)-2 {
		t.Errorf("offset %d column %d for %q", *loc.Offset, loc.Column, doc[:60])
	}
}

func TestLimits(t *testing.T) {
	t.Parallel()

	size := int64(len(completeCheck))
	limits := report.DefaultLimits()

	if limits.MaxBytes != 256<<20 || limits.MaxNesting != 64 || limits.MaxNodes != 2_000_000 {
		t.Fatalf("default limits %+v", limits)
	}

	decode := func(doc string, limits report.Limits) error {
		_, err := report.DecodeLimited(
			report.DecoderTestContext(t.Context(), t),
			savedReport,
			report.ReaderRequiringStorage(strings.NewReader(doc)),
			limits,
		)

		return err
	}

	limits.MaxBytes = size
	if err := decode(completeCheck, limits); err != nil {
		t.Errorf("a report of exactly the byte limit: %v", err)
	}

	limits.MaxBytes = size - 1
	if code := errorCode(decode(completeCheck, limits)); code != report.CodeTooLarge {
		t.Errorf("one byte over: %s", code)
	}

	nest := func(depth int) string {
		return `{"format_version":2,"attempt_id":"AAAAAAAAAAAAAAAAAAAAAAAAAA","kind":"report","x":` + strings.Repeat(
			"[",
			depth-1,
		) + strings.Repeat(
			"]",
			depth-1,
		) + `}`
	}

	if code := errorCode(decode(nest(64), report.DefaultLimits())); code == report.CodeLimitDepth {
		t.Error("nesting 64 is within the limit")
	}

	if code := errorCode(decode(nest(65), report.DefaultLimits())); code != report.CodeLimitDepth {
		t.Errorf("nesting 65: %s", code)
	}

	if code := errorCode(decode(nest(20000), report.DefaultLimits())); code != report.CodeLimitDepth {
		t.Errorf("very deep input: %s", code)
	}
}

func TestReadFailuresAndInterruption(t *testing.T) {
	t.Parallel()

	_, err := report.Decode(report.DecoderTestContext(t.Context(), t), savedReport, iotest.ErrReader(io.ErrClosedPipe))

	found, _ := report.AsError(err)
	if found.Diagnostic.Code != report.CodeReadFailed || found.Status() != report.StatusFailed || !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("read failure: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = report.Decode(ctx, savedReport, strings.NewReader(completeCheck))

	found, _ = report.AsError(err)
	if found.Diagnostic.Code != report.CodeInterrupted || found.Status().ExitCode() != 130 || !errors.Is(err, context.Canceled) {
		t.Errorf("interruption: %v", err)
	}

	// A reader that cancels while delivering the last bytes: the token pass notices it.
	ctx, cancel = context.WithCancel(context.Background())
	_, err = report.Decode(ctx, savedReport, &cancelAfterRead{cancel: cancel, data: []byte(completeCheck)})

	if errorCode(err) != report.CodeInterrupted {
		t.Errorf("cancel during read: %v", err)
	}
}

func TestStatusExitCodes(t *testing.T) {
	t.Parallel()

	for status, want := range map[report.Status]int{
		report.StatusOK: 0, report.StatusInvalid: 2, report.StatusFailed: 1, report.StatusInterrupted: 130,
	} {
		if status.ExitCode() != want {
			t.Errorf("%s: %d", status, status.ExitCode())
		}
	}
}
