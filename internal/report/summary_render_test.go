// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

// programName is the first word of the command a summary suggests.
const programName = "pdfconcat"

func TestSummaryPointsAtTheSavedReportsPartsWhenThereIsNothingToFix(t *testing.T) {
	t.Parallel()

	summary := mustDecode(t, completeCheck).Summary()
	summary.BindContinuation(programName, fixtureReportPath, originalReportReference)

	want := []string{programName, commandReport, "/w/r.json", fixtureExpectAttemptFlag, fixtureAttemptID, fixtureViewFlag, report.ViewParts}
	if !slices.Equal(nextArguments(summary.Next), want) {
		t.Errorf("next = %v, want %v", summary.Next, want)
	}
}

func TestSummaryNamesTheSavedReportOnlyWhenItWasWritten(t *testing.T) {
	t.Parallel()

	summary := mustDecode(t, completeCheck).Summary()
	summary.BindContinuation(programName, fixtureReportPath, originalReportReference)

	var output strings.Builder
	if err := summary.RenderText(&output); err != nil {
		t.Fatal(err)
	}

	text := output.String()
	if !strings.Contains(text, "--expect-attempt AAAAAAAAAAAAAAAAAAAAAAAAAA --view parts") {
		t.Errorf("missing bound navigation: %s", text)
	}

	unsaved := mustDecode(t, failedCheck)
	if len(nextArguments(unsaved.Summary().Next)) != 0 {
		t.Errorf("next = %v for a report that was not requested", unsaved.Summary().Next)
	}

	if text = renderQuery(t, unsaved, report.Request{}); strings.Contains(text, "more:") {
		t.Errorf("a summary without a saved report has no pointer to one:\n%s", text)
	}
}

func TestDetailedBlankPartRendersItsWholeStyleLine(t *testing.T) {
	t.Parallel()

	failed, longText := syntheticFailure()

	got := renderQuery(t, failed, report.Request{Part: new(pointerItemOne), Details: true})

	var decoded report.PartResponse
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Part.Style.Text.Value != longText || decoded.Part.Style.Text.Font.Name != fontName || decoded.Part.Style.Text.Width != 523 {
		t.Fatalf("incomplete human details: %+v", decoded.Part.Style.Text)
	}
}

func TestDirectDetailedRecordRenderersKeepAllValues(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, completeCheck)
	for _, request := range []report.Request{{Part: new(firstItemPointer), Details: true}, {Page: new(int64(4)), Details: true}} {
		response, err := saved.Query(request)
		if err != nil {
			t.Fatal(err)
		}

		var output strings.Builder
		if part, ok := report.ContentOf[report.PartResponse](response); ok {
			err = part.RenderText(&output)
		} else if page, pageOK := report.ContentOf[report.PageResponse](response); pageOK {
			err = page.RenderText(&output)
		}

		if err != nil {
			t.Fatal(err)
		}

		if !json.Valid([]byte(output.String())) {
			t.Fatal("complete detail is not readable JSON")
		}
	}
}

func TestBriefOversizedPathStillAdvancesAndExplainsItsSize(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, completeCheck)
	saved.Sources[0].Path = "/" + strings.Repeat("x", 200000)

	response, err := saved.Query(report.Request{View: report.ViewParts})
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err = response.RenderText(&output); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output.String(), "larger than the response bound") {
		t.Fatal("oversized brief row not explained")
	}

	view, ok := report.ContentOf[report.ViewResponse[report.PartView]](response)
	if !ok || view.NextOffset == nil || *view.NextOffset != 1 {
		t.Fatal("oversized brief row did not advance")
	}
}
