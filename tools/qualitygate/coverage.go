// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	// scalePackageSuffix is the package of the scale acceptance test, which has its own job and is
	// not part of the default test, coverage or mutation runs.
	scalePackageSuffix = "/test/scale"

	// envCoverDir and envCoverPkg are the environment variables through which this gate tells test
	// code how to instrument the executable. internal/exectest reads them.
	envCoverDir = "PDFCONCAT_COVERDIR"
	envCoverPkg = "PDFCONCAT_COVERPKG"
)

// runtimePackages lists the module's packages linked into the executable on the host platform.
func runtimePackages(ctx context.Context, root, module string) ([]string, error) {
	format := `{{if and (not .Standard) .Module}}{{if eq .Module.Path "` + module + `"}}{{.ImportPath}}{{end}}{{end}}`

	output, err := baselineGoCommand(root, goListVerb, foreignDependencyFlag, "-f", format, productCommandPackage).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime packages: %w", err)
	}

	var packages []string

	for line := range strings.SplitSeq(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			packages = append(packages, trimmed)
		}
	}

	if len(packages) == 0 {
		return nil, fmt.Errorf("%w: no runtime packages found below ./cmd/pdfconcat", errGate)
	}

	return packages, nil
}

// testPackages lists every package of the module except the scale acceptance package.
func testPackages(ctx context.Context, root string) ([]string, error) {
	output, err := baselineGoCommand(root, goListVerb, allPackages).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}

	var packages []string

	for line := range strings.SplitSeq(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasSuffix(trimmed, scalePackageSuffix) {
			packages = append(packages, trimmed)
		}
	}

	return packages, nil
}

