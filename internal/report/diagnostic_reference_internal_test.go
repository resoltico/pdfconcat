// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import "testing"

const referenceArgvID = "argv:4"

func TestArgumentLocationsArePreciseAndMalformedIDsDoNotInventIndices(t *testing.T) {
	t.Parallel()

	part := Part{ID: referenceArgvID, Origin: Position{File: featureArgv}}

	location := partLocation(&part)
	if location.ArgvIndex == nil || *location.ArgvIndex != 4 || location.Pointer != "" {
		t.Fatalf("argument location lost original index: %+v", location)
	}

	part.ID = "argv:bad"

	location = partLocation(&part)
	if location.ArgvIndex != nil || location.Pointer != part.ID {
		t.Fatal("malformed argument identifier invented a valid location")
	}

	saved := nodeSamples()[0]

	saved.Parts[0].ID = part.ID
	if err := saved.Validate(); err == nil {
		t.Fatal("malformed declaration identifier accepted by report validation")
	}
}

func TestDiagnosticRelationsMatchArgumentDeclarationsWithoutGuessingConsumers(t *testing.T) {
	t.Parallel()

	index := 4

	diagnostic := Diagnostic{Severity: SeverityWarning, Location: &Location{File: featureArgv, ArgvIndex: &index}}
	if !diagnosticRefersTo(&diagnostic, []string{referenceArgvID}) {
		t.Fatal("argument declaration failed to select its warning")
	}

	if diagnosticRefersTo(&diagnostic, []string{"argv:5", "/items/4"}) {
		t.Fatal("unrelated declaration absorbed a warning")
	}

	diagnostic.Location = nil
	if diagnosticRefersTo(&diagnostic, []string{referenceArgvID}) {
		t.Fatal("unlocated record acquired an invented consumer")
	}
}
