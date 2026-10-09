// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// equivalentNativeProfile rejects missing, extra or changed instrumentation blocks before merging
// altered-signal fixture counts into ordinary CGO0 production coverage.
func equivalentNativeProfile(baseline, native *repopolicy.CoverageProfile) error {
	if baseline.Mode != "atomic" || native.Mode != baseline.Mode {
		return fmt.Errorf("%w: native coverage mode differs from atomic baseline", errGate)
	}

	baselineShape, baselineErr := coverageShape(baseline)

	nativeShape, nativeErr := coverageShape(native)
	if baselineErr != nil || nativeErr != nil {
		return errors.Join(baselineErr, nativeErr)
	}

	if !maps.Equal(baselineShape, nativeShape) {
		return fmt.Errorf("%w: native coverage file/block/statement inventory differs", errGate)
	}

	return nil
}

func coverageShape(profile *repopolicy.CoverageProfile) (map[repopolicy.Block]bool, error) {
	shape := map[repopolicy.Block]bool{}

	for _, block := range profile.Blocks {
		if block.Count < 0 || block.Stmts < 0 {
			return nil, fmt.Errorf("%w: negative coverage count or statement count", errGate)
		}

		block.Count = 0
		if shape[block] {
			return nil, fmt.Errorf("%w: duplicate coverage block", errGate)
		}

		shape[block] = true
	}

	return shape, nil
}

func nativeBaselineProfile(ctx context.Context, root, directory string, packages []string) (*repopolicy.CoverageProfile, error) {
	path := filepath.Join(directory, "baseline-shape.out")

	run := baselineGoCommand(
		root,
		testVerb,
		countOnce,
		"-run=^$",
		atomicCoverageFlag,
		"-coverpkg="+strings.Join(packages, ","),
		"-coverprofile="+path,
		productCommandPackage,
	)
	if err := run.run(ctx); err != nil {
		return nil, fmt.Errorf("compile CGO0 coverage shape (no behavioral tests): %w", err)
	}

	return readProfile(path)
}

func supplementNativeProgress(
	ctx context.Context,
	root, scratch string,
	packages []string,
	baseline *repopolicy.CoverageProfile,
) (*repopolicy.CoverageProfile, error) {
	native, err := collectNativeProgress(ctx, root, filepath.Join(scratch, "native-progress"), packages, testOptions{timeout: "10m"})
	if err != nil {
		return nil, err
	}

	if shapeErr := equivalentNativeProfile(baseline, native); shapeErr != nil {
		return nil, shapeErr
	}

	return repopolicy.MergeProfiles(baseline, native)
}

// collectCoverage retains the standalone baseline and qualifies altered native fault counts only
// after source identity is checked across the entire baseline and supplement interval.
func collectCoverage(ctx context.Context, root, scratch string, packages []string) (*repopolicy.CoverageProfile, error) {
	if runtime.GOOS != nativeProgressOS {
		return collectBaselineCoverage(ctx, root, scratch, packages)
	}

	sources, err := nativeProductionSources(ctx, root)
	if err != nil {
		return nil, err
	}

	baseline, err := collectBaselineCoverage(ctx, root, scratch, packages)
	if err != nil {
		return nil, err
	}

	native, err := supplementNativeProgress(ctx, root, scratch, packages, baseline)
	if err != nil {
		return nil, err
	}

	if sourceErr := checkNativeSources(ctx, root, sources); sourceErr != nil {
		return nil, sourceErr
	}

	return native, nil
}
