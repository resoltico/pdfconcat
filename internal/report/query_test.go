// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// pageStats is what a paging walk needs to know about one page of a view.
	pageStats struct {
		next      *int64
		returned  int
		size      int
		oversized bool
	}

	// pagingCase is one request against the two-part sample report and what the page must say.
	pagingCase struct {
		next      *int64
		offset    int64
		limit     int64
		returned  int
		hasOffset bool
		empty     bool
	}

	// queryErrorCase is a request that must be refused with a usage error of the given code.
	queryErrorCase struct {
		name string
		code report.Code
		req  report.Request
	}

	// walkSpec names a paging walk: the view, the page size, and whether records are detailed.
	walkSpec struct {
		view    string
		limit   int64
		details bool
	}
)

const (
	syntheticPairs     = 5000 // PDF parts, each followed by a blank run
	syntheticBlankSize = 2
	syntheticDiags     = 5000
	longTextRunes      = 10000

	maxPagingSteps = 100_000
	briefBlankSize = 700
)

// syntheticBuilder starts a large complete layout: syntheticPairs one-page PDFs, each followed by a run of
// two generated pages that all share one style whose text is 10 KB. It returns the builder and that text.
func syntheticBuilder() (*report.Builder, string) {
	longText := strings.Repeat("Ā-ļ ", longTextRunes/4)

	builder := report.NewBuilder(testCommandCheck)
	font := builder.Font(report.Font{Digest: digestB, Name: fontName})
	text := report.Text{
		Value:    longText,
		Color:    fixtureBlackColor,
		Anchor:   alignCenter,
		Align:    alignCenter,
		Overflow: fixtureOverflowAllow,
		Size:     12,
		Width:    523,
		Leading:  1.2,
		Bounds:   &report.Rect{X: 36, Y: 36, Width: 523, Height: 770},
		Font:     font,
	}
	style := builder.Style(&report.Style{
		Background: colorWhite, Size: report.PageSize{Origin: report.SizeFollowingSource, Width: 595, Height: 842}, Text: &text,
	})

	page := int64(1)

	for i := range syntheticPairs {
		source := builder.Source(
			report.Source{Path: fmt.Sprintf("/work/inputs/chapter-%05d.pdf", i), Digest: digestA, Bytes: new(int64(1000 + i))},
		)
		builder.AddPart(report.Part{
			ID: fmt.Sprintf(itemPointerFormat, 2*i), Kind: report.PartPDF, Source: &source,
			Range: &report.PageRange{Start: page, End: page}, Pages: new(int64(1)),
			Origin: report.Position{File: jobFile, Offset: new(int64(40 * i)), Line: 2 + i, Column: 5},
		})
		builder.AddPart(report.Part{
			ID: fmt.Sprintf(itemPointerFormat, 2*i+1), Kind: report.PartBlank, Style: &style,
			Range:  &report.PageRange{Start: page + 1, End: page + syntheticBlankSize},
			Pages:  new(int64(syntheticBlankSize)),
			Origin: report.Position{File: jobFile, Offset: new(int64(40*i + 20)), Line: 3 + i, Column: 5},
		})

		page += 1 + syntheticBlankSize
	}

	total := page - 1

	builder.SetPhases(report.Phases{
		Instructions:       report.PhaseComplete,
		InputInspection:    report.PhaseComplete,
		Layout:             report.PhaseComplete,
		OutputVerification: report.PhaseNotRun,
	})
	builder.SetCounts(report.Counts{
		SourcePages: new(int64(syntheticPairs)), GeneratedPages: new(total - syntheticPairs), TotalPages: &total,
	})
	builder.SetPublication(report.Publication{ReportStatus: report.ReportWritten, ReportPath: "/work/job.report.json"})

	return builder, longText
}

// syntheticSuccess is the synthetic layout as a successful check without diagnostics.
func syntheticSuccess() (*report.Report, string) {
	builder, longText := syntheticBuilder()

	return builder.Build(report.StatusOK), longText
}

