// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
)

func TestLinuxSourceReadFailureIsFailed(t *testing.T) {
	// Reading the unmapped zero address through procfs produces genuine EIO after
	// regular-file acquisition, without a production fault-injection hook.
	const path = "/proc/self/mem"
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Skip("procfs unavailable")
	}
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("procfs prerequisite: %v", err)
	}
	r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.Items{core.SourceItem{Ref: "memory", Path: path}}, WorkingDir: filepath.Dir(path)})
	if r.Status != "failed" || core.ExitCode(r) != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code.String() != "source_unreadable" || r.Diagnostics[0].Detail == "" {
		t.Fatalf("unexpected regular-file read error classification: %+v", r)
	}
}
