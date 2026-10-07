// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type (
	// mutationEvidence binds discovery and outcomes produced with identical snapshot inputs.
	mutationEvidence struct {
		discovery *repopolicy.MutationReport
		campaign  *repopolicy.MutationReport
	}

	// scopeOfMutation is what gremlins mutates and what the gate judges.
	scopeOfMutation struct {
		hostFiles map[string]bool
		excludes  []string
		packages  int
	}
)

const (
	// defaultTimeoutCoefficient is generous because the tool derives each mutant's timeout from the
	// time of a plain coverage run, which does not include compiling the mutated package.
	defaultTimeoutCoefficient = 25

	// listFieldCount is the number of fields of a package listing line: import path, directory, files.
	listFieldCount = 3
)

// runMutation runs gremlins over the runtime packages in a clean snapshot of the working tree and
// judges the result against the registry: mutants must be killed or not viable; survivors and
// timeouts fail unless a precise registry entry accepts them. A final uncovered outcome is
// incomplete and always fails; a tool failure is a failure, never a pass.
func runMutation(ctx context.Context, args []string) error {
	started := time.Now()

	options, err := parseMutationFlags(args)
	if err != nil {
		return err
	}

	ctx, cancel := mutationContext(ctx, started, options.maxDuration)
	defer cancel()

	root, err := repoRoot()
	if err != nil {
		return err
	}

	registry, err := repopolicy.LoadRegistry(filepath.Join(root, registryName))
	if err != nil {
		return fmt.Errorf(loadRegistryError, err)
	}

	snapshot, err := snapshotTree(ctx, root)
	if err != nil {
		return err
	}

	options.root, options.snapshot = root, snapshot

	if options.keep {
		log.Printf("mutation: snapshot kept at %s", snapshot)
	} else {
		defer removeAll(snapshot)
	}

	result, err := mutateSnapshot(ctx, &options, registry)
	if err != nil {
		return err
	}

	for _, line := range result.SummaryLines() {
		log.Print("mutation: ", line)
	}

	return report(
		mutationCommand,
		result.Problems,
		fmt.Sprintf("%d mutants judged, none unkilled beyond %d registry-accepted", result.Evaluated, result.Accepted),
	)
}

// mutateSnapshot runs gremlins in the snapshot and judges its report.
func mutateSnapshot(ctx context.Context, options *mutationOptions, registry *repopolicy.Registry) (*repopolicy.MutationResult, error) {
	root, snapshot := options.root, options.snapshot

	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	binary, err := gremlinsBinary(ctx, root)
	if err != nil {
		return nil, err
	}

	scope, err := mutationScope(ctx, snapshot, module, splitList(options.only))
	if err != nil {
		return nil, err
	}

	reportFile := options.report
	if reportFile == "" {
		reportFile = filepath.Join(snapshot, "mutation-report.json")
	}

	log.Printf("mutation: gremlins in a snapshot of %d runtime packages (%d source files)", scope.packages, len(scope.hostFiles))

	evidence, err := executeMutation(ctx, binary, reportFile, options, scope.excludes)
	if err != nil {
		return nil, err
	}

	discoveryProblems := repopolicy.MutationDiscoveryIssues(evidence.discovery, evidence.campaign, scope.hostFiles)
	executionProblems := repopolicy.MutationExecutionEvidenceIssues(
		evidence.campaign, mutationCaptureReader(mutationExecutionDirectory(reportFile)),
	)

	mutableFiles := map[string]bool{}
	for _, mutant := range evidence.discovery.Mutants {
		mutableFiles[mutant.File] = true
	}

	result := repopolicy.EvaluateMutation(evidence.campaign, registry.For(repopolicy.ToolMutation), mutableFiles, fileReader(snapshot))

	result.Problems = append(result.Problems, discoveryProblems...)
	result.Problems = append(result.Problems, executionProblems...)

	for _, entry := range registry.For(repopolicy.ToolMutation) {
		if scope.hostFiles[entry.Path] && !mutableFiles[entry.Path] {
			result.Problems = append(result.Problems, entry.ID+": no mutant was discovered; delete the exception")
		}
	}

	return &result, nil
}

