// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	fixtureBlackColor        = "#000000"
	findingOutsideHorizontal = "outside-page-horizontal"
	fixtureLayoutStage       = "layout"
	fixtureOverflowCode      = "text_overflow"
	unknownProducerCommit    = "unknown"
	unknownFindingKind       = "unknown"
	expectedSummaryBytes     = 2048
	testCommandCheck         = "check"
	testCommandBuild         = "build"
	firstArgvID              = "argv:0"
	secondArgvID             = "argv:1"
	detailFormat             = "%+v"
	memberStatusNull         = `"status":null`
	duplicatedKindMembers    = `"kind":"report","kind":"report",`
	memberFontZeroEnd        = `"font":0}`
	namedErrorFormat         = "%s: %v"
	firstItemPointer         = "/items/0"
	itemPointerFormat        = "/items/%d"

	// Names shared by the test files.
	fontName       = "NotoSans-Regular"
	colorWhite     = "#ffffff"
	alignCenter    = "center"
	sourcePath     = "/a.pdf"
	jobFile        = "/work/job.json"
	argvFile       = "argv"
	commandReport  = "report"
	savedReport    = "saved.json"
	jobName        = "job.json"
	pointerStatus  = "/status"
	pointerItemOne = "/items/1"
	pointerItemTwo = "/items/2"

	// JSON fragments of the sample documents that the corpus rows edit.
	memberVersion   = `"report_version":1`
	memberKind      = `"kind":"report",`
	memberStatusOK  = `"status":"ok"`
	memberFontName  = `"name":"NotoSans-Regular"`
	memberBytes     = `"bytes":1234`
	memberItemOneID = `"id":"/items/1"`
	memberArgvIndex = `"argv_index":2`
	memberPagesOf3  = `"pages":3`

	digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// completeCheck is a successful check: one PDF of three pages and one blank run of two pages. It is
	// written out by hand so that it is an oracle independent of the Builder and the encoder.
	completeCheck = `{"report_version":1,"kind":"report","status":"ok","command":"check",` +
		`"phases":{"instructions":"complete","input_inspection":"complete","layout":"complete","output_verification":"not_run"},` +
		`"counts":{"source_pages":3,"generated_pages":2,"total_pages":5},` +
		`"publication":{"report_status":"written","report_path":"/w/r.json","published":false},` +
		`"diagnostics":[],` +
		`"parts":[` +
		`{"id":"/items/0","kind":"pdf","origin":{"file":"/w/job.json","offset":30,"line":2,"column":3},"range":{"start":1,"end":3},` +
		`"pages":3,"source":0},` +
		`{"id":"/items/1","kind":"blank","origin":{"file":"/w/job.json","offset":50,"line":3,"column":3},"range":{"start":4,"end":5},` +
		`"pages":2,"style":0}],` +
		`"sources":[{"path":"/w/a.pdf","digest":"` + digestA + `","bytes":1234}],` +
		`"fonts":[{"digest":"` + digestB + `","name":"NotoSans-Regular"}],` +
		`"styles":[{"background":"#ffffff","size":{"origin":"explicit","width":595,"height":842},` +
		`"text":{"value":"Chapter ā","color":"#000000","anchor":"center","align":"center","overflow":"error","size":12,"x":0,"y":0,` +
		`"width":523,` +
		`"leading":1.2,"bounds":{"x":250,"y":420,"width":60,"height":14},"ink_bounds":null,"font":0}}]}`

	// failedDiagnostics are the two diagnostics of failedCheck: one located by pointer, one by argv index.
	failedDiagnostics = `[{"stage":"inspect","code":"source_unreadable","location":{"file":"/w/job.json","offset":30,"line":2,"column":3,` +
		`"pointer":"/items/0"},"path":"/w/a.pdf","message":"cannot read"},` +
		`{"stage":"usage","code":"bad_flag","location":{"file":"argv","argv_index":2},"message":"bad flag"}]`

	// failedCheck is an incomplete run: layout is unknown, so counts and ranges are null.
	failedCheck = `{"report_version":1,"kind":"report","status":"invalid","command":"check",` +
		`"phases":{"instructions":"complete","input_inspection":"incomplete","layout":"not_run","output_verification":"not_run"},` +
		`"counts":{"source_pages":null,"generated_pages":null,"total_pages":null},` +
		`"publication":{"report_status":"not_requested","published":false},` +
		`"diagnostics":` + failedDiagnostics + `,` +
		`"parts":[{"id":"argv:0","kind":"pdf","origin":{"file":"argv"},"range":null,"pages":null,"source":0}],` +
		`"sources":[{"path":"/w/a.pdf","bytes":null}],"fonts":[],"styles":[]}`

	// richFailure is a failed build that exercises every optional member: published output, a failed report
	// with a recovery file, a font file, a pointer and an argv location, and a path.
	richFailure = `{"report_version":1,"kind":"report","status":"failed","command":"build",` +
		`"phases":{"instructions":"complete","input_inspection":"complete","layout":"complete","output_verification":"complete"},` +
		`"counts":{"source_pages":1,"generated_pages":1,"total_pages":2},` +
		`"publication":{"output":"/w/out.pdf","report_status":"failed","report_path":"/w/r.json",` +
		`"recovery_report":"/w/.rec.json","recovery_state":"current",` +
		`"published":true},` +
		`"diagnostics":[` +
		`{"stage":"publish","code":"report_write_failed","path":"/w/r.json","message":"cannot write the report"},` +
		`{"stage":"shape","code":"plan_bad_value","location":{"file":"/w/job.json","offset":5,"line":1,"column":6,` +
		`"pointer":"/items/1"},"message":"bad"}],` +
		`"parts":[` +
		`{"id":"argv:0","kind":"pdf","origin":{"file":"argv"},"range":{"start":1,"end":1},"pages":1,"source":0},` +
		`{"id":"argv:1","kind":"blank","origin":{"file":"argv","offset":0,"line":1,"column":1},"range":{"start":2,"end":2},"pages":1,` +
		`"style":0}],` +
		`"sources":[{"path":"/w/a.pdf","digest":"` + digestA + `","bytes":10}],` +
		`"fonts":[{"digest":"` + digestB + `","name":"X","file":"/w/x.ttf"}],` +
		`"styles":[{"background":"none","size":{"origin":"preceding_source","width":10,"height":20}}]}`
)

