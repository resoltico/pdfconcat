// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
	"github.com/resoltico/pdfconcat/internal/files"
	result "github.com/resoltico/pdfconcat/internal/wire"
)

func writeReport(r *outcome.Result, roles files.Roles, records resultRecords) {
	if r.Report == nil {
		return
	}
	refusals := roles.CheckReport()
	r.Diagnostics = append(r.Diagnostics, refusals...)
	if hasErrors(refusals) {
		return
	}
	full := *r
	report := *r.Report
	report.Written = true
	full.Report = &report
	staged, err := files.Create(roles.Report)
	if err == nil {
		defer abortStage(staged, packet.ReportDestination, roles.Report, &r.Diagnostics)
		err = result.WriteReport(staged, full, records.partRecords(records.parts), records.sectionRecords(records.sections))
		if err == nil {
			var refusals []diag.Diagnostic
			r.Report.Written, refusals, err = roles.Publish(staged, packet.ReportDestination)
			r.Diagnostics = append(r.Diagnostics, refusals...)
			if err != nil {
				r.Diagnostics = append(r.Diagnostics, publicationDiagnostic(packet.ReportDestination, roles.Report, roles.InputsKnown, r.Report.Written, err))
			}
			return
		}
	}
	if err != nil {
		d := ioDiagnostic(diag.ReportWriteFailed, err, roles.Report)
		r.Diagnostics = append(r.Diagnostics, d)
	}
}

func abortStage(staged *files.Staged, role packet.DestinationRole, path string, findings *[]diag.Diagnostic) {
	if err := staged.Abort(); err != nil {
		code := diag.OutputPublishFailed
		if role == packet.ReportDestination {
			code = diag.ReportWriteFailed
		}
		*findings = append(*findings, diag.StageCleanupFailure(code, path, err.Error()))
	}
}