// gremlinsArguments builds the gremlins command line: every operator, the report path, the timeout
// coefficient, and one exclusion per package that is not linked into the executable.
func gremlinsArguments(reportFile string, options *mutationOptions, excludes []string) []string {
	args := append(
		[]string{
			"unleash", "--output", reportFile, "--timeout-coefficient", strconv.Itoa(options.coefficient),
			"--diff=", "--tags=", "--coverpkg=", "--dry-run=false", "--test-cpu=0",
			"--run-uncovered=true", "--execution-log-dir", mutationExecutionDirectory(reportFile),
			"--threshold-efficacy=0", "--threshold-mcover=0",
		},
		operatorFlags()...)
	if options.workers > 0 {
		args = append(args, "--workers", strconv.Itoa(options.toolWorkers()))
	}

	args = append(args, "--integration="+strconv.FormatBool(options.integration))

	for _, pattern := range excludes {
		args = append(args, "--exclude-files", pattern)
	}

	return args
}

// operatorFlags enables every mutation operator.
func operatorFlags() []string {
	flags := make([]string, 0, len(repopolicy.MutationOperators()))

	for _, operator := range repopolicy.MutationOperators() {
		flags = append(flags, "--"+strings.ToLower(strings.ReplaceAll(operator, "_", "-"))+"=true")
	}

	return flags
}

// mutationScope derives the runtime packages and their host-compiled source files in dir, and the
// exclusion patterns for every other package of the module.
func mutationScope(ctx context.Context, dir, module string, only []string) (scopeOfMutation, error) {
	// The go command reports directories with symbolic links resolved (macOS temporary directories
	// live behind one), so relative paths must be taken from the resolved directory.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return scopeOfMutation{}, fmt.Errorf("resolve %s: %w", dir, err)
	}

	runtimeList, err := runtimePackages(ctx, dir, module)
	if err != nil {
		return scopeOfMutation{}, err
	}

	runtimeSet := map[string]bool{}
	for _, pkg := range runtimeList {
		runtimeSet[pkg] = true
	}

	listing, err := goCommand(
		dir,
		append(
			[]string{goListVerb, "-f", `{{.ImportPath}}|{{.Dir}}|{{join .GoFiles ","}},{{join .CgoFiles ","}}`, allPackages},
			runtimeList...)...).output(ctx)
	if err != nil {
		return scopeOfMutation{}, fmt.Errorf("list packages: %w", err)
	}

	scope := scopeOfMutation{packages: len(runtimeList), hostFiles: map[string]bool{}, excludes: []string{`_test\.go$`, `(^|/)testdata/`}}

	err = scope.collectRuntimeListing(resolved, listing, runtimeSet, only)
	if err != nil {
		return scopeOfMutation{}, err
	}

	ignored, err := buildTagExclusions(ctx, dir, resolved, runtimeList)
	if err != nil {
		return scopeOfMutation{}, err
	}

	scope.excludes = append(scope.excludes, ignored...)

	if len(scope.hostFiles) == 0 {
		return scopeOfMutation{}, fmt.Errorf("%w: the runtime packages have no source files to mutate", errGate)
	}

	return scope, nil
}

// addOther excludes a package that is not linked into the executable.
func (s *scopeOfMutation) addOther(relative string) {
	s.excludes = append(s.excludes, "^"+regexp.QuoteMeta(relative)+"/")
}

// addRuntime records the host-compiled files of a runtime package.
func (s *scopeOfMutation) addRuntime(relative, files string) {
	for file := range strings.SplitSeq(files, ",") {
		if file != "" {
			s.hostFiles[relative+"/"+file] = true
		}
	}
}

// readMutationReport reads only a freshly produced tool report and restores package-relative paths.
func readMutationReport(file string) (*repopolicy.MutationReport, error) {
	data, err := readInRoot(filepath.Dir(file), filepath.Base(file))
	if err != nil {
		return nil, fmt.Errorf("mutation tool wrote no readable report: %w", err)
	}

	parsed, err := repopolicy.ParseMutationReport(data)
	if err != nil {
		return nil, fmt.Errorf("mutation report: %w", err)
	}

	return parsed, nil
}