// runCoverage measures statement coverage of the runtime packages from the unit tests and from the
// executable run as a subprocess by those tests and reports their merged raw profile.
func runCoverage(ctx context.Context, args []string) (resultErr error) {
	set := newFlags(coverageCommand)
	evidenceDir := set.String("evidence", "", "new caller-owned directory for manifests, raw inputs and validated merged output")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}
	if *evidenceDir == "" {
		return fmt.Errorf("%w: coverage requires -evidence DIRECTORY", errGate)
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	directory := *evidenceDir
	if !filepath.IsAbs(directory) {
		if filepath.Separator == '\\' && (filepath.VolumeName(directory) != "" || strings.HasPrefix(directory, `\`) || strings.HasPrefix(directory, "/")) {
			return &os.PathError{Op: "validate coverage evidence", Path: directory, Err: os.ErrInvalid}
		}
		caller, err := os.Getwd()
		if err != nil {
			return err
		}
		directory = caller + string(filepath.Separator) + directory
	}
	directory, err = capture.NativeArtifactPath(directory)
	if err != nil {
		return err
	}
	attempt, err := startCoverageEvidence(ctx, root, directory)
	if err != nil {
		return err
	}
	defer func() { resultErr = finishCoverageEvidence(directory, attempt, resultErr) }()
	module, err := modulePath(root)
	if err != nil {
		return err
	}
	runtimeList, err := runtimePackages(ctx, root, module)
	if err != nil {
		return err
	}
	attempt.Stages["discovery"] = "complete"
	merged, err := collectCoverage(ctx, root, directory, runtimeList, attempt)
	if err != nil {
		return err
	}
	attempt.Stages["collection"] = "complete"
	if err := writeProfileFiles(ctx, root, filepath.Join(directory, "merged.out"), merged); err != nil {
		return err
	}
	attempt.Stages["merged_profile"] = "complete"
	result := repopolicy.EvaluateCoverage(repopolicy.CoverageInput{Profile: merged, Module: module})
	log.Printf("coverage: raw %.2f%% (%d/%d statements); no excluded statements or global percentage gate", result.Raw.Percent(), result.Raw.Covered, result.Raw.Total)
	return report(coverageCommand, result.Problems, "raw unit and executable coverage collected")
}

// collectBaselineCoverage measures CGO0 unit and executable coverage.
func collectBaselineCoverage(ctx context.Context, root, scratch string, runtimeList []string, attempt *coverageAttempt) (*repopolicy.CoverageProfile, error) {
	coverDir := filepath.Join(scratch, "covdata")

	err := os.MkdirAll(coverDir, dirMode)
	if err != nil {
		return nil, fmt.Errorf("create coverage data directory: %w", err)
	}

	packages, err := testPackages(ctx, root)
	if err != nil {
		return nil, err
	}

	coverPkg := strings.Join(runtimeList, ",")
	unitProfile := filepath.Join(scratch, "unit.out")

	evidence, err := openTestEvidence(filepath.Join(scratch, "unit.events.json"))
	if err != nil {
		return nil, err
	}
	run := &command{dir: root, name: goTool, env: []string{baselineCGOEnv, readonlyGoFlags, envCoverDir + "=" + coverDir, envCoverPkg + "=" + coverPkg}, args: append([]string{testVerb, jsonFlag, countOnce, atomicCoverageFlag, "-coverpkg=" + coverPkg, "-coverprofile=" + unitProfile}, packages...)}
	outcome, runErr := collectTestEvents(ctx, run, evidence)
	closeErr := evidence.closeFiles()
	withTests, discoveryErr := packagesWithTests(ctx, root, packages)
	if err := errors.Join(judgeTests(outcome, withTests, testOptions{}, runErr), closeErr, discoveryErr); err != nil {
		return nil, fmt.Errorf("unit collection failed: %w", err)
	}

	attempt.Stages["unit"] = "complete"
	inputs, err := executableCoverageInputs(coverDir)
	if err != nil {
		return nil, err
	}

	attempt.Stages["executable_data"] = "complete"
	subprocessProfile := filepath.Join(scratch, "subprocess.out")
	// Relative inputs preserve a caller root containing commas and keep hundreds of exclusive
	// child directories within native command-line limits. Their owned root was already inspected.
	for index, input := range inputs {
		inputs[index], err = filepath.Rel(coverDir, input)
		if err != nil {
			return nil, err
		}
	}

	conversion, conversionErr := goCommand(coverDir, goToolVerb, covdataVerb, "textfmt", "-i="+strings.Join(inputs, ","), "-o="+subprocessProfile).output(ctx)
	if conversionErr != nil {
		conversion += conversionErr.Error()
	}
	logErr := os.WriteFile(filepath.Join(scratch, "covdata-conversion.log"), []byte(conversion), fileMode)
	if err := errors.Join(conversionErr, logErr); err != nil {
		return nil, fmt.Errorf("convert executable coverage: %w", err)
	}

	attempt.Stages["conversion"] = "complete"
	unit, err := readProfile(unitProfile)
	if err != nil {
		return nil, err
	}

	subprocess, err := readProfile(subprocessProfile)
	if err != nil {
		return nil, err
	}

	log.Printf("coverage: unit profile %d blocks, executable profile %d blocks", len(unit.Blocks), len(subprocess.Blocks))

	merged, err := repopolicy.MergeProfiles(unit, subprocess)
	if err != nil {
		return nil, fmt.Errorf("merge profiles: %w", err)
	}

	attempt.Stages["baseline_merge"] = "complete"
	return merged, nil
}

func readProfile(file string) (*repopolicy.CoverageProfile, error) {
	content, err := readInRoot(filepath.Dir(file), filepath.Base(file))
	if err != nil {
		return nil, err
	}

	profile, err := repopolicy.ParseCoverageProfile(strings.NewReader(string(content)))
	if err != nil {
		return nil, fmt.Errorf(pathError, file, err)
	}

	return profile, nil
}

// writeProfileFiles saves the merged profile and the per-function report derived from it.
func writeProfileFiles(ctx context.Context, root, target string, merged *repopolicy.CoverageProfile) error {
	var text strings.Builder

	err := merged.Write(&text)
	if err != nil {
		return fmt.Errorf("render merged profile: %w", err)
	}

	err = os.WriteFile(target, []byte(text.String()), fileMode)
	if err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}

	functions, err := goCommand(root, "tool", "cover", "-func="+target).output(ctx)
	if err != nil {
		return fmt.Errorf("per-function report: %w", err)
	}

	err = os.WriteFile(target+".func.txt", []byte(functions), fileMode)
	if err != nil {
		return fmt.Errorf("write per-function report: %w", err)
	}

	log.Printf("coverage: merged profile %s, per-function report %s.func.txt", target, target)

	return nil
}
