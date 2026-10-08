// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	envelopeJSONTransport    = "json"
	envelopeSuccessorMessage = "after-cut"
)

func TestQueryEnvelopeHonorsIndependentJSONAndTextBoundaries(t *testing.T) {
	t.Parallel()

	for _, transport := range []string{envelopeJSONTransport, "text"} {
		t.Run(transport, func(t *testing.T) { t.Parallel(); testEnvelopeTransportBoundary(t, transport) })
	}
}

func testEnvelopeTransportBoundary(t *testing.T, transport string) {
	t.Helper()

	for _, extra := range []int{0, 1} {
		saved, request := envelopeFixtureAtBoundary(t, transport, (128<<10)+extra)
		response := queryOK(t, saved, request)
		candidate := envelopeCandidate(saved, response)
		raw, text := envelopeTransportBytes(t, candidate)

		selected := lenEnvelopeRecords(t, response, request.View)
		assertEnvelopeCalibration(t, transport, (128<<10)+extra, raw, text, selected)

		query := report.QueryResultOf(saved, response, programName, fixtureReportPath)
		gotRaw, gotText := envelopeTransportBytes(t, query)

		returned := lenEnvelopeRecords(t, query.Result, request.View)
		if gotRaw > 128<<10 || gotText > 128<<10 {
			t.Fatalf("wrapped bounds: %d/%d", gotRaw, gotText)
		}

		want := selected
		if extra != 0 {
			want--
		}

		if returned != want {
			t.Fatalf("whole record boundary: returned %d want %d", returned, want)
		}

		assertEnvelopePageEnd(t, saved, request, query, returned)

		if schemaVerdict(t, compileSchema(t, report.ResponseSchema(), responseSchemaURL), envelopeJSON(t, query)) != schemaAccept {
			t.Fatal("wrapped query violates response schema")
		}
	}
}

func assertEnvelopeCalibration(t *testing.T, transport string, target, raw, text, selected int) {
	t.Helper()

	if selected < 2 {
		t.Fatalf("inner query already trimmed boundary fixture: %d", selected)
	}

	if transport == envelopeJSONTransport && (raw != target || text >= raw) {
		t.Fatalf("JSON fixture: %d/%d", raw, text)
	}

	if transport == "text" && (text != target || raw >= text) {
		t.Fatalf("text fixture: %d/%d", raw, text)
	}
}

func envelopeFixtureAtBoundary(t *testing.T, transport string, target int) (*report.Report, report.Request) {
	t.Helper()

	if transport == envelopeJSONTransport {
		return escapedDiagnosticEnvelope(t, target)
	}

	return quotedPartEnvelope(t, target)
}

func escapedDiagnosticEnvelope(t *testing.T, target int) (*report.Report, report.Request) {
	t.Helper()

	request := report.Request{View: report.ViewDiagnostics, Limit: new(int64(2))}
	saved := failedReportFixture(
		report.Diagnostic{
			Stage:   report.StageUsage,
			Code:    report.Code(fixtureBadFlagCode),
			Message: "escaped-cause record",
			Cause:   strings.Repeat("\\\"\n", 10000),
		},
		report.Diagnostic{Stage: report.StageUsage, Code: report.Code(fixtureBadFlagCode), Message: "padded-cause record", Cause: "x"},
		report.Diagnostic{Stage: report.StageUsage, Code: report.Code(fixtureBadFlagCode), Message: envelopeSuccessorMessage},
	)
	candidate := envelopeCandidate(saved, queryOK(t, saved, request))
	size, _ := envelopeTransportBytes(t, candidate)

	padding := target - size
	if padding < 0 {
		t.Fatal("JSON seed exceeds boundary")
	}

	saved.Diagnostics[1].Cause += strings.Repeat("x", padding)

	return saveEnvelopeFixture(t, saved), request
}

func quotedPartEnvelope(t *testing.T, target int) (*report.Report, report.Request) {
	t.Helper()

	request := report.Request{View: report.ViewParts, Limit: new(int64(100))}
	saved := mustDecode(t, completeCheck)
	saved.Styles[0].Text.Value = strings.Repeat("\u0080", 160)
	template := saved.Parts[1]
	saved.Parts = saved.Parts[:1]

	for index := 1; index < 100; index++ {
		part := template
		part.ID = fmt.Sprintf("/items/%d", index)
		part.Pages = new(int64(1))
		part.Range = &report.PageRange{Start: int64(index + 3), End: int64(index + 3)}
		saved.Parts = append(saved.Parts, part)
	}

	saved.Counts.GeneratedPages = new(int64(99))
	saved.Counts.TotalPages = new(int64(102))
	candidate := envelopeCandidate(saved, queryOK(t, saved, request))
	_, size := envelopeTransportBytes(t, candidate)

	padding := target - size
	if padding < 0 {
		t.Fatal("text seed exceeds boundary")
	}
	// Nested authored identifiers add equal ASCII bytes in both transports; previews retain the Go %q difference.
	for index := 1; padding > 0; index++ {
		slot := 1 + (index-1)%99
		if padding >= 8 {
			saved.Parts[slot].ID += "/items/0"
			padding -= 8
		} else {
			id := saved.Parts[slot].ID
			saved.Parts[slot].ID = id[:len(id)-1] + strings.Repeat("1", padding+1)
			padding = 0
		}
	}

	return saveEnvelopeFixture(t, saved), request
}

func saveEnvelopeFixture(t *testing.T, saved *report.Report) *report.Report {
	t.Helper()

	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "saved.json")
	if err := os.WriteFile(path, envelopeJSON(t, saved), 0o600); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	decoded, err := report.Decode(report.DecoderTestContext(t.Context(), t), path, file)
	if err != nil {
		t.Fatal(err)
	}

	return decoded
}

