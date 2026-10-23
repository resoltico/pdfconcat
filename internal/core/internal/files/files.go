// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package files owns safe destination roles and native file publication.
package files

import (
	"errors"
	"io/fs"
	"os"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

// Files holds the roles known at this point in a run. Unknown inputs force
// report no-replace even when Overwrite was requested.
type Files struct {
	Output, Report         string
	Inputs                 []string
	Overwrite, InputsKnown bool
}

// Check checks initial roles/destinations. Input read failures belong to their stages.
func (f Files) Check() []diag.Diagnostic {
	ds := f.checkTarget(f.Output, "output", f.Overwrite, identity)
	return append(ds, f.checkReport(false, identity)...)
}

// CheckReport must also run immediately before report publication, after any PDF
// publication, so initially absent aliases cannot overwrite the published PDF.
func (f Files) CheckReport() []diag.Diagnostic { return f.checkReport(true, identity) }

func (f Files) checkReport(final bool, query identityQuery) []diag.Diagnostic {
	ds := f.checkTarget(f.Report, "report", f.Overwrite && f.InputsKnown, query)
	if f.Report != "" && f.Output != "" && !hasNonregularReport(ds) {
		same, err := samePathOrFile(f.Report, f.Output, query)
		if err != nil {
			d := diag.New(diag.ReportInspectionFailed, err.Error())
			d.File = f.Report
			ds = append(ds, d)
		}
		if same {
			d := diag.New(diag.ReportIsOutput, "Report and output must be different files.")
			d.File = f.Report
			ds = append(ds, d)
		}
	}
	if final && f.Report != "" && !roleErrors(ds) {
		for _, input := range f.Inputs {
			reserved, err := reserve(input, query)
			collision, unproved := false, false
			if err == nil && len(reserved.missing) > 0 {
				collision, unproved, err = missingInputRelation(f.Report, reserved, query)
			}
			code := diag.Code{}
			message := ""
			switch {
			case err != nil:
				code = diag.ReportInspectionFailed
				message = err.Error()
			case collision:
				code = diag.ReportIsInput
				message = "Destination aliases an input: " + input
			case unproved:
				code = diag.ReportIdentityUnverified
				message = "Cannot establish report separation from input: " + input + ". Use a different existing report directory."
			}
			if code != (diag.Code{}) {
				d := diag.New(code, message)
				d.File = f.Report
				ds = append(ds, d)
				break
			}
		}
	}
	return ds
}
func hasNonregularReport(ds []diag.Diagnostic) bool {
	for _, d := range ds {
		if d.Code == diag.ReportNotRegular {
			return true
		}
	}
	return false
}
func roleErrors(ds []diag.Diagnostic) bool { status, _ := diag.Outcome(ds); return status != "ok" }

func (f Files) checkTarget(path, role string, overwrite bool, query identityQuery) []diag.Diagnostic {
	if path == "" {
		return nil
	}
	var ds []diag.Diagnostic
	exists, notRegular, isInput, inspection := diag.OutputExists, diag.OutputNotRegular, diag.OutputIsInput, diag.OutputInspectionFailed
	if role == "report" {
		exists, notRegular, isInput, inspection = diag.ReportExists, diag.ReportNotRegular, diag.ReportIsInput, diag.ReportInspectionFailed
	}
	add := func(code diag.Code, message string) { d := diag.New(code, message); d.File = path; ds = append(ds, d) }
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			add(notRegular, "Destination must be a regular file; symlinks, directories and devices are refused.")
		}
		if !overwrite {
			add(exists, "Destination exists; use --overwrite (reports with unknown inputs always require a new path).")
		}
	case !errors.Is(err, fs.ErrNotExist):
		add(inspection, "Cannot inspect destination: "+err.Error())
	}
	if err == nil && !info.Mode().IsRegular() {
		return ds
	}
	for _, input := range f.Inputs {
		same, identityErr := samePathOrFile(path, input, query)
		if identityErr != nil {
			add(inspection, identityErr.Error())
			break
		}
		if same {
			add(isInput, "Destination aliases an input: "+input)
			break
		}
	}
	return ds
}