// syntheticFailure is the synthetic layout with syntheticDiags diagnostics.
func syntheticFailure() (*report.Report, string) {
	builder, longText := syntheticBuilder()

	for i := range syntheticDiags {
		builder.AddDiagnostic(i, report.Diagnostic{
			Stage: fixtureLayoutStage,
			Code:  fixtureOverflowCode,
			Path:  fmt.Sprintf("/work/inputs/chapter-%05d.pdf", i),
			Location: &report.Location{
				File: jobFile, Offset: new(int64(i)), Line: 1 + i, Column: 3,
				Pointer: fmt.Sprintf(itemPointerFormat, i),
			},
			Message: fmt.Sprintf("diagnostic %d: ", i) + strings.Repeat("the text does not fit the box; ", 10),
		})
	}

	return builder.Build(report.StatusInvalid), longText
}

func jsonSize(tb testing.TB, value any) int {
	tb.Helper()

	data, err := report.Encode(value)
	if err != nil {
		tb.Fatal(err)
	}

	return len(data)
}

func queryOK(tb testing.TB, rep *report.Report, req report.Request) report.Response {
	tb.Helper()

	response, err := rep.Query(req)
	if err != nil {
		tb.Fatalf("query %+v: %v", req, err)
	}

	return response
}

// queryAs answers a request that must succeed with a response whose content is a T.
func queryAs[T any](tb testing.TB, rep *report.Report, req report.Request) *T {
	tb.Helper()

	content, ok := report.ContentOf[T](queryOK(tb, rep, req))
	if !ok {
		tb.Fatalf("query %+v: the response content is not a %T", req, content)
	}

	return content
}

// viewStats runs a paged view request and summarizes its page, for either kind of view.
func viewStats(tb testing.TB, rep *report.Report, req report.Request) pageStats {
	tb.Helper()

	response := queryOK(tb, rep, req)

	if view, ok := report.ContentOf[report.ViewResponse[report.PartView]](response); ok {
		return pageStats{next: view.NextOffset, returned: view.Returned, size: jsonSize(tb, view), oversized: view.OversizedRecord}
	}

	if view, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](response); ok {
		return pageStats{next: view.NextOffset, returned: view.Returned, size: jsonSize(tb, view), oversized: view.OversizedRecord}
	}

	tb.Fatalf("query %+v: the response is not a view", req)

	return pageStats{}
}

func TestLongTextIsStoredOnce(t *testing.T) {
	t.Parallel()

	failed, longText := syntheticFailure()

	saved := encodeReport(t, failed)
	if strings.Count(saved, longText) != 1 {
		t.Errorf("the 10 KB text is stored %d times; a shared style entry stores it once", strings.Count(saved, longText))
	}

	if len(failed.Parts) != 2*syntheticPairs || len(failed.Diagnostics) != syntheticDiags {
		t.Fatalf("synthetic report: %d parts, %d diagnostics", len(failed.Parts), len(failed.Diagnostics))
	}

	mustDecode(t, saved)
}

func TestSuccessSummaryIsSmall(t *testing.T) {
	t.Parallel()

	success, _ := syntheticSuccess()

	summary := queryOK(t, success, report.Request{})
	if size := jsonSize(t, summary); size >= 2048 {
		t.Errorf("a normal success summary is %d bytes, want under 2048", size)
	}
}

