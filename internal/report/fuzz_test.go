// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/resoltico/pdfconcat/internal/report"
)

// codedName is the shape of every stage and code: lower-case snake_case.
func codedName() *regexp.Regexp {
	return regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
}

// checkRejection requires a decoder rejection to be a located, coded, exit-2 error.
func checkRejection(t *testing.T, err error, name *regexp.Regexp) {
	t.Helper()

	found, ok := report.AsError(err)
	if !ok {
		t.Fatalf("a rejection must be a report error: %#v", err)
	}

	coded := name.MatchString(string(found.Diagnostic.Code)) && name.MatchString(string(found.Diagnostic.Stage))
	located := found.Diagnostic.Message != "" && found.Diagnostic.Location != nil

	if !coded || !located || found.Status() != report.StatusInvalid {
		t.Fatalf("a rejection must be a located, coded, exit-2 error: %#v", err)
	}
}

// checkAcceptedReport requires an accepted report to re-encode into a document that the schema validator and
// the decoder both accept again.
func checkAcceptedReport(t *testing.T, compiled *jsonschema.Schema, decoded *report.Report) {
	t.Helper()

	var out bytes.Buffer

	_, err := report.Write(&out, decoded, report.MaxReportBytes)
	if err != nil {
		t.Fatalf("an accepted report must be writable: %v", err)
	}

	if got := schemaVerdict(t, compiled, out.Bytes()); got != schemaAccept {
		t.Fatalf("schema %s for an accepted report:\n%s", got, out.String())
	}

	again, err := report.Decode(context.Background(), "again.json", bytes.NewReader(out.Bytes()))
	if err != nil || encodeReport(t, again) != out.String() {
		t.Fatalf("re-decoding changed the report: %v", err)
	}
}

// FuzzDecodeReport feeds arbitrary bytes to the untrusted-file decoder. It must neither panic nor hang
// nor allocate beyond the limits, must reject with a coded diagnostic, and whatever it accepts must
// re-encode into a document that the schema validator and the decoder both accept again.
func FuzzDecodeReport(f *testing.F) {
	compiled := reportSchema(f)
	name := codedName()

	seeds := []string{completeCheck, failedCheck, richFailure, "", "{}", "[]", "\xef\xbb\xbf" + completeCheck, completeCheck + "x"}
	for _, doc := range seeds {
		f.Add([]byte(doc))
	}

	for _, row := range corpus() {
		f.Add([]byte(documentOf(f, &row)))
	}

	limits := report.Limits{MaxBytes: 1 << 20, MaxNesting: report.MaxNesting, MaxNodes: 20_000}

	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := report.DecodeLimited(context.Background(), "fuzz.json", bytes.NewReader(data), limits)
		if err != nil {
			checkRejection(t, err, name)

			return
		}

		checkAcceptedReport(t, compiled, decoded)
	})
}

// fuzzRequest builds a request from the fuzzer's values: one selection mode, with paging values where they apply.
func fuzzRequest(mode uint8, id string, page, offset, limit int64, which uint8, details bool) report.Request {
	req := report.Request{Details: details}

	switch mode % 4 {
	case 0:
		req.Part = &id
	case 1:
		req.Page = &page
	case 2:
		req.View = []string{report.ViewParts, report.ViewDiagnostics, id}[int(which/3)%3]
		req.Offset, req.Limit = &offset, &limit
	default:
		req.Part, req.Offset = &id, &offset
	}

	return req
}

// checkResponse requires a response to encode, render, and, for a paged view, to stay within the bound.
func checkResponse(t *testing.T, response report.Response, req *report.Request) {
	t.Helper()

	data, err := report.Encode(response)
	if err != nil {
		t.Fatal(err)
	}

	var text bytes.Buffer

	err = response.RenderText(&text)
	if err != nil {
		t.Fatal(err)
	}

	oversized := bytes.Contains(data, []byte(`"oversized_record":true`)) && bytes.Contains(data, []byte(`"returned":1`))
	if len(data) > report.MaxResponseBytes && !oversized && req.View != "" {
		t.Fatalf("a paged response is %d bytes", len(data))
	}
}

// FuzzQuery runs arbitrary requests against fixed valid reports. Every request must either fail with a
// usage error or return a bounded, encodable response; paging by next_offset must terminate.
func FuzzQuery(f *testing.F) {
	reports := []*report.Report{mustDecode(f, completeCheck), mustDecode(f, failedCheck), mustDecode(f, richFailure)}
	reports[1].Diagnostics[0].Message = strings.Repeat("long ", 40_000)

	f.Add(uint8(0), firstItemPointer, int64(1), int64(0), int64(20), uint8(0), false)
	f.Add(uint8(1), "", int64(2), int64(0), int64(20), uint8(0), true)
	f.Add(uint8(2), "", int64(0), int64(0), int64(1), uint8(1), true)
	f.Add(uint8(2), "", int64(0), int64(-1), int64(101), uint8(2), false)
	f.Add(uint8(3), firstArgvID, int64(1), int64(5), int64(5), uint8(1), true)

	f.Fuzz(func(t *testing.T, mode uint8, id string, page, offset, limit int64, which uint8, details bool) {
		saved := reports[int(which)%len(reports)]
		req := fuzzRequest(mode, id, page, offset, limit, which, details)

		response, err := saved.Query(req)
		if err != nil {
			found, ok := report.AsError(err)
			if !ok || found.Status() != report.StatusInvalid || found.Diagnostic.Stage != report.StageUsage {
				t.Fatalf("a bad request is an exit-2 usage error: %v", err)
			}

			return
		}

		checkResponse(t, response, &req)

		if req.View != "" {
			walkTerminates(t, saved, &req)
		}
	})
}

// walkTerminates follows next_offset from the request's view and checks that it ends within total+1 steps.
func walkTerminates(t *testing.T, saved *report.Report, req *report.Request) {
	t.Helper()

	total := len(saved.Parts)
	if req.View == report.ViewDiagnostics {
		total = len(saved.Diagnostics)
	}

	offset := int64(0)

	for step := 0; step <= total+1; step++ {
		walk := *req
		walk.Offset = &offset

		next := viewStats(t, saved, walk).next
		if next == nil {
			return
		}

		if *next <= offset {
			t.Fatalf("next offset %d does not advance past %d", *next, offset)
		}

		offset = *next
	}

	t.Fatal("paging did not terminate")
}
