// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"reflect"
	"strings"
	"testing"
)

func TestSummaryCutScopesNormalizeAndLeaveNoOmittedRecordLabels(t *testing.T) {
	t.Parallel()
	s := Summary{Diagnostics: make([]DiagnosticView, 2), TruncatedFields: []string{"diagnostics/0/path", "diagnostics/0/location/file", "diagnostics/1/cause", "diagnostics/10/path", "publication.output"}}
	s.markTruncated("diagnostics/0")
	s.markTruncated("diagnostics/0/location")
	s.markTruncated("diagnostics/1")
	s.omitDiagnostic()
	want := []string{"diagnostics/10/path", "publication.output", "diagnostics/0"}
	if !reflect.DeepEqual(s.TruncatedFields, want) || len(s.Diagnostics) != 1 || s.DiagnosticsOmitted != 1 {
		t.Fatalf("visible scope bookkeeping changed: %+v", s)
	}
}

func TestDiagnosticMessageTighteningStopsAfterFiniteWorkWithoutFit(t *testing.T) {
	t.Parallel()
	records := []DiagnosticView{{Message: strings.Repeat("m", 256)}}
	checks := 0
	shrinkDiagnosticMessages(records, func() bool { checks++; return false })
	if checks != 3 || records[0].Message != strings.Repeat("m", 32) || !records[0].MessageTruncated {
		t.Fatalf("unfitting preview did not finish three staged cuts: checks%d record%+v", checks, records[0])
	}
}
