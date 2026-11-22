// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Package files owns safe destination roles and native file publication.
package files

import (
	"errors"
	"fmt"
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
	_, ds := f.inspectTarget(f.Output, packet.OutputDestination, true)
	return append(ds, f.CheckReport()...)
}

// CheckOutput checks aliases again against paths as observed at publication.
func (f Roles) CheckOutput() []diag.Diagnostic {
	_, ds := f.inspectTarget(f.Output, packet.OutputDestination, false)
	return ds
}

// CheckReport checks the report against inputs and the output.
func (f Roles) CheckReport() []diag.Diagnostic {
	_, ds := f.inspectReport()
	return ds
}

// Publish validates the destination against its role, applies the permissions
// from that observation, and commits the stage.
func (f Roles) Publish(staged *Staged, role packet.DestinationRole) (bool, []diag.Diagnostic, error) {
	path := f.Output
	if role == packet.ReportDestination {
		path = f.Report
	}
	if path == "" || path != staged.target {
		return false, nil, fmt.Errorf("stage does not match publication destination %q", path)
	}
	var target pathEntry
	var ds []diag.Diagnostic
	if role == packet.ReportDestination {
		target, ds = f.inspectReport()
	} else {
		target, ds = f.inspectTarget(path, role, false)
	}
	if len(ds) != 0 {
		return false, ds, nil
	}
	overwrite := packet.MayReplace(role, f.Overwrite, f.InputsKnown)
	mode := staged.creationMode
	if overwrite && target.file != nil {
		mode = target.file.Mode().Perm()
	}
	if err := staged.setMode(mode); err != nil {
		return false, nil, err
	}
	published, err := staged.Commit(overwrite)
	return published, nil, err
}

func (f Roles) inspectReport() (pathEntry, []diag.Diagnostic) {
	target, ds := f.inspectTarget(f.Report, packet.ReportDestination, false)
	if f.Report == "" || f.Output == "" || len(ds) > 0 && ds[0].Code != diag.ReportExists {
		return target, ds
	}
	if filepath.Clean(f.Report) == filepath.Clean(f.Output) {
		return target, packet.CheckReportSeparation(true, f.Report)
	}
	output, err := observeEntry(f.Output, make(map[string]fs.FileInfo))
	if err != nil {
		d := diag.Report(diag.ReportInspectionFailed, diag.PublicationInspectionFailure(err.Error()))
		d.File = f.Report
		return target, []diag.Diagnostic{d}
	}
	if target.aliases(output, true) {
		return target, packet.CheckReportSeparation(true, f.Report)
	}
	return target, ds
}

func (f Roles) inspectTarget(path string, role packet.DestinationRole, preflightPDF bool) (pathEntry, []diag.Diagnostic) {
	if path == "" {
		return pathEntry{}, nil
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
		return pathEntry{}, packet.CheckDestination(facts)
	}
	info, err := os.Lstat(path)
	if err == nil {
		facts.Exists = true
		facts.Regular = info.Mode().IsRegular()
	} else if !errors.Is(err, fs.ErrNotExist) {
		facts.InspectionFailure = err.Error()
	}
	if facts.Exists && !facts.Regular {
		return pathEntry{}, packet.CheckDestination(facts)
	}
	if facts.Exists {
		for _, input := range f.AcceptedSources {
			if os.SameFile(info, input.info) {
				facts.AliasedInput = input.path
				return pathEntry{}, packet.CheckDestination(facts)
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
	return target, packet.CheckDestination(facts)
}

func missingEntry(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotDirectory)
}