// buildTagExclusions derives the exact files not built on this host from Go's package discovery.
func buildTagExclusions(ctx context.Context, dir, resolved string, runtimeList []string) ([]string, error) {
	listing, err := goCommand(
		dir,
		append([]string{goListVerb, "-f", `{{.Dir}}|{{join .IgnoredGoFiles ","}}`, allPackages}, runtimeList...)...).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("list build-tag exclusions: %w", err)
	}

	var excludes []string

	for line := range strings.SplitSeq(strings.TrimSpace(listing), "\n") {
		directory, files, found := strings.Cut(line, "|")
		if !found {
			return nil, fmt.Errorf("%w: invalid build-tag discovery line %q", errGate, line)
		}

		relative, relErr := filepath.Rel(resolved, directory)
		if relErr != nil {
			return nil, fmt.Errorf("build-tag directory: %w", relErr)
		}

		for file := range strings.SplitSeq(files, ",") {
			if file != "" {
				excludes = append(excludes, "^"+regexp.QuoteMeta(filepath.ToSlash(filepath.Join(relative, file)))+"$")
			}
		}
	}

	return excludes, nil
}

// executeMutation establishes complete discovery before running outcomes on the same snapshot.
func executeMutation(ctx context.Context, tool, reportFile string, options *mutationOptions, excludes []string) (mutationEvidence, error) {
	discoveryFile := reportFile + ".discovery.json"

	paths := []string{reportFile, discoveryFile, mutationExecutionDirectory(reportFile), mutationExecutionDirectory(discoveryFile)}
	for _, file := range paths {
		if _, statErr := os.Lstat(file); !errors.Is(statErr, os.ErrNotExist) {
			if statErr != nil {
				return mutationEvidence{}, fmt.Errorf("%w: inspect mutation evidence path %s: %w", errGate, file, statErr)
			}

			return mutationEvidence{}, fmt.Errorf("%w: refusing to reuse existing mutation evidence path %s", errGate, file)
		}
	}

	discoveryArgs := append(gremlinsArguments(discoveryFile, options, excludes), "--dry-run", ".")

	err := (&command{
		dir: options.snapshot, name: tool, args: discoveryArgs, stdout: log.Writer(), stderr: log.Writer(),
		env: []string{readonlyGoFlags},
	}).runMutation(ctx, options.maxDuration)
	if err != nil {
		return mutationEvidence{}, fmt.Errorf("mutation discovery failed: %w", err)
	}

	discovery, err := readMutationReport(discoveryFile)
	if err != nil {
		return mutationEvidence{}, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return mutationEvidence{}, fmt.Errorf("mutation discovery canceled: %w", ctxErr)
	}

	err = (&command{
		dir: options.snapshot, name: tool,
		args: append(gremlinsArguments(reportFile, options, excludes), "."), stdout: log.Writer(), stderr: log.Writer(),
		env: []string{readonlyGoFlags},
	}).runMutation(ctx, options.maxDuration)
	if err != nil {
		return mutationEvidence{}, fmt.Errorf("mutation tool failure (infrastructure, not a pass): %w", err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return mutationEvidence{}, fmt.Errorf("mutation campaign canceled: %w", ctxErr)
	}

	campaign, err := readMutationReport(reportFile)

	return mutationEvidence{discovery: discovery, campaign: campaign}, err
}

func (s *scopeOfMutation) collectRuntimeListing(resolved, listing string, runtimeSet map[string]bool, only []string) error {
	selected := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(listing), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) != listFieldCount {
			return fmt.Errorf("%w: invalid runtime discovery line %q", errGate, line)
		}

		relative, relErr := filepath.Rel(resolved, fields[1])
		if relErr != nil {
			return fmt.Errorf("package directory %s: %w", fields[1], relErr)
		}

		if runtimeSet[fields[0]] && (len(only) == 0 || slices.Contains(only, filepath.ToSlash(relative))) {
			s.addRuntime(filepath.ToSlash(relative), fields[2])
			selected[filepath.ToSlash(relative)] = true
		} else {
			s.addOther(filepath.ToSlash(relative))
		}
	}

	for _, requested := range only {
		if !selected[requested] {
			return fmt.Errorf("%w: requested mutation package %s is not a host runtime package", errGate, requested)
		}
	}

	s.packages = len(selected)

	return nil
}

// mutationExecutionDirectory retains baseline and per-mutant process evidence beside its report.
func mutationExecutionDirectory(reportFile string) string { return reportFile + ".executions" }
