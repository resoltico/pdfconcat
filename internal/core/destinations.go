// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

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
	Role                                                            DestinationRole
	Path                                                            string
	Exists, Regular, Overwrite, InputsKnown                         bool
	InvalidParent, AliasedInput, InspectionFailure, IdentityFailure string
}

// CheckDestination applies product refusal rules to explicit observed facts.
func CheckDestination(f DestinationFacts) []diag.Diagnostic {
	if f.Path == "" {
		return nil
	}
	exists, notRegular, isInput, inspection, parentInvalid := diag.OutputExists, diag.OutputNotRegular, diag.OutputIsInput, diag.OutputInspectionFailed, diag.OutputParentInvalid
	if f.Role == ReportDestination {
		exists, notRegular, isInput, inspection, parentInvalid = diag.ReportExists, diag.ReportNotRegular, diag.ReportIsInput, diag.ReportInspectionFailed, diag.ReportParentInvalid
	}
	var ds []diag.Diagnostic
	add := func(code diag.Code, message string) {
		d := diag.New(code, message)
		d.File = f.Path
		ds = append(ds, d)
	}
	switch {
	case f.Exists && !f.Regular:
		add(notRegular, "Destination must be a regular file. Choose a different path.")
	case f.InvalidParent != "":
		add(parentInvalid, fmt.Sprintf("Destination parent %q must be an existing directory. Choose an existing directory or create it.", f.InvalidParent))
	case !f.Exists && f.InspectionFailure != "":
		add(inspection, "Cannot inspect destination: "+f.InspectionFailure)
	case f.IdentityFailure != "":
		add(inspection, f.IdentityFailure)
	case f.AliasedInput != "":
		add(isInput, fmt.Sprintf("Destination must use a distinct file from input %q. Choose a different filename or directory.", f.AliasedInput))
	case f.Exists && f.Role == ReportDestination && !f.InputsKnown:
		add(exists, "Report destination exists and the plan inputs are unknown. Choose a new report path.")
	case f.Exists && !MayReplace(f.Role, f.Overwrite, f.InputsKnown):
		add(exists, "Destination exists. Choose a new path or use --overwrite to authorize replacement.")
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
