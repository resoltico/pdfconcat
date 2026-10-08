// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestUnresolvedRequestedReportPreservesInvocationAuthority(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, failedCheck)

	saved.Publication = report.Publication{
		ReportStatus: report.ReportFailed, ReportFrom: originalReportReference,
		ReportWrite: fixtureNotWritten, ReportTargetObservation: unknownMetadataValue,
	}
	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	encoded := encodeReport(t, saved)

	decoded := mustDecode(t, encoded)
	if decoded.Publication.ReportPath != "" || decoded.Publication.ReportFrom != originalReportReference ||
		decoded.Publication.ReportStatus != report.ReportFailed {
		t.Fatal("unresolved receipt invented a path or lost requested report")
	}

	schema := compileSchema(t, report.Schema(), schemaURL)
	if schemaVerdict(t, schema, []byte(encoded)) != schemaAccept {
		t.Fatal("schema rejected unresolved report authority")
	}
}

func TestUnresolvedReportReferenceRejectsMissingAndContradictoryFacts(t *testing.T) {
	t.Parallel()

	mutations := []struct {
		mutate func(*report.Publication)
		name   string
	}{
		{name: "missing authority", mutate: func(pub *report.Publication) { pub.ReportFrom = "" }},
		{name: "wrong authority", mutate: func(pub *report.Publication) { pub.ReportFrom = fixtureQueryReportReference }},
		{name: "resolved path plus reference", mutate: func(pub *report.Publication) { pub.ReportPath = fixtureReportPath }},
		{name: "written report", mutate: func(pub *report.Publication) { pub.ReportStatus = report.ReportWritten }},
		{name: "unrequested report", mutate: func(pub *report.Publication) { pub.ReportStatus = report.ReportNotRequested }},
		{name: "written current attempt", mutate: func(pub *report.Publication) { pub.ReportWrite = string(report.ReportWritten) }},
		{name: "missing current-attempt fact", mutate: func(pub *report.Publication) { pub.ReportWrite = "" }},
		{name: "observed absence", mutate: func(pub *report.Publication) { pub.ReportTargetObservation = "absent_when_observed" }},
		{name: "missing observation", mutate: func(pub *report.Publication) { pub.ReportTargetObservation = "" }},
	}

	schema := compileSchema(t, report.Schema(), schemaURL)
	for _, tc := range mutations {
		saved := mustDecode(t, failedCheck)
		saved.Publication = report.Publication{
			ReportStatus: report.ReportFailed, ReportFrom: originalReportReference,
			ReportWrite: fixtureNotWritten, ReportTargetObservation: unknownMetadataValue,
		}
		tc.mutate(&saved.Publication)

		if err := saved.Validate(); err == nil {
			t.Errorf("%s: invalid receipt accepted", tc.name)
		}

		document, err := report.Encode(saved)
		if err != nil {
			t.Fatal(err)
		}

		if schemaVerdict(t, schema, document) != schemaReject {
			t.Errorf("%s: schema accepted invalid receipt", tc.name)
		}

		if _, err = decodeText(t, string(document)); err == nil {
			t.Errorf("%s: decoder accepted invalid receipt", tc.name)
		}
	}
}
