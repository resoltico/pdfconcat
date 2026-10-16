// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publication_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/publication"
)

func TestKnownMissingInputsRequireEstablishedSeparation(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	input := filepath.Join(dir, "missing.pdf")
	cases := []struct {
		name, report string
		want         string
	}{
		{"exact", input, "report_is_input"},
		{"sibling", filepath.Join(dir, "report.json"), "report_identity_unverified"},
		{"separate existing directory", filepath.Join(other, "report.json"), ""},
		{"missing report parent", filepath.Join(dir, "missing-dir", "report.json"), "report_write_failed"},
	}
	if err := os.Symlink(dir, filepath.Join(other, "alias")); err == nil {
		cases = append(cases, struct {
			name, report string
			want         string
		}{"directory alias exact", filepath.Join(other, "alias", "missing.pdf"), "report_is_input"}, struct {
			name, report string
			want         string
		}{"directory alias sibling", filepath.Join(other, "alias", "report.json"), "report_identity_unverified"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roles := publication.Files{Report: tc.report, Inputs: []string{input}, InputsKnown: true, Overwrite: true}
			ds := roles.CheckReport()
			if tc.want == "" {
				if len(ds) > 0 {
					t.Fatal(ds)
				}
			} else if !hasCode(ds, diag.Code(tc.want)) {
				t.Fatalf("%v want %s", ds, tc.want)
			}
		})
	}
	existing := filepath.Join(dir, "existing.json")
	if err := os.WriteFile(existing, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	ds := (publication.Files{Report: existing, Inputs: []string{input}, InputsKnown: true, Overwrite: true}).CheckReport()
	if !hasCode(ds, "report_identity_unverified") {
		t.Fatal(ds)
	}
}
func TestBrokenInputIdentityNeverAuthorizesReport(t *testing.T) {
	for _, kind := range []string{"dangling", "loop", "non-directory prefix"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input")
			switch kind {
			case "dangling":
				if err := os.Symlink(filepath.Join(dir, "absent"), input); err != nil {
					t.Skip(err)
				}
			case "loop":
				if err := os.Symlink(input, input); err != nil {
					t.Skip(err)
				}
			case "non-directory prefix":
				if err := os.WriteFile(input, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
				input = filepath.Join(input, "child")
			}
			roles := publication.Files{Report: filepath.Join(t.TempDir(), "report.json"), Inputs: []string{input}, InputsKnown: true}
			if ds := roles.CheckReport(); !hasCode(ds, "report_write_failed") {
				t.Fatal(ds)
			}
		})
	}
	dir := t.TempDir()
	report := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "absent"), report); err != nil {
		t.Skip(err)
	}
	ds := (publication.Files{Report: report, Inputs: []string{filepath.Join(dir, "missing")}, InputsKnown: true, Overwrite: true}).CheckReport()
	if !hasCode(ds, "report_not_regular") || hasCode(ds, "report_write_failed") {
		t.Fatal(ds)
	}
}

func TestInaccessibleInputPrefixDoesNotAuthorizeReport(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "private")
	if err := os.Mkdir(prefix, 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(prefix, "missing.pdf")
	if err := os.Chmod(prefix, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(prefix, 0700); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(input); !errors.Is(err, os.ErrPermission) {
		t.Skipf("host does not expose an inaccessible-prefix permission error: %v", err)
	}
	ds := (publication.Files{Report: filepath.Join(t.TempDir(), "report.json"), Inputs: []string{input}, InputsKnown: true}).CheckReport()
	if !hasCode(ds, "report_write_failed") {
		t.Fatal(ds)
	}
}
