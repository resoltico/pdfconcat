// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"errors"
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
