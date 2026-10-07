// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	// fixedValues is the JSON values of a failed report with one diagnostic and no other record.
	fixedValues = 26

	// valuesPerSource is the JSON values of a source without a digest: its object, path, and bytes.
	valuesPerSource = 3

	// sourcesToFill is how many sources make a report of exactly report.MaxNodes values.
	sourcesToFill = (report.MaxNodes - fixedValues) / valuesPerSource
)

// reportOfNodeLimit is a failed report with one diagnostic and as many sources as make exactly report.MaxNodes values.
func reportOfNodeLimit() *report.Report {
	r := report.NewErrorReport(commandReport, report.StatusInvalid, report.Diagnostic{Stage: "s", Code: "c", Message: "m"})
	r.Sources = make([]report.Source, sourcesToFill)

	for index := range r.Sources {
		r.Sources[index] = report.Source{Path: "/p"}
	}

	return r
}

// TestWriteAcceptsExactlyTheNodesTheDecoderReads builds a report of exactly report.MaxNodes JSON values and
// checks that it is written and read back, and that one value more is refused. The count is worked out by
// hand: a failed report with one diagnostic holds 26 values (the object, 4 scalars, 3 objects with their 4, 3
// and 2 members, 5 arrays, and the diagnostic's object with its stage, code and message), and a source without
// a digest is 3 values (object, path, bytes).
func TestWriteAcceptsExactlyTheNodesTheDecoderReads(t *testing.T) {
	t.Parallel()

	if fixedValues+valuesPerSource*sourcesToFill != report.MaxNodes {
		t.Fatalf("%d sources do not make exactly %d values", sourcesToFill, report.MaxNodes)
	}

	var out bytes.Buffer

	_, err := report.Write(&out, reportOfNodeLimit(), report.MaxReportBytes)
	if err != nil {
		t.Fatalf("a report of exactly %d values: %v", report.MaxNodes, err)
	}

	_, err = report.Decode(context.Background(), savedReport, bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Errorf("the decoder refuses what the writer wrote: %v", err)
	}
}

func TestWriteRefusesOneValueBeyondTheDecoderLimit(t *testing.T) {
	t.Parallel()

	r := reportOfNodeLimit()
	r.Sources[0].Digest = digestA

	var out bytes.Buffer

	_, err := report.Write(&out, r, report.MaxReportBytes)
	if errorCode(err) != report.CodeLimitNodes || out.Len() != 0 {
		t.Errorf("one value past the limit: %v after %d bytes", err, out.Len())
	}
}