func TestFailureSummaryDoesNotGrowWithDiagnostics(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	failure := queryAs[report.Summary](t, failed, report.Request{})
	failure.BindContinuation(programName, failed.Publication.ReportPath, originalReportReference)

	if failure.DiagnosticCount != syntheticDiags || len(failure.Diagnostics) > report.PreviewDiagnostics ||
		failure.DiagnosticsOmitted != syntheticDiags-len(failure.Diagnostics) {
		t.Errorf("failure summary shows %d of %d", len(failure.Diagnostics), failure.DiagnosticCount)
	}

	if failure.Kind != report.KindSummary {
		t.Errorf("kind %q", failure.Kind)
	}

	if size := jsonSize(t, failure); size+1 > expectedSummaryBytes {
		t.Errorf("a failure summary is %d bytes; it must not grow with the diagnostic count", size)
	}

	want := []string{
		"pdfconcat",
		commandReport,
		"/work/job.report.json",
		fixtureExpectAttemptFlag,
		failed.AttemptID,
		fixtureViewFlag,
		report.ViewDiagnostics,
	}
	if !slices.Equal(nextArguments(failure.Next), want) {
		t.Errorf("next = %v", failure.Next)
	}
}

func TestPagedResponsesStayWithinTheBound(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	for _, view := range []string{report.ViewParts, report.ViewDiagnostics} {
		page := queryOK(t, failed, report.Request{View: view, Limit: new(int64(report.MaxLimit))})
		if size := jsonSize(t, page); size > report.MaxResponseBytes {
			t.Errorf("%s page is %d bytes", view, size)
		}
	}
}

func TestBriefBlankRecordCarriesAPreviewNotTheText(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	response := queryAs[report.ViewResponse[report.PartView]](
		t, failed, report.Request{View: report.ViewParts, Offset: new(int64(1)), Limit: new(int64(2))},
	)

	blank := response.Records[0]

	generated := blank.Generated != nil && blank.Generated.Text != nil
	if blank.Kind != report.PartBlank || !generated || blank.Style != nil || blank.Source != nil {
		t.Fatalf("brief blank: %+v", blank)
	}

	text := blank.Generated.Text
	if utf8.RuneCountInString(text.Preview) != report.PreviewRunes || !text.Truncated || text.Chars != longTextRunes {
		t.Errorf("preview %d runes, truncated %v, chars %d", utf8.RuneCountInString(text.Preview), text.Truncated, text.Chars)
	}

	if size := jsonSize(t, blank); size > briefBlankSize {
		t.Errorf("a brief blank record is %d bytes", size)
	}
}

func TestBriefPDFRecordCarriesItsPathOnly(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	response := queryAs[report.ViewResponse[report.PartView]](
		t, failed, report.Request{View: report.ViewParts, Offset: new(int64(1)), Limit: new(int64(2))},
	)

	pdf := response.Records[1]
	if pdf.Path != "/work/inputs/chapter-00001.pdf" || pdf.Source != nil || pdf.Generated != nil {
		t.Errorf("brief pdf: %+v", pdf)
	}
}

func TestSelectedBlankPartIsCompleteInOneCall(t *testing.T) {
	t.Parallel()

	failed, longText := syntheticFailure()

	part := queryAs[report.PartResponse](t, failed, report.Request{Part: new("/items/7"), Details: true}).Part
	if part.Kind != report.PartBlank || part.Style == nil || part.Style.Text == nil {
		t.Fatalf("detail: %+v", part)
	}

	checkDetailedText(t, part.Style.Text, longText)

	if part.Style.Size.Origin != report.SizeFollowingSource || part.Style.Background != colorWhite {
		t.Errorf("size and background: %+v", part.Style)
	}

	if *part.Pages != syntheticBlankSize || part.Range.Start != 11 {
		t.Errorf("pages and range: %+v", part)
	}
}

// checkDetailedText checks the synthetic style's text with its font, bounds, and overflow policy materialized.
func checkDetailedText(t *testing.T, text *report.TextDetail, longText string) {
	t.Helper()

	if text.Value != longText {
		t.Error("the detailed part must carry the whole text")
	}

	if text.Font.Name != fontName || text.Font.Digest != digestB {
		t.Errorf("font identity: %+v", text.Font)
	}

	if text.Bounds.Width != 523 || text.Overflow != fixtureOverflowAllow {
		t.Errorf("bounds and overflow policy: %+v", text.TextSettings)
	}
}

