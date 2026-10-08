// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"math"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestDiagnosticOrderingHandlesFullIntegerKeysAndRetainsArrival(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	for _, fault := range []struct {
		message string
		order   int
	}{
		{"maximum key", math.MaxInt},
		{"zero key earlier", 0},
		{"minimum key", math.MinInt},
		{"zero key later", 0},
	} {
		builder.AddDiagnostic(fault.order, report.Diagnostic{Stage: "inspect", Code: "fault", Message: fault.message})
	}

	got := builder.Build(report.StatusInvalid).Diagnostics
	want := []string{"minimum key", "zero key earlier", "zero key later", "maximum key"}

	if len(got) != len(want) {
		t.Fatalf("lost diagnostics: %d", len(got))
	}

	for index, message := range want {
		if got[index].Message != message {
			t.Fatalf("diagnostic %d: %q, want %q", index, got[index].Message, message)
		}
	}
}
