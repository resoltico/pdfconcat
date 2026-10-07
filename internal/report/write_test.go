// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

type failingWriter struct{}

var errBroken = errors.New("disk on fire")

func TestWriteRejectsAnInvalidReport(t *testing.T) {
	t.Parallel()

	bad := mustDecode(t, completeCheck)
	bad.Status = "fine"

	var out bytes.Buffer

	n, err := report.Write(&out, bad, report.MaxReportBytes)
	if errorCode(err) != report.CodeInvalidValue || n != 0 || out.Len() != 0 {
		t.Errorf("n=%d len=%d err=%v", n, out.Len(), err)
	}
}

func TestWriteStopsAtTheByteLimit(t *testing.T) {
	t.Parallel()

	r := mustDecode(t, completeCheck)
	size := int64(len(encodeReport(t, r)))

	var exact bytes.Buffer

	n, err := report.Write(&exact, r, size)
	if err != nil || n != size {
		t.Fatalf("a report of exactly the limit: %d, %v", n, err)
	}

	for _, limit := range []int64{size - 1, size / 2, 0} {
		var out bytes.Buffer

		n, err = report.Write(&out, r, limit)

		found, _ := report.AsError(err)
		if found == nil || found.Diagnostic.Code != report.CodeTooLarge || found.Status() != report.StatusFailed {
			t.Fatalf("limit %d: %v", limit, err)
		}

		if n > limit || int64(out.Len()) > limit {
			t.Errorf("limit %d: wrote %d bytes", limit, out.Len())
		}
	}
}

func TestWriteStopsEarlyOnAHugeReport(t *testing.T) {
	t.Parallel()

	big := mustDecode(t, completeCheck)
	for range 2000 {
		big.Diagnostics = append(big.Diagnostics, report.Diagnostic{Stage: "x", Code: "y", Message: strings.Repeat("m", 1000)})
	}

	big.Status = report.StatusFailed

	var out bytes.Buffer

	_, err := report.Write(&out, big, 100_000)
	if errorCode(err) != report.CodeTooLarge || out.Len() > 100_000 {
		t.Errorf("wrote %d bytes, err %v", out.Len(), err)
	}
}

func TestWriteReportsAFailingWriter(t *testing.T) {
	t.Parallel()

	_, err := report.Write(failingWriter{}, mustDecode(t, completeCheck), report.MaxReportBytes)

	found, _ := report.AsError(err)
	if found == nil || found.Diagnostic.Code != report.CodeWriteFailed || !errors.Is(err, errBroken) {
		t.Errorf("got %v", err)
	}
}

func TestWriteReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()

	r := mustDecode(t, completeCheck)
	r.Sources[0].Path = "/w/\xffname.pdf"

	text := encodeReport(t, r)
	if !strings.Contains(text, "/w/�name.pdf") {
		t.Errorf("path was not made valid: %s", text)
	}

	mustDecode(t, text)
}

func TestWriteEndsWithANewlineAndIsCompact(t *testing.T) {
	t.Parallel()

	text := encodeReport(t, mustDecode(t, richFailure))
	if !strings.HasSuffix(text, "}\n") || strings.Count(text, "\n") != 1 {
		t.Errorf("not a single compact line: %q", text[len(text)-20:])
	}
}

func TestWriteRefusesMoreNodesThanTheDecoderReads(t *testing.T) {
	t.Parallel()

	r := mustDecode(t, completeCheck)
	r.Sources = make([]report.Source, 700_000)

	for i := range r.Sources {
		r.Sources[i] = report.Source{Path: "/p", Bytes: new(int64(1))}
	}

	var out bytes.Buffer

	_, err := report.Write(&out, r, report.MaxReportBytes)

	found, _ := report.AsError(err)
	if found == nil || found.Diagnostic.Code != report.CodeLimitNodes || out.Len() != 0 {
		t.Errorf("got %v after %d bytes", err, out.Len())
	}
}

func TestEncodeFailsOnANonFiniteNumber(t *testing.T) {
	t.Parallel()

	_, err := report.Encode(report.Rect{Width: nan()})
	if err == nil {
		t.Error("NaN has no JSON form")
	}
}

func (failingWriter) Write([]byte) (int, error) { return 0, errBroken }
