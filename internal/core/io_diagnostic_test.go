// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
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
