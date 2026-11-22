// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
)

func TestIODiagnosticSeparatesPublicTargetFromNativeStagingDetail(t *testing.T) {
	const public = "/chosen/report.json"
	const staged = "/chosen/.report.json.RANDOM-STAGING.tmp"
	for _, cause := range []error{
		&fs.PathError{Op: "open", Path: staged, Err: fs.ErrNotExist},
		&os.LinkError{Op: "rename", Old: staged, New: public, Err: fs.ErrPermission},
	} {
		d := ioDiagnostic(diag.ReportWriteFailed, cause, public)
		if d.File != public || !strings.Contains(d.Message, "Report writing or publication") || strings.Contains(d.Message, staged) || !strings.Contains(d.Detail, staged) {
			t.Fatalf("public context or native evidence lost: %+v", d)
		}
		if !strings.Contains(d.Message, "does not exist") && !strings.Contains(d.Message, "permission denied") {
			t.Fatalf("native cause hidden: %+v", d)
		}
	}
	if d := ioDiagnostic(diag.OutputPublishFailed, &fs.PathError{Op: "sync", Path: "/chosen", Err: fs.ErrPermission}, "/chosen/packet.pdf"); !strings.Contains(d.Message, "publication reported an error") {
		t.Fatal("publication error falsely described as absence", d)
	}
	if d := ioDiagnostic(diag.PlanUnreadable, errors.New("reader failed"), "<stdin>"); d.File != "" || d.Message != "Cannot read plan: reader failed." {
		t.Fatal(d)
	}
	if d := ioDiagnostic(diag.SourceUnreadable, context.Canceled, "/source.pdf"); d.Code != diag.Interrupted || d.File != "/source.pdf" {
		t.Fatal(d)
	}
}

func TestPublicationDiagnosesLateDestinationCollision(t *testing.T) {
	for _, tc := range []struct {
		name             string
		role             packet.DestinationRole
		overwrite, known bool
		code, failure    diag.Code
	}{
		{"output", packet.OutputDestination, false, true, diag.OutputExists, diag.OutputPublishFailed},
		{"report", packet.ReportDestination, false, true, diag.ReportExists, diag.ReportWriteFailed},
		{"report-unknown-inputs", packet.ReportDestination, true, false, diag.ReportExists, diag.ReportWriteFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "destination")
			facts := packet.DestinationFacts{Role: tc.role, Path: path, InputsKnown: tc.known, Overwrite: tc.overwrite}
			if ds := packet.CheckDestination(facts); len(ds) != 0 {
				t.Fatal(ds)
			}
			staged, err := files.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := staged.Abort(); err != nil {
					t.Error(err)
				}
			})
			if err := os.WriteFile(path, []byte("winner"), 0600); err != nil {
				t.Fatal(err)
			}
			published, err := staged.Commit(packet.MayReplace(tc.role, tc.overwrite, tc.known))
			if published || !errors.Is(err, files.ErrDestinationExists) {
				t.Fatalf("commit = %t, %v", published, err)
			}
			d := publicationDiagnostic(tc.role, path, tc.known, published, err)
			facts.Exists, facts.Regular = true, true
			preflight := packet.CheckDestination(facts)
			if d.Code != tc.code || diag.Outcome([]diag.Diagnostic{d}) != "invalid" || len(preflight) != 1 ||
				preflight[0].Code != d.Code || d.File != path || strings.Contains(d.Message, "--overwrite") {
				t.Fatal(d, preflight)
			}
			if tc.known {
				if !strings.Contains(d.Message, "Choose a new path") || !strings.Contains(preflight[0].Message, "--overwrite") {
					t.Fatal(d, preflight)
				}
			} else if d.Message != preflight[0].Message || !strings.Contains(d.Message, "plan inputs are unknown") {
				t.Fatal(d, preflight)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "winner" {
				t.Fatalf("destination = %q, %v", data, err)
			}
			for _, technical := range []struct {
				published bool
				err       error
			}{{false, fs.ErrPermission}, {false, fs.ErrExist}, {true, fs.ErrExist}} {
				if d := publicationDiagnostic(tc.role, path, tc.known, technical.published, technical.err); d.Code != tc.failure {
					t.Fatal(d)
				}
			}
		})
	}
}
