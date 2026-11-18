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

// InputIdentity records the native identity a regular-file acquisition observed.
type InputIdentity struct {
	path string
	info fs.FileInfo
}

// Roles holds the roles known at this point in a run. Unknown inputs force
// report no-replace even when Overwrite was requested.
type Roles struct {
	Output, Report         string
	Inputs                 []string
	AcceptedSources        []InputIdentity
	Overwrite, InputsKnown bool
}

// Check checks initial roles/destinations. Input read failures belong to their stages.
func (f Roles) Check() []diag.Diagnostic {
	ds := f.checkTarget(f.Output, packet.OutputDestination, true)
	return append(ds, f.checkReport()...)
}

// CheckOutput checks aliases again against paths as observed at publication.
func (f Roles) CheckOutput() []diag.Diagnostic {
	return f.checkTarget(f.Output, packet.OutputDestination, false)
}

// CheckReport must also run immediately before report publication, after any PDF
// publication, so initially absent aliases cannot overwrite the published PDF.
func (f Roles) CheckReport() []diag.Diagnostic { return f.checkReport() }

func (f Roles) checkReport() []diag.Diagnostic {
	ds := f.checkTarget(f.Report, packet.ReportDestination, false)
	if f.Report == "" || f.Output == "" || len(ds) > 0 && ds[0].Code != diag.ReportExists {
		return ds
	}
	same, err := samePathOrFile(f.Report, f.Output)
	if err != nil {
		d := diag.Report(diag.ReportInspectionFailed, diag.PublicationInspectionFailure(err.Error()))
		d.File = f.Report
		return []diag.Diagnostic{d}
	}
	if same {
		return packet.CheckReportSeparation(same, f.Report)
	}
	return ds
}

func (f Roles) checkTarget(path string, role packet.DestinationRole, preflightPDF bool) []diag.Diagnostic {
	if path == "" {
		return nil
	}
	facts := packet.DestinationFacts{Path: path, Role: role, Overwrite: f.Overwrite, InputsKnown: f.InputsKnown}
	parent := filepath.Dir(path)
	dir, err := os.Stat(parent)
	switch {
	case missingEntry(err) || err == nil && !dir.IsDir():
		facts.InvalidParent = parent
	case err != nil:
		facts.InspectionFailure = err.Error()
	}
	if err != nil || facts.InvalidParent != "" {
		return packet.CheckDestination(facts)
	}
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
	if facts.Exists {
		for _, input := range f.AcceptedSources {
			if os.SameFile(info, input.info) {
				facts.AliasedInput = input.path
				return packet.CheckDestination(facts)
			}
		}
	}
	target := pathEntry{file: info, parent: dir, parentPath: parent, leaf: filepath.Base(path)}
	parents := map[string]fs.FileInfo{parent + string(os.PathSeparator): dir}
	for _, input := range f.Inputs {
		if filepath.Clean(input) == path {
			facts.AliasedInput = input
			break
		}
		entry, err := observeEntry(input, parents)
		if err != nil {
			// A new PDF cannot be published without acquiring its inputs.
			// Let that stage diagnose inaccessible inputs during preflight.
			if preflightPDF && !facts.Exists {
				continue
			}
			facts.IdentityFailure = err.Error()
			break
		}
		// The PDF's inputs must exist before publication, so a future
		// DOS alias alone need not replace a missing-source diagnosis.
		if target.aliases(entry, !preflightPDF) {
			facts.AliasedInput = input
			break
		}
	}
	return packet.CheckDestination(facts)
}

func missingEntry(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotDirectory)
}
