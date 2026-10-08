// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

type firstWriteFault struct {
	err   error
	calls int
}

var errHumanSinkClosed = errors.New("human sink closed")

func TestSummaryHumanTextDescribesOnlyActualPreviewTruncation(t *testing.T) {
	t.Parallel()

	for _, long := range []bool{false, true} {
		saved := mustDecode(t, failedCheck)
		if long {
			saved.Diagnostics[0].Cause = strings.Repeat("directory policy; ", 1000)
		}

		saved = mustDecode(t, encodeReport(t, saved))
		summary := saved.Summary()

		var output strings.Builder
		if err := summary.RenderText(&output); err != nil {
			t.Fatal(err)
		}

		hasCue := strings.Contains(output.String(), "truncated previews:")
		if hasCue != long || strings.Contains(output.String(), "truncated previews: []") {
			t.Fatalf("truncation cue must describe actual lost evidence: long=%t text=%s", long, output.String())
		}
	}
}

func TestHumanDiagnosticsRetainTheActualReadFailureCause(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, failedCheck)
	saved.Diagnostics[0].Cause = "permission denied by directory policy"

	saved = mustDecode(t, encodeReport(t, saved))
	for _, request := range []report.Request{{}, {View: report.ViewDiagnostics}} {
		response := queryOK(t, saved, request)

		var output strings.Builder
		if err := response.RenderText(&output); err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(output.String(), "  cause: permission denied by directory policy\n") {
			t.Fatalf("human evidence lost read cause: %s", output.String())
		}
	}
}

func TestHumanDiagnosticsIdentifyInputPathsWithoutUnboundedText(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/work/Rīga/sākums.pdf", "/work/" + strings.Repeat("ļ", 1000) + ".pdf"} {
		saved := mustDecode(t, failedCheck)
		saved.Diagnostics[0].Path = path
		saved.Diagnostics[0].Cause = "source permission denied"

		for _, request := range []report.Request{{}, {View: report.ViewDiagnostics}} {
			response := queryOK(t, saved, request)

			var output strings.Builder
			if err := response.RenderText(&output); err != nil {
				t.Fatal(err)
			}

			assertHumanPath(t, output.String(), path)
		}

		response := queryAs[report.ViewResponse[report.DiagnosticView]](
			t, saved, report.Request{View: report.ViewDiagnostics, Details: true},
		)
		if response.Records[0].Path != path {
			t.Fatal("explicit details lost original path")
		}
	}
}

func assertHumanPath(t *testing.T, text, path string) {
	t.Helper()

	if !strings.Contains(text, "  path: ") || !strings.Contains(text, "source permission denied") {
		t.Fatalf("path or cause omitted: %s", text)
	}

	if len(path) < 200 {
		if !strings.Contains(text, strconv.Quote(path)) {
			t.Fatalf("Unicode input path changed: %s", text)
		}

		return
	}

	cutMarked := strings.Contains(text, "path cut; inspect details for the full path") ||
		strings.Contains(text, "truncated previews: [diagnostics/0/path]")
	if strings.Contains(text, path) || !cutMarked {
		t.Fatalf("long path is unbounded or silently cut: %s", text)
	}
}

func (w *firstWriteFault) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

func TestHumanRendererKeepsItsFirstWriteFailure(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, failedCheck)
	saved.Diagnostics[0].Cause = "permission denied"
	saved = mustDecode(t, encodeReport(t, saved))
	failure := errHumanSinkClosed

	writer := &firstWriteFault{err: failure}
	if err := saved.Summary().RenderText(writer); !errors.Is(err, failure) || writer.calls != 1 {
		t.Fatalf("write failure lost or subsequent writes continued: %v calls=%d", err, writer.calls)
	}
}
