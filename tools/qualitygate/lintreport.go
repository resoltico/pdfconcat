// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"fmt"
	"os"
)

// readLintRunReport preserves the child failure when it could not produce its required JSON report.
func readLintRunReport(root string, runErr error, output string) ([]byte, error) {
	data, readErr := readInRoot(root, lintReportFileName)
	if readErr != nil {
		return nil, fmt.Errorf("read lint report: %w\n%s", errors.Join(readErr, runErr), output)
	}

	return data, nil
}

// prepareLintRunReport removes only the previous generated report inside the owned scratch root.
func prepareLintRunReport(root string) error {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open lint report root: %w", err)
	}

	removeErr := tree.Remove(lintReportFileName)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}

	if finishErr := errors.Join(removeErr, tree.Close()); finishErr != nil {
		return fmt.Errorf("prepare fresh lint report: %w", finishErr)
	}

	return nil
}