func TestSelectedPDFPartIsCompleteInOneCall(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	pdf := queryAs[report.PartResponse](t, failed, report.Request{Part: new("/items/6"), Details: true}).Part
	if pdf.Source == nil {
		t.Fatalf("detail pdf has no source: %+v", pdf)
	}

	if pdf.Source.Path != "/work/inputs/chapter-00003.pdf" || pdf.Source.Digest != digestA || *pdf.Source.Bytes != 1003 {
		t.Errorf("detail pdf source: %+v", pdf.Source)
	}

	if pdf.Path != "" {
		t.Errorf("a detailed pdf carries its source, not a path: %q", pdf.Path)
	}
}

func TestSelectedPartWithoutDetailsStaysBrief(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	brief := queryAs[report.PartResponse](t, failed, report.Request{Part: new("/items/7")}).Part
	if brief.Generated == nil || brief.Style != nil {
		t.Errorf("without details the part stays brief: %+v", brief)
	}
}

func TestPageQuery(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()
	lastPart := fmt.Sprintf(itemPointerFormat, 2*syntheticPairs-1)

	for _, tc := range []struct {
		id     string
		page   int64
		inPart int64
	}{
		{firstItemPointer, 1, 1}, {"/items/1", 2, 1}, {"/items/1", 3, 2}, {"/items/2", 4, 1}, {lastPart, int64(3 * syntheticPairs), 2},
	} {
		response := queryAs[report.PageResponse](t, failed, report.Request{Page: &tc.page})
		if response.Part.ID != tc.id || response.PageInPart != tc.inPart || response.Page != tc.page || response.Kind != "page" {
			t.Errorf("page %d: %s page %d of part", tc.page, response.Part.ID, response.PageInPart)
		}
	}

	detail := queryAs[report.PageResponse](t, failed, report.Request{Page: new(int64(2)), Details: true})
	if detail.Part.Style == nil {
		t.Error("details expand the page's part")
	}

	for _, page := range []int64{0, -1, int64(3*syntheticPairs) + 1} {
		_, err := failed.Query(report.Request{Page: &page})

		want := report.CodePageOutOfRange
		if page < 1 {
			want = report.CodeInvalidNumber
		}

		if errorCode(err) != want {
			t.Errorf("page %d: %v", page, err)
		}
	}
}

func TestPageQueryNeedsACompleteLayout(t *testing.T) {
	t.Parallel()

	_, err := mustDecode(t, failedCheck).Query(report.Request{Page: new(int64(1))})

	if !isUsageError(err, report.CodeLayoutIncomplete) {
		t.Errorf("got %v", err)
	}

	// Incomplete reports still answer part and view queries; unknown ranges and counts stay null.
	part := queryAs[report.PartResponse](t, mustDecode(t, failedCheck), report.Request{Part: new(firstArgvID)}).Part
	if part.Range != nil || part.Pages != nil || part.Path != "/w/a.pdf" {
		t.Errorf(detailFormat, part)
	}

	data, err := report.Encode(part)
	if err != nil || !strings.Contains(string(data), `"range":null,"pages":null`) {
		t.Errorf("unknown must be null: %s %v", data, err)
	}
}

func TestRequestValidationDoesNotRequireAReport(t *testing.T) {
	t.Parallel()

	var saved *report.Report
	for _, request := range []report.Request{
		{Page: new(int64(0))},
		{View: report.ViewParts, Limit: new(int64(0))},
		{View: report.ViewParts, Limit: new(int64(101))},
		{View: report.ViewParts, Offset: new(int64(-1))},
		{View: "unknown"},
		{Details: true},
		{ExpectAttempt: "bad"},
	} {
		_, err := saved.Query(request)

		found, ok := report.AsError(err)
		if !ok || found.Status() != report.StatusInvalid || found.Diagnostic.Stage != report.StageUsage {
			t.Fatalf("invalid request accessed report or lost usage status: %+v: %v", request, err)
		}
	}
}

