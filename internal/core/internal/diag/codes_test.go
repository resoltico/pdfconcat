// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package diag_test

import (
	"os"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

func TestOutcomePrecedence(t *testing.T) {
	for _, tc := range []struct {
		codes  []diag.Code
		status string
		exit   int
	}{
		{nil, "ok", 0}, {[]diag.Code{diag.SourceXfaRemoved}, "ok", 0},
		{[]diag.Code{diag.PlanNull}, "invalid", 2}, {[]diag.Code{diag.PlanNull, diag.SourceUnreadable}, "failed", 1},
		{[]diag.Code{diag.SourceUnreadable, diag.PlanNull}, "failed", 1},
		{[]diag.Code{diag.SourceUnreadable, diag.Interrupted, diag.PlanNull}, "interrupted", 130},
	} {
		var ds []diag.Diagnostic
		for _, c := range tc.codes {
			ds = append(ds, diag.New(c, "test"))
		}
		status, exit := diag.Outcome(ds)
		if string(status) != tc.status || exit != tc.exit {
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
	delete(registry, diag.PlanNull)
	if diag.New(diag.PlanNull, "test").Severity != "error" {
		t.Fatal("registry mutation reached definitions")
	}
}

// TestCodeTextContract verifies every registered code and rejects arbitrary names.
func TestCodeTextContract(t *testing.T) {
	seen := map[diag.Code]bool{}
	for _, entry := range diag.Entries() {
		if seen[entry.Code] {
			t.Fatalf("duplicate code %s", entry.Code)
		}
		seen[entry.Code] = true
		wire, err := entry.Code.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var decoded diag.Code
		if err = decoded.UnmarshalText(wire); err != nil || decoded != entry.Code {
			t.Fatalf("round trip %s: %v", entry.Code, err)
		}
	}
	var code diag.Code
	for _, name := range []string{"", "source_encryptedd"} {
		if err := code.UnmarshalText([]byte(name)); err == nil {
			t.Fatalf("accepted unknown code %q", name)
		}
	}
}

func TestDocumentedCodesEqualRegistry(t *testing.T) {
	data, err := os.ReadFile("../../../../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	found := map[diag.Code]diag.Definition{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) != 6 {
			continue
		}
		severity := strings.TrimSpace(columns[2])
		if severity != "error" && severity != "warning" {
			continue
		}
		var code diag.Code
		if err := code.UnmarshalText([]byte(strings.Trim(strings.TrimSpace(columns[1]), "`"))); err != nil {
			t.Fatal(err)
		}
		class := strings.TrimSpace(columns[3])
		if class == "—" {
			class = ""
		}
		if _, duplicate := found[code]; duplicate {
			t.Fatalf("duplicate code row %s", code)
		}
		found[code] = diag.Definition{Severity: diag.Severity(severity), Class: diag.Class(class), Description: strings.TrimSpace(columns[4])}
	}
	registry := diag.Registry()
	if len(found) != len(registry) {
		t.Fatalf("documented codes=%d registry=%d", len(found), len(registry))
	}
	for code, definition := range registry {
		documented, ok := found[code]
		if !ok || documented.Severity != definition.Severity || documented.Class != definition.Class || documented.Description != strings.ReplaceAll(definition.Description, "|", " / ") {
			t.Fatalf("registry/table mismatch %s: %+v / %+v", code, definition, documented)
		}
	}
}