func envelopeCandidate(saved *report.Report, response report.Response) *report.QueryResult {
	return &report.QueryResult{
		FormatVersion: report.Version,
		Kind:          "report_query",
		Command:       "report",
		Status:        report.StatusOK,
		SavedRun:      report.SavedRun{AttemptID: saved.AttemptID, Command: saved.Command, Status: saved.Status},
		Result:        response,
	}
}

func envelopeTransportBytes(t *testing.T, q *report.QueryResult) (int, int) {
	t.Helper()

	var text bytes.Buffer
	if err := q.RenderText(&text); err != nil {
		t.Fatal(err)
	}

	return len(envelopeJSON(t, q)), text.Len()
}

func envelopeJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := jsonv2.Marshal(value, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		t.Fatal(err)
	}

	return append(data, '\n')
}

func lenEnvelopeRecords(t *testing.T, response report.Response, view string) int {
	t.Helper()

	if view == report.ViewDiagnostics {
		data, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](response)
		if !ok {
			t.Fatal("missing diagnostic view")
		}

		return len(data.Records)
	}

	data, ok := report.ContentOf[report.ViewResponse[report.PartView]](response)
	if !ok {
		t.Fatal("missing part view")
	}

	return len(data.Records)
}

func assertEnvelopePageEnd(t *testing.T, saved *report.Report, request report.Request, q *report.QueryResult, returned int) {
	t.Helper()

	var next *int64

	if request.View == report.ViewDiagnostics {
		v, _ := report.ContentOf[report.ViewResponse[report.DiagnosticView]](q.Result)

		next = v.NextOffset
		if v.Returned != returned || v.OversizedRecord {
			t.Fatal("wrong diagnostics count/flag")
		}
	} else {
		v, _ := report.ContentOf[report.ViewResponse[report.PartView]](q.Result)

		next = v.NextOffset
		if v.Returned != returned || v.OversizedRecord {
			t.Fatal("wrong parts count/flag")
		}
	}

	total := len(saved.Parts)
	if request.View == report.ViewDiagnostics {
		total = len(saved.Diagnostics)
	}

	if returned < total {
		if next == nil || *next != int64(returned) {
			t.Fatal("next offset skipped or repeated a record")
		}

		request.Offset = next

		following := queryOK(t, saved, request)
		assertEnvelopeNextRecord(t, saved, following, request.View, returned)
	} else if next != nil {
		t.Fatal("complete page still advertises next offset")
	}
}

func TestQueryEnvelopeEmptyPagesPreserveSelection(t *testing.T) {
	t.Parallel()

	for _, view := range []string{report.ViewParts, report.ViewDiagnostics} {
		fixture := failedReportFixture(report.Diagnostic{Stage: report.StageUsage, Code: "command_bad", Message: "captured failure"})
		fixture.Command = strings.Repeat("a", 32)
		saved := saveEnvelopeFixture(t, fixture)
		req := report.Request{View: view, Offset: new(int64(9223372036854775807))}
		query := report.QueryResultOf(saved, queryOK(t, saved, req), programName, fixtureReportPath)

		raw, text := envelopeTransportBytes(t, query)
		if raw > 128<<10 || text > 128<<10 || lenEnvelopeRecords(t, query.Result, view) != 0 {
			t.Fatal("empty view exceeded bounded envelope")
		}

		if schemaVerdict(t, compileSchema(t, report.ResponseSchema(), responseSchemaURL), envelopeJSON(t, query)) != schemaAccept {
			t.Fatal("empty query schema")
		}
	}
}

func TestQueryEnvelopeOversizedPagePreservesSelection(t *testing.T) {
	t.Parallel()

	cause := strings.Repeat("\\", 128<<10)
	saved := saveEnvelopeFixture(
		t,
		failedReportFixture(
			report.Diagnostic{Stage: report.StageUsage, Code: report.Code(fixtureBadFlagCode), Message: "large", Cause: cause},
			report.Diagnostic{Stage: report.StageUsage, Code: report.Code(fixtureBadFlagCode), Message: envelopeSuccessorMessage},
		),
	)
	req := report.Request{View: report.ViewDiagnostics, Limit: new(int64(2))}
	query := report.QueryResultOf(saved, queryOK(t, saved, req), programName, fixtureReportPath)

	records, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](query.Result)
	if !ok || len(records.Records) != 1 || !records.OversizedRecord || records.NextOffset == nil || *records.NextOffset != 1 ||
		records.Records[0].Cause != cause {
		t.Fatal("oversized record split, lost or skipped")
	}

	req.Offset = records.NextOffset
	next := report.QueryResultOf(saved, queryOK(t, saved, req), programName, fixtureReportPath)

	records, ok = report.ContentOf[report.ViewResponse[report.DiagnosticView]](next.Result)
	if !ok || len(records.Records) != 1 || records.Records[0].Message != envelopeSuccessorMessage || records.NextOffset != nil ||
		records.OversizedRecord {
		t.Fatal("oversized successor lost")
	}
}

func assertEnvelopeNextRecord(t *testing.T, saved *report.Report, response report.Response, view string, index int) {
	t.Helper()

	if view == report.ViewDiagnostics {
		records, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](response)
		if !ok || len(records.Records) == 0 || records.Records[0].Message != saved.Diagnostics[index].Message ||
			records.Records[0].Cause != saved.Diagnostics[index].Cause {
			t.Fatal("trimmed diagnostic lost identity")
		}

		return
	}

	records, ok := report.ContentOf[report.ViewResponse[report.PartView]](response)
	if !ok || len(records.Records) == 0 || records.Records[0].ID != saved.Parts[index].ID {
		t.Fatal("trimmed part lost identity")
	}
}
