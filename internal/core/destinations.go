// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import "github.com/resoltico/pdfconcat/internal/core/diag"

// DestinationRole is a product publication role, independent of native identity.
type DestinationRole uint8

// Publication roles used by the native adapter.
const (
	OutputDestination DestinationRole = iota
	ReportDestination
)

// MayReplace is shared by role admission and the actual staged-file commit.
func MayReplace(role DestinationRole, overwrite, inputsKnown bool) bool {
	return overwrite && (role == OutputDestination || inputsKnown)
}

// DestinationFacts are observations supplied by the native identity adapter.
type DestinationFacts struct {
	Role                                             DestinationRole
	Path                                             string
	Exists, Regular, Overwrite, InputsKnown          bool
	AliasedInput, InspectionFailure, IdentityFailure string
}

// CheckDestination applies product refusal rules to explicit observed facts.
func CheckDestination(f DestinationFacts) []diag.Diagnostic {
	if f.Path == "" {
		return nil
	}
	exists, notRegular, isInput, inspection := diag.OutputExists, diag.OutputNotRegular, diag.OutputIsInput, diag.OutputInspectionFailed
	if f.Role == ReportDestination {
		exists, notRegular, isInput, inspection = diag.ReportExists, diag.ReportNotRegular, diag.ReportIsInput, diag.ReportInspectionFailed
	}
	var ds []diag.Diagnostic
	add := func(code diag.Code, message string) {
		d := diag.New(code, message)
		d.File = f.Path
		ds = append(ds, d)
	}
	if f.Exists {
		if !f.Regular {
			add(notRegular, "Destination must be a regular file; symlinks, directories and devices are refused.")
		}
		if !MayReplace(f.Role, f.Overwrite, f.InputsKnown) {
			add(exists, "Destination exists; authorize replacement or choose a new path. Reports with unknown inputs require a new path.")
		}
	} else if f.InspectionFailure != "" {
		add(inspection, "Cannot inspect destination: "+f.InspectionFailure)
	}
	if f.Exists && !f.Regular {
		return ds
	}
	if f.IdentityFailure != "" {
		add(inspection, f.IdentityFailure)
	} else if f.AliasedInput != "" {
		add(isInput, "Destination aliases an input: "+f.AliasedInput)
	}
	return ds
}

// CheckReportSeparation owns the product rule after native identity observation.
func CheckReportSeparation(same bool, path string) []diag.Diagnostic {
	if !same {
		return nil
	}
	d := diag.Report(diag.ReportIsOutput, diag.ReportAndOutputMustBeDifferentFiles())
	d.File = path
	return []diag.Diagnostic{d}
}
