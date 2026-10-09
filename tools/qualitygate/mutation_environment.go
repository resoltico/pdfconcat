// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// mutationEnvironment exposes the verified checker to whole-suite tests without copying ignored tools into source snapshots.
func mutationEnvironment(ctx context.Context, options *mutationOptions, registry *repopolicy.Registry) ([]string, error) {
	binary, err := findTool(options.root, lintTool)
	if err != nil {
		return nil, err
	}

	problems, err := checkBinary(ctx, options.snapshot, binary, registry)
	if err != nil {
		return nil, err
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: mutation checker prerequisite: %v", errGate, problems)
	}

	return mutationCheckerEnvironment(binary)
}

func mutationCheckerEnvironment(binary string) ([]string, error) {
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return nil, fmt.Errorf("resolve mutation checker path: %w", err)
	}

	return []string{readonlyGoFlags, "PATH=" + filepath.Dir(absolute) + string(os.PathListSeparator) + os.Getenv("PATH")}, nil
}
