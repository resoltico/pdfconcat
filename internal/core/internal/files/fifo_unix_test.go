// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package files_test

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"

	"github.com/resoltico/pdfconcat/internal/core/internal/files"
)

func TestFileRoleFIFORefusals(t *testing.T) {
	for _, role := range []string{"output", "report"} {
		t.Run(role, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pipe")
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Skipf("native FIFO unavailable: %v", err)
			}
			roles := files.Files{InputsKnown: true, Overwrite: true}
			if role == "output" {
				roles.Output = path
			} else {
				roles.Report = path
			}
			ds := roles.Check()
			if role == "output" && !hasCode(ds, diag.OutputNotRegular) || role == "report" && !hasCode(ds, diag.ReportNotRegular) {
				t.Fatal(ds)
			}
		})
	}
}
