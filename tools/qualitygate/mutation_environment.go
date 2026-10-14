// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

	paths := []string{filepath.Dir(absolute)}
	if runtime.GOOS == windowsOS {
		qpdf, poppler := os.Getenv("PDFCONCAT_QPDF_BIN"), os.Getenv("PDFCONCAT_POPPLER_BIN")
		if (qpdf == "") != (poppler == "") {
			return nil, fmt.Errorf("%w: Windows oracle directories must be provided together", errGate)
		}
		if qpdf != "" {
			for _, directory := range []string{qpdf, poppler} {
				if !filepath.IsAbs(directory) {
					return nil, fmt.Errorf("%w: Windows oracle directory must be absolute: %q", errGate, directory)
				}
				root, openErr := os.OpenRoot(directory)
				if openErr != nil {
					return nil, fmt.Errorf("%w: open Windows oracle directory: %w", errGate, openErr)
				}
				if closeErr := root.Close(); closeErr != nil {
					return nil, fmt.Errorf("%w: close Windows oracle directory: %w", errGate, closeErr)
				}
			}
			paths = append(paths, qpdf, poppler)
		}
	}
	paths = append(paths, os.Getenv("PATH"))

	return []string{readonlyGoFlags, "PATH=" + strings.Join(paths, string(os.PathListSeparator))}, nil
}