func TestDirectQueryHonorsAttemptGuard(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, failedCheck)
	if _, err := saved.Query(report.Request{ExpectAttempt: saved.AttemptID}); err != nil {
		t.Fatal(err)
	}

	if _, err := saved.Query(report.Request{ExpectAttempt: "BBBBBBBBBBBBBBBBBBBBBBBBBB"}); errorCode(err) != report.CodeAttemptMismatch {
		t.Fatalf("direct query ignored guard: %v", err)
	}
}

func TestQueryErrors(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, completeCheck)

	for _, tc := range []queryErrorCase{
		{"part and page", report.CodeSelectionConflict, report.Request{Part: new(firstItemPointer), Page: new(int64(1))}},
		{"part and view", report.CodeSelectionConflict, report.Request{Part: new(firstItemPointer), View: report.ViewParts}},
		{"page and view", report.CodeSelectionConflict, report.Request{Page: new(int64(1)), View: report.ViewParts}},
		{"offset without view", report.CodePagingNeedsView, report.Request{Offset: new(int64(0))}},
		{"limit with part", report.CodePagingNeedsView, report.Request{Part: new(firstItemPointer), Limit: new(int64(1))}},
		{"unknown view", report.CodeUnknownView, report.Request{View: "sources"}},
		{"negative offset", report.CodeInvalidPaging, report.Request{View: report.ViewParts, Offset: new(int64(-1))}},
		{"negative limit", report.CodeInvalidPaging, report.Request{View: report.ViewParts, Limit: new(int64(-1))}},
		{"zero limit", report.CodeInvalidPaging, report.Request{View: report.ViewParts, Limit: new(int64(0))}},
		{"limit over the maximum", report.CodeInvalidPaging, report.Request{View: report.ViewParts, Limit: new(int64(101))}},
		{"missing part", report.CodePartNotFound, report.Request{Part: new("/items/9")}},
		{"empty part id", report.CodePartNotFound, report.Request{Part: new("")}},
	} {
		_, err := saved.Query(tc.req)
		if !isUsageError(err, tc.code) {
			t.Errorf(namedErrorFormat, tc.name, err)
		}
	}
}

func TestPagingBoundaries(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, completeCheck) // two parts

	for _, tc := range []pagingCase{
		{nil, 0, 20, 2, false, false},
		{new(int64(1)), 0, 1, 1, true, false},
		{nil, 1, 1, 1, true, false},
		{nil, 2, 20, 0, true, true},
		{nil, 3, 20, 0, true, true},
		{nil, 1 << 62, 20, 0, true, true},
	} {
		req := report.Request{View: report.ViewParts, Limit: &tc.limit}
		if tc.hasOffset {
			req.Offset = &tc.offset
		}

		checkPagingCase(t, &tc, queryAs[report.ViewResponse[report.PartView]](t, saved, req))
	}

	defaults := queryAs[report.ViewResponse[report.DiagnosticView]](t, saved, report.Request{View: report.ViewDiagnostics})
	if defaults.Total != 0 || defaults.Offset != 0 || defaults.NextOffset != nil {
		t.Errorf(detailFormat, defaults)
	}
}

func checkPagingCase(t *testing.T, tc *pagingCase, response *report.ViewResponse[report.PartView]) {
	t.Helper()

	if response.Returned != tc.returned || len(response.Records) != tc.returned || response.Total != 2 || response.Records == nil {
		t.Errorf("%+v: returned %d of %d", tc, response.Returned, response.Total)
	}

	if (response.NextOffset == nil) != (tc.next == nil) || (tc.next != nil && *response.NextOffset != *tc.next) {
		t.Errorf("%+v: next %v", tc, response.NextOffset)
	}

	if tc.empty && !strings.Contains(jsonText(t, response), `"records":[]`) {
		t.Errorf("an empty page renders [] not null: %s", jsonText(t, response))
	}
}

