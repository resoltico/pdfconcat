// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	nativeProgressOS      = "darwin"
	baselineCGOEnv        = "CGO_ENABLED=0"
	productCommandPackage = "./cmd/pdfconcat"
	atomicCoverageFlag    = "-covermode=atomic"
	goToolVerb            = "tool"
	covdataVerb           = "covdata"
	nativeProgressOwner   = "test-support-progress-native"
	nativeProgressTag     = "progress_native"
	nativeProgressFixture = "internal/progressnativefixture"
	nativeProgressPattern = "^TestProgressNative(WriteInterruption|TerminalPathPolicyFailure)$"
	nativeProgressSignal  = "TestProgressNativeWriteInterruption"
	nativeProgressSandbox = "TestProgressNativeTerminalPathPolicyFailure"
)

func runNativeProgress(ctx context.Context, root string, options testOptions) error {
	if runtime.GOOS != nativeProgressOS {
		return fmt.Errorf("%w: native progress requires a native Darwin compiler and host", errGate)
	}

	module, err := modulePath(root)
	if err != nil {
		return err
	}

	packages, err := runtimePackages(ctx, root, module)
	if err != nil {
		return err
	}

	directory, err := nativeProgressDirectory(options.events)
	if err != nil {
		return err
	}

	sources, err := nativeProductionSources(ctx, root)
	if err != nil {
		return err
	}

	baseline, err := nativeBaselineProfile(ctx, root, directory, packages)
	if err != nil {
		return err
	}

	profile, err := collectNativeProgress(ctx, root, directory, packages, options)
	if err != nil {
		return err
	}

	return errors.Join(equivalentNativeProfile(baseline, profile), checkNativeSources(ctx, root, sources))
}

func nativeProgressDirectory(events string) (string, error) {
	if events == "" {
		directory, err := os.MkdirTemp("", "qualitygate-native-progress-")
		if err != nil {
			return "", fmt.Errorf("create native progress evidence: %w", err)
		}

		return directory, nil
	}

	directory := events + ".native-progress"
	if err := os.Mkdir(directory, dirMode); err != nil {
		return "", fmt.Errorf("create native progress evidence: %w", err)
	}

	return directory, nil
}

func collectNativeProgress(
	ctx context.Context,
	root, directory string,
	packages []string,
	options testOptions,
) (*repopolicy.CoverageProfile, error) {
	if runtime.GOOS != nativeProgressOS {
		return nil, fmt.Errorf("%w: native progress requires Darwin", errGate)
	}

	if err := os.MkdirAll(directory, dirMode); err != nil {
		return nil, fmt.Errorf("create native evidence directory: %w", err)
	}

	if compilerErr := nativeCompilerReceipt(ctx, root, directory); compilerErr != nil {
		return nil, compilerErr
	}

	sources, err := nativeProductionSources(ctx, root)
	if err != nil {
		return nil, err
	}

	if options.events == "" {
		options.events = filepath.Join(directory, "events.jsonl")
	}

	profile, err := executeNativeProgress(ctx, root, directory, packages, options)
	if err != nil {
		return nil, err
	}

	after, err := nativeProductionSources(ctx, root)
	if err != nil {
		return nil, err
	}

	if !maps.Equal(sources, after) {
		return nil, fmt.Errorf("%w: production source changed during native progress verification", errGate)
	}

	content, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode native source receipt: %w", err)
	}

	if writeErr := os.WriteFile(filepath.Join(directory, "production-and-fixture-source-sha256.json"), content, fileMode); writeErr != nil {
		return nil, fmt.Errorf("write native source receipt: %w", writeErr)
	}

	log.Printf("native-progress: altered-signal and sandbox fixtures passed; production source identity retained at %s", directory)

	return profile, nil
}

func executeNativeProgress(
	ctx context.Context,
	root, directory string,
	packages []string,
	options testOptions,
) (*repopolicy.CoverageProfile, error) {
	evidence, err := openTestEvidence(options.events)
	if err != nil {
		return nil, err
	}

	coverDir := filepath.Join(directory, "children")
	if err = os.Mkdir(coverDir, dirMode); err != nil {
		return nil, errors.Join(err, evidence.closeFiles())
	}

	unitPath := filepath.Join(directory, "native-unit.out")
	run := &command{dir: root, name: goTool, env: append(nativeProgressEnv(), envCoverDir+"="+coverDir), args: []string{
		testVerb,
		jsonFlag,
		countOnce,
		"-race",
		atomicCoverageFlag,
		"-coverpkg=" + strings.Join(packages, ","),
		"-coverprofile=" + unitPath,
		"-timeout=" + options.timeout,
		"-run=" + nativeProgressPattern,
		productCommandPackage,
	}}
	outcome, runErr := collectTestEvents(ctx, run, evidence)
	runErr = errors.Join(runErr, evidence.closeFiles())
	module, moduleErr := modulePath(root)
	options.require = []string{nativeProgressSignal, nativeProgressSandbox}
	judged := errors.Join(
		moduleErr,
		judgeTests(outcome, []string{module + "/cmd/pdfconcat"}, options, runErr),
		nativeProgressCases(outcome, module),
	)
	evidence.finish(judged)

	if judged != nil {
		return nil, judged
	}

	unit, err := readProfile(unitPath)
	if err != nil {
		return nil, err
	}

	return nativeChildProfiles(ctx, root, coverDir, unit)
}

func nativeProgressCases(outcome *testOutcome, module string) error {
	for _, name := range []string{nativeProgressSignal + "/stimulus", nativeProgressSignal + "/retry"} {
		if outcome.completed[module+"/cmd/pdfconcat "+name] != actionPass {
			return fmt.Errorf("%w: mandatory native scenario did not pass: %s", errGate, name)
		}
	}

	if len(outcome.skipped) != 0 {
		return fmt.Errorf("%w: native progress fixture skipped", errGate)
	}

	return nil
}
