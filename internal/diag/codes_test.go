// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package diag_test

import (
	"github.com/resoltico/pdfconcat/internal/diag"
	"testing"
)

func TestOutcomePrecedence(t *testing.T) {
	for _, tc := range []struct {
		codes  []diag.Code
		status string
		exit   int
	}{
		{nil, "ok", 0}, {[]diag.Code{"source_xfa_removed"}, "ok", 0},
		{[]diag.Code{"plan_null"}, "invalid", 2}, {[]diag.Code{"plan_null", "source_unreadable"}, "failed", 1},
		{[]diag.Code{"source_unreadable", "plan_null"}, "failed", 1},
		{[]diag.Code{"source_unreadable", "interrupted", "plan_null"}, "interrupted", 130},
	} {
		var ds []diag.Diagnostic
		for _, c := range tc.codes {
			ds = append(ds, diag.New(c, "test"))
		}
		status, exit := diag.Outcome(ds)
		if status != tc.status || exit != tc.exit {
			t.Fatalf("%v: %s %d", tc.codes, status, exit)
		}
	}
}
func TestRegistryIsCompleteAndImmutable(t *testing.T) {
	registry := diag.Registry()
	for code, d := range registry {
		if d.Description == "" || d.Severity != "error" && d.Severity != "warning" || d.Severity == "warning" && d.Class != "" || d.Severity == "error" && d.Class == "" {
			t.Fatalf("%s: %+v", code, d)
		}
		if diag.New(code, "message").Severity != d.Severity {
			t.Fatal(code)
		}
	}
	delete(registry, "plan_null")
	if diag.New("plan_null", "test").Severity != "error" {
		t.Fatal("registry mutation reached definitions")
	}
}
