// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main_test

import (
	"encoding/json"
	"strings"
	"testing"
)

const fixtureProgressNone = "none"

func TestRequestedProgressIsSchemaValidNDJSONSeparateFromFinalResult(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")
	schema := schemaFromExecutable(t, dir, "response")
	observed := run(t, dir, "", commandCheck, "--progress=json", fileA, flagBlank)
	requireExit(t, observed, 0)
	validateContract(t, schema, observed.stdout)
	lines := strings.Split(strings.TrimSpace(observed.stderr), "\n")

	var (
		attempt  string
		sequence uint64
	)

	for _, line := range lines {
		validateContract(t, schema, line)

		var record struct {
			Kind     string `json:"kind"`
			Attempt  string `json:"attempt_id"`
			Sequence uint64 `json:"sequence"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}

		if record.Kind != "progress" || record.Sequence <= sequence || record.Attempt == "" {
			t.Fatalf("invalid live record: %s", line)
		}

		if attempt != "" && attempt != record.Attempt {
			t.Fatal("progress attempt changed")
		}

		attempt, sequence = record.Attempt, record.Sequence
	}

	final := generic(t, observed.stdout)
	if final["attempt_id"] != attempt {
		t.Fatal("progress and final result have different attempts")
	}

	for _, mode := range []string{"auto", fixtureProgressNone} {
		quiet := run(t, dir, "", commandCheck, "--progress="+mode, fileA)
		requireExit(t, quiet, 0)

		if quiet.stderr != "" {
			t.Fatalf("%s redirected stderr was noisy: %q", mode, quiet.stderr)
		}
	}
}
