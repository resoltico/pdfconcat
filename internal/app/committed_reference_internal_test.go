// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	stateOnStderr(t.Context(), Env{Stderr: &stderr}, command, cli.ProgressAuto, rep, errInjected)

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

func TestJSONCommittedStateRetainsExactMachineFactsAfterStdoutFailure(t *testing.T) {
	t.Parallel()

	rep := report.NewBuilder(string(cli.NameBuild)).Build(report.StatusOK)
	rep.Publication = report.Publication{
		Published:    true,
		Output:       "/" + strings.Repeat("ē", 1500) + "/out.pdf",
		ReportStatus: report.ReportWritten,
		ReportPath:   "/" + strings.Repeat("🙂", 1500) + "/saved.json",
	}
	sink := &progressCapture{}

	var stderr bytes.Buffer

	env := Env{Stdout: &failingWriter{}, Stderr: &stderr, ProgressRecord: sink, ProgressSequence: func() uint64 { return 701 }}

	code := finishCommand(t.Context(), env, &cli.Command{Name: cli.NameBuild, Progress: cli.ProgressJSON}, rep)
	if code != 1 || stderr.Len() != 0 {
		t.Fatalf("stdout failure outcome=%d humanstderr=%q", code, stderr.String())
	}

	records := sink.recordsCopy()
	if len(records) != 1 {
		t.Fatalf("committed records=%d", len(records))
	}

	var state committedState
	if err := json.Unmarshal(records[0], &state); err != nil {
		t.Fatal(err)
	}

	assertJSONCommittedFacts(t, &state, rep)

	cause := fmt.Errorf("%s: %w", strings.Repeat("ē", 2000), errInjected)
	stateOnStderr(t.Context(), env, cli.NameBuild, cli.ProgressJSON, rep, cause)

	records = sink.recordsCopy()
	if err := json.Unmarshal(records[1], &state); err != nil {
		t.Fatal(err)
	}

	if state.Diagnostics[0].Cause != cause.Error() || state.Truncated {
		t.Fatal("machine exception cause was trimmed")
	}
}

func assertJSONCommittedFacts(t *testing.T, state *committedState, rep *report.Report) {
	t.Helper()

	if state.Sequence == nil || *state.Sequence != 701 || state.Truncated || !state.Published || state.Output != rep.Publication.Output ||
		state.ReportPath != rep.Publication.ReportPath ||
		state.SavedRun.Status != report.StatusOK {
		t.Fatalf("machine committed facts lost: %+v", state)
	}
}
