// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package pipeline coordinates plan checking, PDF building and result publication.
package pipeline

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/diag"
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
	ds := f.checkTarget(f.Output, "output", f.Overwrite)
	return append(ds, f.CheckReport()...)
}

// CheckReport must also run immediately before report publication, after any PDF
// publication, so initially absent aliases cannot overwrite the published PDF.
func (f Files) CheckReport() []diag.Diagnostic {
	ds := f.checkTarget(f.Report, "report", f.Overwrite && f.InputsKnown)
	if f.Report != "" && f.Output != "" && samePathOrFile(f.Report, f.Output) {
		d := diag.New("report_is_output", "Report and output must be different files.")
		d.File = f.Report
		ds = append(ds, d)
	}
	return ds
}

func (f Files) checkTarget(path, role string, overwrite bool) []diag.Diagnostic {
	if path == "" {
		return nil
	}
	var ds []diag.Diagnostic
	add := func(code diag.Code, message string) { d := diag.New(code, message); d.File = path; ds = append(ds, d) }
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			add(diag.Code(role+"_not_regular"), "Destination must be a regular file; symlinks, directories and devices are refused.")
		}
		if !overwrite {
			add(diag.Code(role+"_exists"), "Destination exists; use --overwrite (reports with unknown inputs always require a new path).")
		}
	case !errors.Is(err, fs.ErrNotExist):
		add(diag.Code(role+"_write_failed"), "Cannot inspect destination: "+err.Error())
	}
	for _, input := range f.Inputs {
		if samePathOrFile(path, input) {
			add(diag.Code(role+"_is_input"), "Destination aliases an input: "+input)
			break
		}
	}
	return ds
}

func samePathOrFile(a, b string) bool {
	aa, e1 := filepath.Abs(a)
	bb, e2 := filepath.Abs(b)
	if e1 == nil && e2 == nil && aa == bb {
		return true
	}
	first, e1 := os.Stat(a)
	second, e2 := os.Stat(b)
	return e1 == nil && e2 == nil && os.SameFile(first, second)
}
