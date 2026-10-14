// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMutationFailedSDKRetainsSnapshotUnlessTerminalOwnershipSettled(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, report string
		retain       bool
	}{
		{"missing report", "", true},
		{"truncated report", "{", true},
		{"missing completion", `{}`, true},
		{"unknown termination", `{"complete":false,"termination_reason":"unknown"}`, true},
		{"unsettled native cleanup", `{"complete":false,"termination_reason":"process_cleanup_unsettled"}`, true},
		{"settled deadline", `{"complete":false,"termination_reason":"deadline_exceeded"}`, false},
		{"settled cancellation", `{"complete":false,"termination_reason":"canceled"}`, false},
		{"settled execution failure", `{"complete":false,"termination_reason":"execution_failure"}`, false},
		{"complete report then cleanup failure", `{"complete":true}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			snapshot := filepath.Join(t.TempDir(), "snapshot")
			if err := os.Mkdir(snapshot, dirMode); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(snapshot, "source.go")
			if err := os.WriteFile(source, []byte("owned source"), fileMode); err != nil {
				t.Fatal(err)
			}
			reportFile := filepath.Join(t.TempDir(), "mutation-report.json")
			if scenario.report != "" {
				if err := os.WriteFile(reportFile, []byte(scenario.report), fileMode); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("native SDK failed")
			result := mutationCommandFailure(reportFile, failure)
			if !errors.Is(result, failure) || errors.Is(result, errMutationCleanupUncertain) != scenario.retain {
				t.Fatalf("incorrect terminal classification: %v", result)
			}
			cleanupMutationSnapshot(&result, snapshot)
			contents, err := readInRoot(snapshot, "source.go")
			if scenario.retain {
				if err != nil || string(contents) != "owned source" {
					t.Fatalf("uncertain producer source removed: %q/%v", contents, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settled source cleanup failed: %v", err)
			}
			if !errors.Is(result, failure) {
				t.Fatalf("original failure hidden by cleanup: %v", result)
			}
		})
	}
}
