// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/capture"
)

// mutationOptions are the flags of the mutation command.
type mutationOptions struct {
	storage                      *mutationStorageWatch
	root, snapshot, report, only string
	partition                    mutationPartition
	maxDuration                  time.Duration
	workers, coefficient         int
	integration, keep            bool
}

const maxMutationWorkers = 4

func parseMutationFlags(args []string) (mutationOptions, error) {
	set := newFlags(mutationCommand)
	workers := set.Int("workers", 2, "actual mutation execution workers (1 through 4)")
	duration := set.Duration("max-duration", 0, "total command budget, including setup and 60s cleanup reserve; 0 disables it")
	coefficient := set.Int("timeout-coefficient", defaultTimeoutCoefficient, "gremlins timeout coefficient; timeouts fail the gate")
	integration := set.Bool("integration", true, "run the whole test suite for every mutant instead of the mutated package's tests")
	reportOut := set.String("report", "", "keep the gremlins JSON report at this path")
	keep := set.Bool("keep", false, "keep the snapshot directory")
	only := set.String("only", "", "comma-separated package directories (such as internal/plan) to mutate; default: every runtime package")
	partition := set.String("partition", "0/1", "zero-based INDEX/COUNT partition of sorted host runtime files; every partition is required for a complete campaign")

	if parseErr := set.Parse(args); parseErr != nil {
		return mutationOptions{}, fmt.Errorf(parseFlagsError, parseErr)
	}

	if *workers < 1 || *workers > maxMutationWorkers {
		return mutationOptions{}, fmt.Errorf("%w: workers must be between 1 and %d", errGate, maxMutationWorkers)
	}

	if *coefficient < 1 {
		return mutationOptions{}, fmt.Errorf("%w: timeout-coefficient must be positive", errGate)
	}

	if *duration < 0 || (*duration > 0 && *duration <= mutationCleanupReserve) {
		return mutationOptions{}, fmt.Errorf("%w: max-duration must be zero or greater than %s", errGate, mutationCleanupReserve)
	}
	selectedPartition, err := parseMutationPartition(*partition)
	if err != nil {
		return mutationOptions{}, err
	}

	// The executor changes directory to the source snapshot; retained evidence belongs to the caller.
	if *reportOut != "" {
		bound := *reportOut
		var err error
		if !filepath.IsAbs(bound) {
			if filepath.Separator == '\\' && (filepath.VolumeName(bound) != "" || strings.HasPrefix(bound, `\`) || strings.HasPrefix(bound, "/")) {
				return mutationOptions{}, &os.PathError{Op: "validate mutation report", Path: bound, Err: os.ErrInvalid}
			}
			caller, lookupErr := os.Getwd()
			if lookupErr != nil {
				return mutationOptions{}, fmt.Errorf("resolve mutation report path: %w", lookupErr)
			}
			bound = caller + string(filepath.Separator) + bound
		}
		*reportOut, err = capture.NativeArtifactPath(bound)
		if err != nil {
			return mutationOptions{}, err
		}
	}

	return mutationOptions{
		partition: selectedPartition,
		report:    *reportOut, only: *only, workers: *workers, coefficient: *coefficient,
		integration: *integration, keep: *keep, maxDuration: *duration,
	}, nil
}

// The pinned upstream pool halves integration values above one.
func (options mutationOptions) toolWorkers() int {
	if options.integration && options.workers > 1 {
		return options.workers * 2
	}

	return options.workers
}

// baselinePackage selects a debugging baseline after mutationScope validates the requested host package.
func (options mutationOptions) baselinePackage() string {
	selected := splitList(options.only)
	if !options.integration && len(selected) == 1 {
		return "./" + selected[0]
	}

	return ""
}
