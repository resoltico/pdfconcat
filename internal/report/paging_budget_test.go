// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

// budgetCase is a list of record sizes (in encoded bytes) and how a detailed diagnostics page must fill.
type budgetCase struct {
	name  string
	sizes []int
}

const (
	// recordFrame is a detailed diagnostic record without its message text; the record is this plus the message.
	recordFrame = `{"severity":"error","stage":"s","code":"c","message":""}`

	// pageBudget selects inputs near the independently specified 128 KiB transport bound.
	pageBudget = (128 << 10) - 1024

	// smallestRecord is the size of a record whose message is one character.
	smallestRecord = len(recordFrame) + 1
)

func diagnosticsOfSizes(t *testing.T, sizes []int) *report.Report {
	t.Helper()

	diagnostics := make([]report.Diagnostic, len(sizes))

	for index, size := range sizes {
		diagnostics[index] = report.Diagnostic{Stage: "s", Code: "c", Message: strings.Repeat("x", size-len(recordFrame))}
	}

	return failedReportFixture(diagnostics...)
}

func budgetCases() []budgetCase {
	const (
		cost    = 2048
		perPage = 100
	)

	uniform := make([]int, perPage)
	for index := range uniform {
		uniform[index] = cost - 1
	}

	half := pageBudget/2 - 1

	return []budgetCase{
		{"many uniform records", uniform},
		{"two near-half-budget records plus a tiny record", []int{half, half, smallestRecord}},
		{"unequal near-half-budget records", []int{half, half + 1, smallestRecord}},
		{"one near-budget record", []int{pageBudget - 1}},
		{"near-budget and tiny records", []int{pageBudget, smallestRecord}},
		{"oversized first record followed by a tiny record", []int{2 * (128 << 10), smallestRecord}},
	}
}

func TestPagedRecordsRespectActualEncodedBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range budgetCases() {
		saved := diagnosticsOfSizes(t, tc.sizes)

		response, err := saved.Query(report.Request{View: report.ViewDiagnostics, Details: true, Limit: new(int64(report.MaxLimit))})
		if err != nil {
			t.Fatal(err)
		}

		view, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](response)
		if !ok || view.Returned < 1 {
			t.Fatal("missing selected records")
		}

		checkBudgetPage(t, tc.name, saved, response, view)
	}
}

// The standard library independently measures a whole candidate page, including its final envelope.
func assertNextRecordExceedsBudget(t *testing.T, view *report.ViewResponse[report.DiagnosticView], next report.Diagnostic) {
	t.Helper()

	candidate := *view
	candidate.Records = append(
		append([]report.DiagnosticView{}, view.Records...),
		report.DiagnosticView{Severity: next.Severity, Stage: next.Stage, Code: next.Code, Message: next.Message},
	)
	candidate.Returned++

	candidate.NextOffset = nil
	if candidate.Returned < candidate.Total {
		candidate.NextOffset = new(int64(candidate.Returned))
	}

	payload, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if len(payload)+1 <= 128<<10 {
		t.Fatal("the next complete record would fit")
	}
}

func TestDetailedRecordSizeIsWhatThePagingArithmeticAssumes(t *testing.T) {
	t.Parallel()

	for _, size := range []int{len(recordFrame) + 1, 1000} {
		saved := diagnosticsOfSizes(t, []int{size})

		response := queryAs[report.ViewResponse[report.DiagnosticView]](
			t, saved, report.Request{View: report.ViewDiagnostics, Details: true},
		)

		if got := jsonSize(t, response.Records[0]); got != size {
			t.Errorf("record of %d bytes encodes to %d", size, got)
		}
	}
}

func checkBudgetPage(
	t *testing.T,
	name string,
	saved *report.Report,
	response report.Response,
	view *report.ViewResponse[report.DiagnosticView],
) {
	t.Helper()

	var human bytes.Buffer
	if err := response.RenderText(&human); err != nil {
		t.Fatal(err)
	}

	if !view.OversizedRecord && human.Len() > 128<<10 {
		t.Fatalf("%s: human payload %d", name, human.Len())
	}

	if view.OversizedRecord && (view.Returned != 1 || human.Len() <= 128<<10) {
		t.Fatalf("%s: false oversized state", name)
	}

	checkPageRecords(t, saved, view)
}

func checkPageRecords(t *testing.T, saved *report.Report, view *report.ViewResponse[report.DiagnosticView]) {
	t.Helper()

	for index := range view.Records {
		if view.Records[index].Message != saved.Diagnostics[index].Message {
			t.Fatal("record changed")
		}
	}

	if view.Returned < len(saved.Diagnostics) {
		if view.NextOffset == nil || *view.NextOffset != int64(view.Returned) {
			t.Fatal("offset did not advance")
		}

		if !view.OversizedRecord {
			assertNextRecordExceedsBudget(t, view, saved.Diagnostics[view.Returned])
		}
	} else if view.NextOffset != nil {
		t.Fatal("finished page has next offset")
	}
}
