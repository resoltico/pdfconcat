// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"fmt"
	"io"
	"strings"
)

type (
	// SavedRun identifies the captured attempt, independent of the current read-only query.
	SavedRun struct {
		AttemptID string `json:"attempt_id"`
		Command   string `json:"command"`
		Status    Status `json:"status"`
	}

	// QueryResult records successful selection from a saved attempt. Result is captured evidence;
	// any publication target inside it is historical, never the file selected by this query.
	QueryResult struct {
		Kind          string   `json:"kind"`
		Command       string   `json:"command"`
		Status        Status   `json:"status"`
		SavedRun      SavedRun `json:"saved_run"`
		Result        Response `json:"result"`
		FormatVersion int      `json:"format_version"`
	}
)

// QueryResultOf binds summary navigation to the actual file and current executable.
func QueryResultOf(saved *Report, response Response, executable, path string) *QueryResult {
	result := &QueryResult{
		FormatVersion: Version, Kind: "report_query", Command: reportCommand, Status: StatusOK,
		SavedRun: SavedRun{AttemptID: saved.AttemptID, Command: saved.Command, Status: saved.Status}, Result: response,
	}

	if summary, ok := ContentOf[Summary](response); ok {
		summary.byteLimit = SummaryBytes - result.summaryOverhead(summary)

		summary.PublicationContext = "historical_target"
		if summary.RecoveryDirectoryFrom != "" {
			summary.RecoveryDirectoryFrom = historicalRecoveryReference
		}

		summary.BindContinuation(executable, path, queryReportReference)
		summary.bound()
	}

	if view, ok := ContentOf[ViewResponse[PartView]](response); ok {
		boundQueryPage(result, view)
	}

	if view, ok := ContentOf[ViewResponse[DiagnosticView]](response); ok {
		boundQueryPage(result, view)
	}

	return result
}

// RenderText distinguishes successful querying from the saved job's outcome.
func (q *QueryResult) RenderText(w io.Writer) error {
	if err := q.renderHeading(w); err != nil {
		return err
	}

	return q.Result.RenderText(w)
}

// summaryOverhead measures the actual JSON wrapper and text heading rather than reserving a guessed allowance.
func (q *QueryResult) summaryOverhead(summary *Summary) int {
	wrapped, wrappedErr := Encode(q)

	inner, innerErr := Encode(summary)
	if wrappedErr != nil || innerErr != nil {
		return 0
	}

	return max(len(wrapped)-len(inner), len(q.heading()))
}

// boundQueryPage includes the query envelope in both transport budgets and advances by complete records.
func boundQueryPage[T any](query *QueryResult, view *ViewResponse[T]) {
	for len(view.Records) > 0 && !query.fits(MaxResponseBytes) {
		if len(view.Records) == 1 {
			view.OversizedRecord = true
			return
		}

		view.Records = view.Records[:len(view.Records)-1]
		view.setPageEnd()
	}
}

func (q *QueryResult) fits(limit int) bool {
	encoded, err := Encode(q)
	if err != nil || len(encoded)+1 > limit {
		return false
	}

	var text strings.Builder

	err = q.RenderText(&text)

	return err == nil && text.Len() <= limit
}

func (q *QueryResult) renderHeading(w io.Writer) error {
	_, err := io.WriteString(w, q.heading())
	if err != nil {
		return fmt.Errorf("write query heading: %w", err)
	}

	return nil
}

func (q *QueryResult) heading() string {
	return fmt.Sprintf(
		"report query ok; saved attempt %s: %s %s (publication targets are historical)\n",
		q.SavedRun.AttemptID,
		q.SavedRun.Command,
		q.SavedRun.Status,
	)
}
