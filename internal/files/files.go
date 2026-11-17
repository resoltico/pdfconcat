// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Package files owns safe destination roles and native file publication.
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
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
	ds := f.checkTarget(f.Output, packet.OutputDestination)
	return append(ds, f.checkReport()...)
}

// CheckReport must also run immediately before report publication, after any PDF
// publication, so initially absent aliases cannot overwrite the published PDF.
func (f Roles) CheckReport() []diag.Diagnostic { return f.checkReport() }

func (f Roles) checkReport() []diag.Diagnostic {
	ds := f.checkTarget(f.Report, packet.ReportDestination)
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

func (f Roles) checkTarget(path string, role packet.DestinationRole) []diag.Diagnostic {
	if path == "" {
		return nil
	}
	facts := packet.DestinationFacts{Path: path, Role: role, Overwrite: f.Overwrite, InputsKnown: f.InputsKnown}
	info, err := os.Lstat(path)
	if err == nil {
		facts.Exists = true
		facts.Regular = info.Mode().IsRegular()
	} else if !errors.Is(err, fs.ErrNotExist) {
		facts.InspectionFailure = err.Error()
	}
	if facts.Exists && !facts.Regular {
		return packet.CheckDestination(facts)
	}
	for _, input := range f.Inputs {
		same, err := samePathOrFile(path, input)
		if err != nil {
			facts.IdentityFailure = err.Error()
			break
		}
		if same {
			facts.AliasedInput = input
			break
		}
	}
	return packet.CheckDestination(facts)
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
