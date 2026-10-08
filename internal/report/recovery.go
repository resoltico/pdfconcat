// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

// Recovery is guidance, never an automatically executed action. Location and references
// preserve the caller's declaration rather than constructing a shell command.
type Recovery struct {
	LocationFrom string    `json:"location_from,omitempty"`
	Action       string    `json:"action"`
	Command      string    `json:"command"`
	Location     *Location `json:"location,omitempty"`
	Replacement  string    `json:"replacement,omitempty"`
	ReportFrom   string    `json:"report_from,omitempty"`
	RecoveryFrom string    `json:"recovery_from,omitempty"`
}

const (
	recoveryChooseNewReport     = "choose_new_report"
	recoveryInspectReport       = "inspect_report"
	invokingExecutableReference = "invoking_executable"
	jobReportReference          = "original_argv.--report"
	queryReportReference        = "original_argv.report_operand"
	historicalRecoveryReference = "complete_report.publication.recovery_report"
)
