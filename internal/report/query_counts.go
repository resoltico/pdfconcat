// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

// Validate checks the query operation's diagnostic scope separately from its captured evidence.
// It does not authenticate artifacts or replace complete-report validation before selection.
func (q *QueryResult) Validate() error {
	captured := DiagnosticCounts{
		DiagnosticCount: q.SavedRun.DiagnosticCount,
		ErrorCount:      q.SavedRun.ErrorCount,
		WarningCount:    q.SavedRun.WarningCount,
	}

	operation := DiagnosticCounts{DiagnosticCount: q.DiagnosticCount, ErrorCount: q.ErrorCount, WarningCount: q.WarningCount}
	if !validQueryOperation(q) || operation != (DiagnosticCounts{}) {
		return queryCountError("a successful query has no operation diagnostic records")
	}

	if !validDiagnosticCounts(captured) || !validDiagnosticCounts(q.SelectionCounts) {
		return queryCountError("diagnostic_count must equal error_count plus warning_count in each scope")
	}

	if !validCapturedOutcome(q.SavedRun.Status, captured) {
		return queryCountError("saved-run status disagrees with its captured errors")
	}

	if q.SelectionCounts.ErrorCount > captured.ErrorCount || q.SelectionCounts.WarningCount > captured.WarningCount {
		return queryCountError("selected records exceed the saved-run diagnostic scope")
	}

	return q.validateSelectedCounts(captured)
}

func validDiagnosticCounts(counts DiagnosticCounts) bool {
	return counts.DiagnosticCount >= 0 && counts.ErrorCount >= 0 && counts.WarningCount >= 0 &&
		counts.ErrorCount <= counts.DiagnosticCount && counts.WarningCount == counts.DiagnosticCount-counts.ErrorCount
}

func queryCountError(message string) error {
	return newError(StatusFailed, StageWrite, CodeInvalidValue, nil, nil, "query outcome invariant: %s", message)
}

func (q *QueryResult) validateSelectedCounts(captured DiagnosticCounts) error {
	switch selected := q.Result.content.(type) {
	case *Summary:
		counts := DiagnosticCounts{
			DiagnosticCount: selected.DiagnosticCount,
			ErrorCount:      selected.ErrorCount,
			WarningCount:    selected.WarningCount,
		}
		if counts != captured || q.SelectionCounts != captured {
			return queryCountError("summary counts disagree with the captured run")
		}
	case *ViewResponse[DiagnosticView]:
		counts, err := diagnosticViewCounts(selected.Records)
		if err != nil {
			return err
		}

		if counts != q.SelectionCounts {
			return queryCountError("selected-record counts disagree with their severities")
		}

	case *PartResponse:
		return selectedWarningCountFault(selected.Part.WarningCount, q.SelectionCounts.WarningCount)
	case *PageResponse:
		return selectedWarningCountFault(selected.Part.WarningCount, q.SelectionCounts.WarningCount)
	case *ViewResponse[PartView]:
		// Unique relevant records were counted against the captured consumer relationships.
	default:
		return queryCountError("query result is not a supported report selection")
	}

	return nil
}

func selectedWarningCountFault(actual, expected int) error {
	if actual != expected {
		return queryCountError("part/page warning count disagrees with its selection scope")
	}

	return nil
}

func diagnosticViewCounts(records []DiagnosticView) (DiagnosticCounts, error) {
	counts := DiagnosticCounts{DiagnosticCount: len(records)}
	for i := range records {
		switch records[i].Severity {
		case SeverityError:
			counts.ErrorCount++
		case SeverityWarning:
			counts.WarningCount++
		default:
			return DiagnosticCounts{}, queryCountError("selected record has unsupported severity")
		}
	}

	return counts, nil
}

func validQueryOperation(query *QueryResult) bool {
	return query.FormatVersion == Version && query.Kind == "report_query" && query.Command == reportCommand && query.Status == StatusOK
}

func validCapturedOutcome(status Status, counts DiagnosticCounts) bool {
	switch status {
	case StatusOK:
		return counts.ErrorCount == 0
	case StatusFailed, StatusInvalid, StatusInterrupted:
		return counts.ErrorCount > 0
	default:
		return false
	}
}
