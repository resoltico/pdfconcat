// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"encoding/json/v2"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
	"github.com/resoltico/pdfconcat/internal/files"
	result "github.com/resoltico/pdfconcat/internal/wire"
)

func writeReport(r *outcome.Result, roles files.Roles) {
	if r.Report == nil {
		return
	}
	refusals := roles.CheckReport()
	r.Diagnostics = append(r.Diagnostics, refusals...)
	if hasErrors(refusals) {
		return
	}
	full := r.Full()
	report := *r.Report
	report.Written = true
	full.Report = &report
	data, err := json.Marshal(result.Project(full))
	if err == nil {
		var staged *files.Staged
		staged, err = files.Create(roles.Report)
		if err == nil {
			defer staged.Abort()
			_, err = staged.Write(append(data, '\n'))
			if err == nil {
				r.Report.Written, err = staged.Commit(packet.MayReplace(packet.ReportDestination, roles.Overwrite, roles.InputsKnown))
			}
		}
	}
	if err != nil {
		d := ioDiagnostic(diag.ReportWriteFailed, err, roles.Report)
		r.Diagnostics = append(r.Diagnostics, d)
	}
}
