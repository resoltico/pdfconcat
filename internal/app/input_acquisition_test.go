// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestInitialSourceAcquisitionIsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "denied.pdf")
	if err := pdffixture.Plain("source").WriteFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0600); err != nil {
			t.Error(err)
		}
	})
	parent := filepath.Join(dir, "regular-parent")
	if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	cases := []string{filepath.Join(dir, "missing.pdf"), dir, filepath.Join(parent, "child.pdf")}
	f, err := os.Open(path)
	if err != nil && os.IsPermission(err) {
		cases = append(cases, path)
	} else {
		if err == nil {
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
		}
		t.Log("native permission denial not available; absent/nonregular paths still exercised")
	}
	deniedDir := filepath.Join(dir, "denied-directory")
	if err := os.Mkdir(deniedDir, 0700); err != nil {
		t.Fatal(err)
	}
	deniedChild := filepath.Join(deniedDir, "source.pdf")
	if err := pdffixture.Plain("inaccessible source").WriteFile(deniedChild); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(deniedDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(deniedDir, 0700); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(deniedChild); os.IsPermission(err) {
		cases = append(cases, deniedChild)
	} else {
		t.Log("directory search permission denial unavailable")
	}
	for _, path := range cases {
		r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.Items{app.SourceItem{Ref: "input", Path: path}}, WorkingDir: dir, Output: filepath.Join(dir, "prospective.pdf")})
		if r.Status != "invalid" || cli.ExitCode(r.Status) != 2 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code.String() != "source_input_invalid" || r.Diagnostics[0].File != path {
			t.Fatalf("initial acquisition: %+v", r)
		}
	}
}

func TestInvalidEffectiveTimestampReservesParsedInputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reserved.json")
	epoch := "invalid"
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, Input: app.PlanBytes{Data: []byte(`{"version":9,"items":[{"source":"reserved.json"}]}`)}, WorkingDir: dir, Report: path, Overwrite: true, SourceDateEpoch: &epoch})
	if r.Report == nil || r.Report.Written || r.Status != "invalid" {
		t.Fatalf("invalid timestamp bypassed report/input policy: %+v", r)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("report created an input path: %v", err)
	}
}
