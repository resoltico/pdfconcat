// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestDiagnosticsOfOneInputKeepTheOrderTheyArrivedIn(t *testing.T) {
	t.Parallel()

	const (
		count  = 3000
		inputs = 10
		stride = 7919 // coprime to inputs, so the inputs are interleaved evenly
	)

	builder := report.NewBuilder(testCommandCheck)

	for arrival := range count {
		builder.AddDiagnostic(arrival*stride%inputs, report.Diagnostic{Stage: "s", Code: "c", Message: strconv.Itoa(arrival)})
	}

	got := builder.Build(report.StatusInvalid).Diagnostics

	var want []string

	for input := range inputs {
		for arrival := range count {
			if arrival*stride%inputs == input {
				want = append(want, strconv.Itoa(arrival))
			}
		}
	}

	if len(got) != len(want) {
		t.Fatalf("%d diagnostics, want %d", len(got), len(want))
	}

	for index, diagnostic := range got {
		if diagnostic.Message != want[index] {
			t.Fatalf("position %d holds diagnostic %s, want %s", index, diagnostic.Message, want[index])
		}
	}
}