func jsonText(tb testing.TB, value any) string {
	tb.Helper()

	data, err := report.Encode(value)
	if err != nil {
		tb.Fatal(err)
	}

	return string(data)
}

// checkPage verifies one page of a paging walk that started at offset.
func checkPage(tb testing.TB, view string, stats *pageStats, offset int64) {
	tb.Helper()

	if stats.returned == 0 && stats.next != nil {
		tb.Fatalf("%s: an empty page points at offset %d", view, *stats.next)
	}

	if stats.size > report.MaxResponseBytes && (!stats.oversized || stats.returned != 1) {
		tb.Fatalf("%s: %d bytes with %d records, oversized=%v", view, stats.size, stats.returned, stats.oversized)
	}

	if stats.next != nil && *stats.next != offset+int64(stats.returned) {
		tb.Fatalf("%s: next offset %d after %d records from %d", view, *stats.next, stats.returned, offset)
	}
}

// pagedRecords walks a view to its end by following next_offset. It returns every record index seen and the
// number of pages that held one oversized record.
func pagedRecords(tb testing.TB, rep *report.Report, view string, limit int64, details bool) ([]int, int) {
	tb.Helper()

	var seen []int

	oversized := 0
	offset := int64(0)

	for steps := 0; ; steps++ {
		if steps > maxPagingSteps {
			tb.Fatalf("%s: paging does not terminate", view)
		}

		stats := viewStats(tb, rep, report.Request{View: view, Offset: &offset, Limit: &limit, Details: details})
		checkPage(tb, view, &stats, offset)

		if stats.oversized {
			oversized++
		}

		for i := range stats.returned {
			seen = append(seen, int(offset)+i)
		}

		if stats.next == nil {
			return seen, oversized
		}

		offset = *stats.next
	}
}

func TestPagingTerminatesAndCoversEveryRecordOnce(t *testing.T) {
	t.Parallel()

	failed, _ := syntheticFailure()

	// Some diagnostics are far larger than one response, so a detailed view must return them alone.
	for _, index := range []int{0, 7, 8, 4999} {
		failed.Diagnostics[index].Message = strings.Repeat("huge ", 60_000)
	}

	for _, view := range []string{report.ViewParts, report.ViewDiagnostics} {
		for _, details := range []bool{false, true} {
			for _, limit := range []int64{1, 7, 20, 100} {
				checkCoverage(t, failed, walkSpec{view: view, limit: limit, details: details})
			}
		}
	}
}

// checkCoverage pages through a view and checks that every record appears once, in order, and that exactly
// the oversized diagnostics came back alone.
func checkCoverage(t *testing.T, rep *report.Report, spec walkSpec) {
	t.Helper()

	total := len(rep.Parts)
	if spec.view == report.ViewDiagnostics {
		total = len(rep.Diagnostics)
	}

	seen, oversized := pagedRecords(t, rep, spec.view, spec.limit, spec.details)
	if len(seen) != total {
		t.Fatalf("%+v: %d of %d records", spec, len(seen), total)
	}

	for i, index := range seen {
		if index != i {
			t.Fatalf("%+v: record %d at position %d", spec, index, i)
		}
	}

	wantOversized := 0
	if spec.view == report.ViewDiagnostics && spec.details {
		wantOversized = 4
	}

	if oversized != wantOversized {
		t.Errorf("%+v: %d oversized pages, want %d", spec, oversized, wantOversized)
	}
}

func TestDetailedViewStopsAtWholeRecords(t *testing.T) {
	t.Parallel()

	failed, longText := syntheticFailure()

	response := queryAs[report.ViewResponse[report.PartView]](t, failed, report.Request{
		View: report.ViewParts, Limit: new(int64(report.MaxLimit)), Details: true, Offset: new(int64(1)),
	})

	advanced := response.NextOffset != nil && *response.NextOffset == 1+int64(response.Returned)
	if response.Returned < 5 || response.Returned >= report.MaxLimit || !advanced {
		t.Fatalf("returned %d, next %v", response.Returned, response.NextOffset)
	}

	if size := jsonSize(t, response); size > report.MaxResponseBytes {
		t.Errorf("%d bytes", size)
	}

	for i := range response.Records {
		if i%2 == 0 && response.Records[i].Style.Text.Value != longText {
			t.Fatalf("record %d lost text: whole records only", i)
		}
	}

	if response.OversizedRecord {
		t.Error("nothing here is oversized")
	}
}

