// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
)

const findingWideWord = "word-wider-than-box"

func TestReportCodecPreservesInclusiveMetadataLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		edit func(*report.Report)
		name string
	}{
		{name: "largest count sum", edit: func(r *report.Report) {
			incompleteLayout(r)
			r.Counts = report.Counts{
				SourcePages:    new(int64(math.MaxInt64 - 1)),
				GeneratedPages: new(int64(1)),
				TotalPages:     new(int64(math.MaxInt64)),
			}
		}},
		{name: "empty source", edit: func(r *report.Report) { r.Sources[0].Bytes = new(int64(0)) }},
		{name: "largest page side", edit: func(r *report.Report) { r.Styles[0].Size.Width = 14400 }},
		{name: "largest blank extent", edit: func(r *report.Report) {
			incompleteLayout(r)
			r.Parts[1].Pages = new(int64(assembly.MaxBlankCount))
			r.Parts[1].Range = nil
		}},
		{name: "largest findings table", edit: func(r *report.Report) {
			r.Styles[0].Text.Findings = make([]report.TextFinding, report.MaxTextRunes)
			for i := range r.Styles[0].Text.Findings {
				r.Styles[0].Text.Findings[i] = report.TextFinding{Kind: "outside-page-horizontal", Line: -1, Detail: "ink exceeds page"}
			}
		}},
		{name: "last text line", edit: func(r *report.Report) {
			r.Styles[0].Text.Findings = []report.TextFinding{
				{Kind: findingWideWord, Line: report.MaxTextRunes - 1, Detail: "word exceeds wrap width"},
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			saved := mustDecode(t, completeCheck)
			tc.edit(saved)
			encoded := encodeReport(t, saved)

			restored := mustDecode(t, encoded)
			if got := encodeReport(t, restored); got != encoded {
				t.Fatal("codec changed accepted boundary metadata")
			}
		})
	}
}

func TestReportCodecRejectsExclusiveReferenceAndFindingLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		pointer string
		edit    func(*report.Report)
		code    report.Code
	}{
		{
			"source index after last row",
			"/parts/0/source",
			func(r *report.Report) { r.Parts[0].Source = new(len(r.Sources)) },
			report.CodeDanglingReference,
		},
		{
			"style index after last row",
			"/parts/1/style",
			func(r *report.Report) { r.Parts[1].Style = new(len(r.Styles)) },
			report.CodeDanglingReference,
		},
		{"text line after last allowed line", "/styles/0/text/findings/0", func(r *report.Report) {
			r.Styles[0].Text.Findings = []report.TextFinding{
				{Kind: findingWideWord, Line: report.MaxTextRunes, Detail: "word exceeds wrap width"},
			}
		}, report.CodeInvalidValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			saved := mustDecode(t, completeCheck)
			tc.edit(saved)
			assertCodecFault(t, saved, tc.pointer, tc.code)
		})
	}
}

func assertCodecFault(t *testing.T, saved *report.Report, pointer string, code report.Code) {
	t.Helper()
	// Raw encoding deliberately sends an invalid complete document to the public decoder;
	// Write's validation must not conceal the decoder's own reference/relationship checks.
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}

	_, err = decodeText(t, string(encoded))

	found, ok := report.AsError(err)
	if !ok || found.Diagnostic.Code != code || found.Diagnostic.Location == nil || found.Diagnostic.Location.Pointer != pointer {
		t.Fatalf("decoder fault = %v; want %s at %s", err, code, pointer)
	}
}

func TestRecoveryReferenceRequiresBothPublicationFacts(t *testing.T) {
	t.Parallel()

	for _, change := range []struct {
		edit func(*report.Report)
		name string
	}{
		{name: "report publication succeeded", edit: func(r *report.Report) { r.Publication.ReportStatus = report.ReportWritten }},
		{name: "PDF was never published", edit: func(r *report.Report) { r.Publication.Published = false }},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()

			saved := mustDecode(t, failedCheck)
			saved.Publication = report.Publication{
				Published:      true,
				Output:         "/output.pdf",
				ReportStatus:   report.ReportFailed,
				ReportPath:     "/report.json",
				RecoveryReport: "/retained.json",
				RecoveryState:  report.RecoveryCurrent,
			}
			mustDecode(t, encodeReport(t, saved))
			change.edit(saved)
			assertCodecFault(t, saved, "/publication/recovery_report", report.CodeInvalidValue)
		})
	}
}
