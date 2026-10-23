// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"encoding/json/v2"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

func writeReport(r *result.Result, roles files.Files) {
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
	data, err := json.Marshal(full)
	if err == nil {
		var staged *files.File
		staged, err = files.Create(roles.Report)
		if err == nil {
			defer staged.Abort()
			_, err = staged.Write(append(data, '\n'))
			if err == nil {
				r.Report.Written, err = staged.Commit(roles.Overwrite && roles.InputsKnown)
			}
		}
	}
	if err != nil {
		d := diag.New(diag.ReportWriteFailed, err.Error())
		d.File = roles.Report
		r.Diagnostics = append(r.Diagnostics, d)
	}
}
