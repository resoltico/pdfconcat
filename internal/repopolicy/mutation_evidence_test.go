// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestMutationExecutionEvidenceBindsOutcomeAndCompleteRawFiles(t *testing.T) {
	t.Parallel()

	controls := []string{
		"valid",
		"wrong-reference",
		"missing-result",
		"wrong-status",
		"wrong-original",
		"not-started",
		"artifact-error",
		"truncated-output",
	}
	for _, control := range controls {
		t.Run(control, func(t *testing.T) {
			t.Parallel()

			mutant, record, stdout := mutationEvidenceFixture()

			switch control {
			case "missing-result":
				record = nil
			case "wrong-reference":
				mutant.ExecutionReference = hex.EncodeToString(make([]byte, sha256.Size))
			case "wrong-status":
				record["judgment_status"] = "LIVED"
			case "wrong-original":
				record["discovery_status"] = uncoveredStatus
			case "not-started":
				record["go_started"] = false
			case "artifact-error":
				record["artifact_error"] = "write failed"
			case "truncated-output":
				stdout = stdout[:len(stdout)-1]
			default:
			}

			directory := t.TempDir()
			writeMutationEvidence(t, directory, mutant.ExecutionReference, record, stdout)

			open := mutationEvidenceReader(t, directory)

			problems := repopolicy.MutationExecutionEvidenceIssues(&repopolicy.MutationReport{Mutants: []repopolicy.Mutant{mutant}}, open)
			if (len(problems) == 0) != (control == "valid") {
				t.Fatalf("execution binding %s: %v", control, problems)
			}
		})
	}
}

func mutationEvidenceFixture() (repopolicy.Mutant, map[string]any, []byte) {
	key := "pkg/total.go:4:7:CONDITIONALS_BOUNDARY"
	digest := sha256.Sum256([]byte(key))
	mutant := repopolicy.Mutant{
		File: mutationFile, Type: conditionalBoundaryOperator, Status: killedStatus, DiscoveryStatus: discoveredStatus,
		Line: 4, Column: 7, ExecutionReference: hex.EncodeToString(digest[:]),
	}
	stdout := []byte(`{"Action":"start","Package":"pkg"}` + "\n" + `{"Action":"fail","Package":"pkg"}` + "\n")
	stdoutHash := sha256.Sum256(stdout)
	stderrHash := sha256.Sum256(nil)
	started := time.Now()
	record := map[string]any{
		"mutation": key, "judgment_status": killedStatus, "discovery_status": discoveredStatus,
		"started": started, "finished": started.Add(time.Second), "go_started": true,
		"process_error": "exit status 1", "judgment_error": "", "artifact_error": "", "cache_policy": "cached replay qualified",
		"stdout_sha256": hex.EncodeToString(stdoutHash[:]), "stdout_bytes": len(stdout),
		"stderr_sha256": hex.EncodeToString(stderrHash[:]), "stderr_bytes": 0,
	}

	return mutant, record, stdout
}

func writeMutationEvidence(t *testing.T, directory, reference string, record map[string]any, stdout []byte) {
	t.Helper()

	files := map[string][]byte{reference + ".stdout.json": stdout, reference + ".stderr.txt": {}}

	if record != nil {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}

		files[reference+".result.json"] = data
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMutationExecutionRecordCannotCertifyTwoJudgments(t *testing.T) {
	t.Parallel()

	mutant, record, stdout := mutationEvidenceFixture()
	directory := t.TempDir()
	writeMutationEvidence(t, directory, mutant.ExecutionReference, record, stdout)

	open := mutationEvidenceReader(t, directory)

	duplicates := &repopolicy.MutationReport{Mutants: []repopolicy.Mutant{mutant, mutant}}

	problems := repopolicy.MutationExecutionEvidenceIssues(duplicates, open)
	if len(problems) == 0 {
		t.Fatal("one complete execution record certified two judgments")
	}
}

func mutationEvidenceReader(t *testing.T, directory string) func(string) (io.ReadCloser, error) {
	t.Helper()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return func(file string) (io.ReadCloser, error) { return root.Open(file) }
}
