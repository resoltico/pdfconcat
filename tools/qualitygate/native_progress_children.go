// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func nativeChildProfiles(
	ctx context.Context,
	root, directory string,
	unit *repopolicy.CoverageProfile,
) (*repopolicy.CoverageProfile, error) {
	inputs, err := nativeCoverageDirectories(directory)
	if err != nil {
		return nil, err
	}

	if layoutErr := nativeCoverageLayout(directory, inputs); layoutErr != nil {
		return nil, layoutErr
	}

	merged := unit

	for input := range inputs {
		child, childErr := readNativeChildProfile(ctx, root, input, unit)
		if childErr != nil {
			return nil, childErr
		}

		merged, err = repopolicy.MergeProfiles(merged, child)
		if err != nil {
			return nil, err
		}
	}

	return merged, nil
}

func nativeCoverageDirectories(directory string) (map[string]bool, error) {
	inputs := map[string]bool{}

	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "covmeta.") {
			inputs[filepath.Dir(path)] = true
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover native child coverage: %w", err)
	}

	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w: native progress produced no child coverage", errGate)
	}

	return inputs, nil
}

func nativeCoverageLayout(directory string, inputs map[string]bool) error {
	counts := map[string]int{}

	for input := range inputs {
		if input == directory {
			counts["sandbox"]++
			continue
		}

		if filepath.Dir(input) != directory {
			return fmt.Errorf("%w: nested native child coverage has no scenario owner", errGate)
		}

		name := filepath.Base(input)
		switch {
		case strings.HasPrefix(name, "native-signal-stimulus-"):
			counts["stimulus"]++
		case strings.HasPrefix(name, "native-signal-retry-"):
			counts["retry"]++
		default:
			return fmt.Errorf("%w: unexpected native child coverage", errGate)
		}
	}

	if counts["sandbox"] != 1 || counts["stimulus"] != 1 || counts["retry"] != 1 {
		return fmt.Errorf("%w: native child scenario coverage is missing or duplicated", errGate)
	}

	return nil
}

func readNativeChildProfile(
	ctx context.Context,
	root, input string,
	unit *repopolicy.CoverageProfile,
) (*repopolicy.CoverageProfile, error) {
	output := filepath.Join(input, "gate-child.out")
	if err := goCommand(root, goToolVerb, covdataVerb, "textfmt", "-i="+input, "-o="+output).run(ctx); err != nil {
		return nil, err
	}

	child, err := readProfile(output)
	if err != nil {
		return nil, err
	}

	if shapeErr := equivalentNativeProfile(unit, child); shapeErr != nil {
		return nil, shapeErr
	}

	if strings.HasPrefix(filepath.Base(input), "native-signal-retry-") {
		if counterErr := nativeRetryCounter(root, child); counterErr != nil {
			return nil, counterErr
		}
	}

	return child, nil
}