func decodeText(tb testing.TB, text string) (*report.Report, error) {
	tb.Helper()

	return report.Decode(context.Background(), savedReport, strings.NewReader(text))
}

func mustDecode(tb testing.TB, text string) *report.Report {
	tb.Helper()

	decoded, err := decodeText(tb, text)
	if err != nil {
		tb.Fatalf("decode: %v", err)
	}

	return decoded
}

func errorCode(err error) report.Code {
	found, ok := report.AsError(err)
	if !ok {
		return "<not a report error>"
	}

	return found.Diagnostic.Code
}

func encodeReport(tb testing.TB, r *report.Report) string {
	tb.Helper()

	var out bytes.Buffer

	_, err := report.Write(&out, r, report.MaxReportBytes)
	if err != nil {
		tb.Fatalf("write: %v", err)
	}

	return out.String()
}

// replaceOnce edits a document, failing the test when the text to replace is absent so a row cannot pass vacuously.
func replaceOnce(tb testing.TB, doc, old, replacement string) string {
	tb.Helper()

	if strings.Count(doc, old) != 1 {
		tb.Fatalf("%q occurs %d times in the base document", old, strings.Count(doc, old))
	}

	return strings.Replace(doc, old, replacement, 1)
}

// isUsageError reports whether err is a report usage error with the given code and the exit-2 status.
func isUsageError(err error, code report.Code) bool {
	found, ok := report.AsError(err)

	return ok && found.Diagnostic.Code == code && found.Status() == report.StatusInvalid && found.Diagnostic.Stage == report.StageUsage
}

// containsAll reports whether text contains every part.
func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}

	return true
}

func nan() float64 { return math.NaN() }