func TestOversizedRecordIsReturnedAloneWithoutTruncation(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, failedCheck)
	huge := strings.Repeat("é", 300_000)
	saved.Diagnostics[0].Message = huge

	response := queryAs[report.ViewResponse[report.DiagnosticView]](
		t, saved, report.Request{View: report.ViewDiagnostics, Details: true},
	)
	if !response.OversizedRecord || response.Returned != 1 || response.Records[0].Message != huge || response.Records[0].MessageTruncated {
		t.Fatalf("oversized: %+v", response.Returned)
	}

	if response.NextOffset == nil || *response.NextOffset != 1 {
		t.Errorf("the offset must advance past the oversized record: %v", response.NextOffset)
	}

	brief := queryAs[report.ViewResponse[report.DiagnosticView]](t, saved, report.Request{View: report.ViewDiagnostics})

	cut := brief.Records[0].MessageTruncated && utf8.RuneCountInString(brief.Records[0].Message) == report.PreviewRunes
	if !cut || brief.OversizedRecord {
		t.Error("a brief record cuts the message to 160 characters and says so")
	}
}

func TestDetailsWithoutSelectionIsRejected(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, completeCheck)

	_, err := saved.Query(report.Request{Details: true})

	found, ok := report.AsError(err)
	if !ok || found.Diagnostic.Code != report.CodeDetailsNeedSelect || found.Status() != report.StatusInvalid {
		t.Errorf("details with no selector would print the whole saved report: %v", err)
	}
}

func TestUnresolvedBlankStyleIsBrief(t *testing.T) {
	t.Parallel()

	r := mustDecode(t, failedCheck)
	r.Parts = append(r.Parts, report.Part{ID: secondArgvID, Kind: report.PartBlank, Origin: report.Position{File: "argv"}})

	for _, details := range []bool{false, true} {
		part := queryAs[report.PartResponse](t, r, report.Request{Part: new(secondArgvID), Details: details}).Part
		if part.Generated != nil || part.Style != nil || part.Path != "" {
			t.Errorf("details=%v: %+v", details, part)
		}
	}
}

func TestStyleWithoutTextDetail(t *testing.T) {
	t.Parallel()

	part := queryAs[report.PartResponse](t, mustDecode(t, richFailure), report.Request{Part: new(secondArgvID), Details: true}).Part
	if part.Style == nil || part.Style.Text != nil || part.Style.Background != report.BackgroundNone {
		t.Errorf(detailFormat, part.Style)
	}

	brief := queryAs[report.PartResponse](t, mustDecode(t, richFailure), report.Request{Part: new(secondArgvID)}).Part
	if brief.Generated == nil || brief.Generated.Text != nil || brief.Generated.Width != 10 {
		t.Errorf(detailFormat, brief.Generated)
	}
}

func TestParseNumber(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]int64{"0": 0, "20": 20, "-3": -3, "9223372036854775807": 1<<63 - 1, "007": 7} {
		if got, err := report.ParseNumber("--offset", text); err != nil || got != want {
			t.Errorf("%q: %d, %v", text, got, err)
		}
	}

	for _, text := range []string{"", "-", "+1", "1.5", "1e3", "abc", " 1", "1 ", "9223372036854775808", "-9223372036854775809", "０"} {
		if _, err := report.ParseNumber("--offset", text); errorCode(err) != report.CodeInvalidNumber {
			t.Errorf("%q: %v", text, err)
		}
	}
}
