// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import "unicode/utf8"

// ContinuationReference reconstructs an omitted argv from caller-owned invocation data.
// Executable means the current invoking executable, never the saved report's producer.
// Report means --report's value for a job, or the report command's FILE operand.
// Relative arguments resolve in that invocation's working directory.
type ContinuationReference struct {
	Executable string `json:"executable_from"`
	Report     string `json:"report_from"`
	Action     string `json:"action"`
	View       string `json:"view"`
	AttemptID  string `json:"expect_attempt"`
}

// BindContinuation supplies exact invocation authority after projecting a saved report.
func (s *Summary) BindContinuation(executable, path, reference string) {
	s.Next = nil
	s.NextOmitted = false
	s.NextReference = nil

	if path == "" {
		return
	}

	view := ViewParts
	if s.DiagnosticCount > 0 {
		view = ViewDiagnostics
	}

	next := []string{executable, reportCommand, path, expectAttemptOption, s.AttemptID, "--view", view}
	s.Next = &next

	referenceValue := &ContinuationReference{
		Executable: invokingExecutableReference,
		Report:     reference,
		Action:     recoveryInspectReport,
		View:       view,
		AttemptID:  s.AttemptID,
	}
	if executable == "" || !utf8.ValidString(executable) || !utf8.ValidString(path) {
		s.Next = nil
		s.NextOmitted = true
	}

	s.bound()

	if s.NextOmitted {
		s.NextReference = referenceValue
		s.bound()
	}
}
