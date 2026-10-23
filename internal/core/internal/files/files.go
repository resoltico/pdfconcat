// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package files owns safe destination roles and native file publication.
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

// Roles holds the roles known at this point in a run. Unknown inputs force
// report no-replace even when Overwrite was requested.
type Roles struct {
	Output, Report         string
	Inputs                 []string
	Overwrite, InputsKnown bool
}

// Check checks initial roles/destinations. Input read failures belong to their stages.
func (f Roles) Check() []diag.Diagnostic {
	ds := f.checkTarget(f.Output, "output", f.Overwrite)
	return append(ds, f.checkReport()...)
}

// CheckReport must also run immediately before report publication, after any PDF
// publication, so initially absent aliases cannot overwrite the published PDF.
func (f Roles) CheckReport() []diag.Diagnostic { return f.checkReport() }

func (f Roles) checkReport() []diag.Diagnostic {
	ds := f.checkTarget(f.Report, "report", f.Overwrite && f.InputsKnown)
	if f.Report != "" && f.Output != "" && !hasNonregularReport(ds) {
		same, err := samePathOrFile(f.Report, f.Output)
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

func (f Roles) checkTarget(path, role string, overwrite bool) []diag.Diagnostic {
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
		same, identityErr := samePathOrFile(path, input)
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

// samePathOrFile protects existing bytes by identity and missing inputs by spelling.
func samePathOrFile(a, b string) (bool, error) {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true, nil
	}
	first, err := os.Stat(a)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	second, err := os.Stat(b)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return os.SameFile(first, second), nil
}
