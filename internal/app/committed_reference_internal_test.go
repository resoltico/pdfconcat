// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestCommittedStateRetainsLosslessRecoveryReference(t *testing.T) {
	t.Parallel()

	for _, command := range []cli.Name{cli.NameBuild, cli.Name(strings.Repeat("hostile", 1000))} {
		t.Run(strconv.Itoa(len(command)), func(t *testing.T) { t.Parallel(); verifyCommittedReference(t, command) })
	}
}

func verifyCommittedReference(t *testing.T, command cli.Name) {
	t.Helper()

	recovery := filepath.Join(string(filepath.Separator)+strings.Repeat("ē", 80), ".pdfconcat-report-12345678")
	rep := report.NewBuilder("build").Build(report.StatusFailed)
	rep.Publication = report.Publication{Published: true, ReportStatus: report.ReportFailed, RecoveryReport: recovery}

	var stderr bytes.Buffer
	stateOnStderr(Env{Stderr: &stderr}, command, rep, errInjected)

	if stderr.Len() > report.SummaryBytes {
		t.Fatalf("committed state uses %d bytes", stderr.Len())
	}

	var state committedState
	if err := json.Unmarshal(stderr.Bytes(), &state); err != nil {
		t.Fatal(err)
	}

	if state.RecoveryReport == recovery {
		return
	}

	exactReference := state.RecoveryReport == "" && state.RecoveryBasename == filepath.Base(recovery) &&
		state.RecoveryDirectoryFrom == "original_report_argument"
	if !exactReference {
		t.Fatalf("lossless reference missing: %+v", state)
	}

	if !state.Truncated {
		t.Fatal("truncation not marked")
	}
}
