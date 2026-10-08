// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

	output, err := goCommand(root, goListVerb, "-deps", "-f", format, "./cmd/pdfconcat").output(ctx)
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
	output, err := goCommand(root, goListVerb, allPackages).output(ctx)
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
// executable run as a subprocess by those tests, merges the two profiles, applies the registry and
// enforces its threshold.
func runCoverage(ctx context.Context, args []string) error {
	set := newFlags(coverageCommand)
	profileOut := set.String("profile", "", "also write the merged profile (and its per-function report, with .func.txt appended) here")

	err := set.Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	module, err := modulePath(root)
	if err != nil {
		return err
	}

	registry, err := repopolicy.LoadRegistry(filepath.Join(root, registryName))
	if err != nil {
		return fmt.Errorf(loadRegistryError, err)
	}

	runtimeList, err := runtimePackages(ctx, root, module)
	if err != nil {
		return err
	}

	scratch, err := os.MkdirTemp("", "qualitygate-coverage-")
	if err != nil {
		return fmt.Errorf("create scratch directory: %w", err)
	}

	defer removeAll(scratch)

	merged, err := collectCoverage(ctx, root, scratch, runtimeList)
	if err != nil {
		return err
	}

	if *profileOut != "" {
		err = writeProfileFiles(ctx, root, *profileOut, merged)
		if err != nil {
			return err
		}
	}

	result := repopolicy.EvaluateCoverage(repopolicy.CoverageInput{
		Profile: merged, Entries: registry.For(repopolicy.ToolCoverage), Module: module, GOOS: runtime.GOOS,
		Threshold: registry.CoverageThresholdPercent, Read: fileReader(root),
	})

	printCoverage(&result, registry.CoverageThresholdPercent)

	return report(coverageCommand, result.Problems, fmt.Sprintf("%.2f%% of reachable statements, threshold %.2f%%",
		result.Adjusted.Percent(), registry.CoverageThresholdPercent))
}

// collectCoverage runs the tests with instrumentation and returns the merged unit and subprocess profile.
func collectCoverage(ctx context.Context, root, scratch string, runtimeList []string) (*repopolicy.CoverageProfile, error) {
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

	err = (&command{
		dir: root, name: goTool, stdout: log.Writer(), stderr: log.Writer(),
		env: []string{envCoverDir + "=" + coverDir, envCoverPkg + "=" + coverPkg},
		args: append([]string{testVerb, countOnce, "-covermode=atomic", "-coverpkg=" + coverPkg, "-coverprofile=" + unitProfile},
			packages...),
	}).run(ctx)
	if err != nil {
		return nil, fmt.Errorf("unit tests failed: %w", err)
	}

	entries, err := os.ReadDir(coverDir)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("%w: the tests produced no executable coverage in %s: tests that run the executable must build it with "+
			"internal/exectest so that %s and %s take effect", errGate, coverDir, envCoverDir, envCoverPkg)
	}

	subprocessProfile := filepath.Join(scratch, "subprocess.out")

	err = goCommand(root, "tool", "covdata", "textfmt", "-i="+coverDir, "-o="+subprocessProfile).run(ctx)
	if err != nil {
		return nil, fmt.Errorf("convert executable coverage: %w", err)
	}

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

func printCoverage(result *repopolicy.CoverageResult, threshold float64) {
	for _, pkg := range result.Packages {
		log.Printf("coverage: %6.2f%%  %4d/%-4d  %s", pkg.Percent(), pkg.Covered, pkg.Total, pkg.Package)
	}

	log.Printf("coverage: raw      %6.2f%% (%d/%d statements)", result.Raw.Percent(), result.Raw.Covered, result.Raw.Total)
	log.Printf("coverage: excluded %d statements by registry entries", result.Excluded)
	log.Printf("coverage: reachable %5.2f%% (%d/%d statements), threshold %.2f%%",
		result.Adjusted.Percent(), result.Adjusted.Covered, result.Adjusted.Total, threshold)

	for _, note := range result.NotEvaluated {
		log.Printf("coverage: not evaluated on %s: %s", runtime.GOOS, note)
	}
}
