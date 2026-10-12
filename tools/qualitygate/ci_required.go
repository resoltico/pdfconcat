// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func runCIRequired(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("ci-required accepts no arguments")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	workflow, err := readInRoot(root, ".github/workflows/ci.yml")
	if err != nil {
		return err
	}
	if err := repopolicy.RequireCIOutcomes(workflow, []byte(os.Getenv("PDFCONCAT_CI_NEEDS"))); err != nil {
		return fmt.Errorf("%w: %w", errGate, err)
	}
	return nil
}
