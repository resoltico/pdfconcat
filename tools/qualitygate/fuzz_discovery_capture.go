// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

// boundedFuzzOutput bounds compiler/list discovery before parsing its metadata.
// The private limit parameter permits small real-process rejection controls;
// production callers always use the same 32 MiB combined fuzz evidence budget.
func (c *command) boundedFuzzOutput(ctx context.Context, limit int64) (string, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	budget := &evidenceBudget{remaining: limit, limit: limit, cancel: cancel}
	var stdout, stderr bytes.Buffer
	captured := *c
	captured.stdout = budgetedEvidenceWriter{budget: budget, target: &stdout}
	captured.stderr = budgetedEvidenceWriter{budget: budget, target: &stderr}
	runErr := captured.run(runContext)
	if limitErr := budget.err(); limitErr != nil {
		// Cancellation has settled the native process group before persisting
		// bounded prefixes outside the campaign's disposable private storage.
		return stdout.String(), errors.Join(runErr, limitErr, preserveFuzzDiscoveryOutput(stdout.Bytes(), stderr.Bytes()))
	}
	if runErr != nil {
		return stdout.String(), fmt.Errorf("%w\n%s", runErr, indent(stderr.String()))
	}
	return stdout.String(), nil
}

func preserveFuzzDiscoveryOutput(stdout, stderr []byte) error {
	evidence, err := openTestEvidence("")
	if err != nil {
		return fmt.Errorf("retain partial fuzz discovery output: %w", err)
	}
	_, stdoutErr := evidence.stdout.Write(stdout)
	_, stderrErr := evidence.stderr.Write(stderr)
	return fmt.Errorf("partial fuzz discovery stdout: %s; stderr: %s.stderr: %w", evidence.path, evidence.path,
		errors.Join(errGate, stdoutErr, stderrErr, evidence.closeFiles()))
}
