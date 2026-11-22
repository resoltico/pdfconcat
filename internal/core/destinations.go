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
	notRegular, isInput, inspection, parentInvalid := diag.OutputNotRegular, diag.OutputIsInput, diag.OutputInspectionFailed, diag.OutputParentInvalid
	if f.Role == ReportDestination {
		notRegular, isInput, inspection, parentInvalid = diag.ReportNotRegular, diag.ReportIsInput, diag.ReportInspectionFailed, diag.ReportParentInvalid
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
	case f.Exists && !MayReplace(f.Role, f.Overwrite, f.InputsKnown):
		ds = append(ds, destinationExists(f.Role, f.Path, f.InputsKnown,
			"Destination exists. Choose a new path or use --overwrite to authorize replacement."))
	}
	return ds
}

// PublicationDestinationExists explains occupancy proved by atomic no-replace.
func PublicationDestinationExists(role DestinationRole, path string, inputsKnown bool) diag.Diagnostic {
	return destinationExists(role, path, inputsKnown, "Destination already exists. Choose a new path.")
}

func destinationExists(role DestinationRole, path string, inputsKnown bool, message string) diag.Diagnostic {
	code := diag.OutputExists
	if role == ReportDestination {
		code = diag.ReportExists
		if !inputsKnown {
			message = "Report destination exists and the plan inputs are unknown. Choose a new report path."
		}
	}
	d := diag.New(code, message)
	d.File = path
	return d
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
