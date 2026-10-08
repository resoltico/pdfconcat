// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestQueryPagingAccountsForEnvelopeAndAdvances(t *testing.T) {
	t.Parallel()

	const transportLimit = 128 << 10

	saved, response := nearLimitQueryFixture(t)

	view, selected := report.ContentOf[report.ViewResponse[report.DiagnosticView]](response)
	if !selected || view.Returned != 2 {
		t.Fatal("negative control did not fill two records before envelope")
	}

	query := report.QueryResultOf(saved, response, programName, fixtureReportPath)

	var text strings.Builder
	if err := query.RenderText(&text); err != nil {
		t.Fatal(err)
	}

	payload, err := report.Encode(query)
	if err != nil || len(payload)+1 > transportLimit || text.Len() > transportLimit {
		t.Fatalf(
			"wrapped query: JSON %d, text %d, records %d, next %v: %v",
			len(payload)+1,
			text.Len(),
			view.Returned,
			view.NextOffset,
			err,
		)
	}

	if view.Returned != 1 || view.NextOffset == nil || *view.NextOffset != 1 {
		t.Fatal("wrapped page did not advance by exactly one record")
	}

	request := report.Request{View: report.ViewDiagnostics, Details: true, Limit: new(int64(2)), Offset: view.NextOffset}
	next := queryOK(t, saved, request)

	nextView, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](next)
	if !ok || nextView.Records[0].Message != saved.Diagnostics[1].Message {
		t.Fatal("envelope truncation skipped the next record")
	}
}

func TestSmallQueryPageRetainsAllRecords(t *testing.T) {
	t.Parallel()

	small := diagnosticsOfSizes(t, []int{smallestRecord, smallestRecord})
	smallResponse := queryOK(t, small, report.Request{View: report.ViewDiagnostics})
	report.QueryResultOf(small, smallResponse, programName, fixtureReportPath)

	smallView, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](smallResponse)
	if !ok || smallView.Returned != 2 || smallView.NextOffset != nil || smallView.OversizedRecord {
		t.Fatal("small positive-control page lost records")
	}
}

func TestQuerySummaryIncludesEnvelopeInBothBudgets(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, richFailure)
	long := "/" + strings.Repeat("ā/", 3000)
	saved.Publication.ReportPath = long
	saved.Publication.Output = long
	saved.Publication.RecoveryReport = long + ".pdfconcat-report-123"
	saved.Diagnostics[0].Cause = long
	saved.Diagnostics[0].Location = &report.Location{File: long, Pointer: long}
	saved.Diagnostics[0].Recovery = &report.Recovery{
		Action: fixtureEditInput, Command: commandReport, Replacement: "--plan-json",
		ReportFrom: fixtureQueryReportReference, RecoveryFrom: "publication.recovery_report",
		Location: &report.Location{File: long, Pointer: long},
	}
	saved.Diagnostics[1].Recovery = &report.Recovery{Action: fixtureChooseNewReport, ReportFrom: originalReportReference}
	saved.Diagnostics[1].Cause = long

	response := queryOK(t, saved, report.Request{})
	query := report.QueryResultOf(saved, response, long, long)
	payload, err := report.Encode(query)

	var text strings.Builder
	if textErr := query.RenderText(&text); textErr != nil {
		t.Fatal(textErr)
	}

	if err != nil || len(payload)+1 > expectedSummaryBytes || text.Len() > expectedSummaryBytes {
		t.Fatalf("query summary: JSON %d, text %d: %v: %s", len(payload)+1, text.Len(), err, payload)
	}

	summary, ok := report.ContentOf[report.Summary](query.Result)
	if !ok || !summary.NextOmitted || summary.NextReference.Report != fixtureQueryReportReference ||
		summary.PublicationContext != "historical_target" ||
		summary.RecoveryDirectoryFrom != "complete_report.publication.recovery_report" {
		t.Fatal("query continuation did not bind actual input")
	}
}

func nearLimitQueryFixture(t *testing.T) (*report.Report, report.Response) {
	t.Helper()

	const transportLimit = 128 << 10

	sizes := []int{transportLimit/2 - 1000, transportLimit/2 - 1000, smallestRecord}
	saved := diagnosticsOfSizes(t, sizes)
	request := report.Request{View: report.ViewDiagnostics, Details: true, Limit: new(int64(2))}
	response := queryOK(t, saved, request)

	var raw strings.Builder
	if err := response.RenderText(&raw); err != nil {
		t.Fatal(err)
	}

	// Arrange two complete records exactly at the independently stated transport bound before the wrapper.
	sizes[1] += transportLimit - raw.Len()
	saved = diagnosticsOfSizes(t, sizes)
	response = queryOK(t, saved, request)

	return saved, response
}

func TestWrappedOversizedRecordKeepsItsOffset(t *testing.T) {
	t.Parallel()

	saved := diagnosticsOfSizes(t, []int{2 * (128 << 10), smallestRecord})
	request := report.Request{View: report.ViewDiagnostics, Details: true}
	response := queryOK(t, saved, request)
	query := report.QueryResultOf(saved, response, programName, fixtureReportPath)

	view, selected := report.ContentOf[report.ViewResponse[report.DiagnosticView]](query.Result)
	if !selected || !view.OversizedRecord || view.Returned != 1 || view.NextOffset == nil || *view.NextOffset != 1 {
		t.Fatal("oversized first record stalled or skipped records")
	}

	if err := query.RenderText(failingWriter{}); err == nil {
		t.Fatal("lost query-heading write failure")
	}

	request.Offset = view.NextOffset
	next := queryOK(t, saved, request)

	nextView, selected := report.ContentOf[report.ViewResponse[report.DiagnosticView]](next)
	if !selected || nextView.Returned != 1 || nextView.NextOffset != nil || nextView.Records[0].Message != saved.Diagnostics[1].Message {
		t.Fatal("oversized page lost its successor")
	}
}

func TestWrappedPartPageKeepsSelectedRecords(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, completeCheck)
	response := queryOK(t, saved, report.Request{View: report.ViewParts})
	query := report.QueryResultOf(saved, response, programName, fixtureReportPath)

	parts, selected := report.ContentOf[report.ViewResponse[report.PartView]](query.Result)
	if !selected || parts.Returned != len(saved.Parts) || parts.OversizedRecord {
		t.Fatal("small part page lost records")
	}
}
