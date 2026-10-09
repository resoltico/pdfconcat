// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type (
	// testEvent is one line of `go test -json` output; the tool writes capitalized names, which
	// encoding/json matches case-insensitively.
	testEvent struct {
		Action  string  `json:"action"`
		Package string  `json:"package"`
		Test    string  `json:"test"`
		Output  string  `json:"output"`
		Elapsed float64 `json:"elapsed"`
	}

	// testOutcome accumulates what a `go test -json` run did.
	testOutcome struct {
		passed       map[string]map[string]bool // Package to the top-level tests that passed.
		packages     map[string]string          // Package to its final action.
		reasons      map[string]string          // Package and test to the first skip output line.
		running      map[string]bool
		completed    map[string]string
		started      map[string]bool
		seen         map[string]bool
		failed       []string
		skipped      []string
		streamErrors []string

		subtests int
		mutex    sync.Mutex
	}

	// testOptions are the flags of the test command.
	testOptions struct {
		stage   *foreignTestStage
		run     string
		events  string
		timeout string
		require []string
		race    bool
	}

	// fuzzTarget is one discovered fuzz function.
	fuzzTarget struct {
		pkg  string
		name string
	}
)

const (
	// bufferInitial and bufferMax size the scanner for long JSON lines.
	bufferInitial = 1 << 16
	bufferMax     = 1 << 24

	// listFields is the number of fields in a package listing line.
	listFields = 3
	// minOKFields is the fewest fields of a `go test` package result line: "ok" and the package.
	minOKFields = 2

	actionStart = "start"
	actionRun   = "run"
	actionPass  = "pass"
	actionFail  = "fail"
	actionSkip  = "skip"
)

func newTestOutcome() *testOutcome {
	return &testOutcome{
		passed:    map[string]map[string]bool{},
		packages:  map[string]string{},
		reasons:   map[string]string{},
		running:   map[string]bool{},
		completed: map[string]string{},
		started:   map[string]bool{},
		seen:      map[string]bool{},
	}
}

// consume reads `go test -json` events until the stream ends.
func (o *testOutcome) consume(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, bufferInitial), bufferMax)

	for scanner.Scan() {
		var event testEvent

		err := json.Unmarshal(scanner.Bytes(), &event)
		if err != nil {
			o.mutex.Lock()
			o.streamErrors = append(o.streamErrors, "invalid test event: "+err.Error())
			o.mutex.Unlock()

			continue
		}

		o.record(&event)
	}

	if err := scanner.Err(); err != nil {
		o.streamErrors = append(o.streamErrors, "truncated test event stream: "+err.Error())
		if _, drainErr := io.Copy(io.Discard, reader); drainErr != nil {
			o.streamErrors = append(o.streamErrors, "drain failed event stream: "+drainErr.Error())
		}
	}
}

func (o *testOutcome) record(event *testEvent) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	key := event.Package + " " + event.Test
	o.recordLifecycle(event, key)

	switch {
	case event.Action == "output":
		o.recordOutput(event, key)
	case event.Test == "":
		if slices.Contains([]string{actionPass, actionFail, actionSkip}, event.Action) {
			o.packages[event.Package] = event.Action
		}
	case event.Action == actionPass:
		o.recordPass(event)
	case event.Action == actionFail:
		o.failed = append(o.failed, key)
	case event.Action == actionSkip:
		o.skipped = append(o.skipped, key)
	default:
	}
}

func (o *testOutcome) recordOutput(event *testEvent, key string) {
	switch {
	case event.Test != "" && strings.Contains(event.Output, "SKIP"):
		o.reasons[key] = strings.TrimSpace(event.Output)
	case event.Test == "" && (strings.HasPrefix(event.Output, "ok ") || strings.HasPrefix(event.Output, "FAIL")):
		log.Print(strings.TrimRight(event.Output, "\n"))
	default:
	}
}

func (o *testOutcome) recordPass(event *testEvent) {
	if strings.Contains(event.Test, "/") {
		o.subtests++

		return
	}

	if o.passed[event.Package] == nil {
		o.passed[event.Package] = map[string]bool{}
	}

	o.passed[event.Package][event.Test] = true
}

// packagesWithTests lists the packages among patterns that contain test files.
func packagesWithTests(ctx context.Context, root string, patterns []string) ([]string, error) {
	args := append([]string{goListVerb, "-f", "{{.ImportPath}} {{len .TestGoFiles}} {{len .XTestGoFiles}}"}, patterns...)

	output, err := goCommand(root, args...).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}

	var packages []string

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == listFields && (fields[1] != "0" || fields[2] != "0") {
			packages = append(packages, fields[0])
		}
	}

	return packages, nil
}

// runTests runs go test with discovery checks: every package that has test files must run and pass at
// least one test, and every test named in -require must have passed (a skip does not count).
func runTests(ctx context.Context, args []string) (result error) {
	set := newFlags(testVerb)
	race := set.Bool("race", false, "enable the race detector")
	native := set.Bool("native-progress", false, "require Darwin native progress fault fixtures with atomic coverage")
	run := set.String(runVerb, "", "go test -run pattern")
	timeout := set.String("timeout", "10m", "go test -timeout")
	events := set.String("events", "", "retain complete JSON events here and process stderr at FILE.stderr; paths must be new")
	require := set.String("require", "", "comma-separated test names that must pass; implies they are discovered")

	err := set.Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	options := testOptions{race: *race, run: *run, timeout: *timeout, require: splitList(*require), events: *events}
	if *native {
		if options.run != "" || len(options.require) != 0 || len(set.Args()) != 0 {
			return fmt.Errorf("%w: -native-progress has a fixed mandatory test selection", errGate)
		}

		return runNativeProgress(ctx, root, options)
	}

	stage, err := prepareForeignTests(ctx, root)
	if err != nil {
		return err
	}

	defer func() { result = errors.Join(result, stage.cleanup()) }()

	options.stage = stage

	patterns, err := stage.testSelection(ctx, root, set.Args(), options)
	if err != nil {
		return err
	}

	withTests, err := stagedPackagesWithTests(ctx, root, patterns, options)
	if err != nil {
		return err
	}

	outcome, evidence, runErr := executeWithEvidence(ctx, root, patterns, options)
	if outcome == nil {
		return runErr
	}

	judged := errors.Join(
		judgeTests(outcome, withTests, options, runErr),
		stage.verify(ctx, root),
		stage.compilerEquivalent(ctx, root, patterns, options),
	)
	judged = errors.Join(judged, rejectForeignSkips(outcome, stage.sources))
	evidence.finish(judged)

	return judged
}

// executeWithEvidence runs go test -json and collects its events.
func executeWithEvidence(ctx context.Context, root string, patterns []string, options testOptions) (*testOutcome, *testEvidence, error) {
	evidence, err := openTestEvidence(options.events)
	if err != nil {
		return nil, nil, err
	}

	testArgs := []string{testVerb, jsonFlag, countOnce, "-timeout", options.timeout}
	if options.stage != nil {
		testArgs = append(testArgs, options.stage.flags(options)...)
	} else if options.race {
		testArgs = append(testArgs, foreignRaceFlag)
	}

	if options.run != "" {
		testArgs = append(testArgs, runSelectionFlag, options.run)
	}

	run := &command{dir: root, name: goTool, args: append(testArgs, patterns...), env: []string{readonlyGoFlags, foreignWorkspaceOff}}
	if options.stage != nil {
		run.env = options.stage.environment()
	}

	outcome, runErr := collectTestEvents(ctx, run, evidence)

	return outcome, evidence, errors.Join(runErr, evidence.closeFiles())
}

// collectTestEvents streams complete evidence while validating outcomes without retaining output in RAM.
func collectTestEvents(ctx context.Context, run *command, evidence *testEvidence) (*testOutcome, error) {
	outcome := newTestOutcome()
	pipeReader, pipeWriter := io.Pipe()
	done := make(chan struct{})

	go func() { outcome.consume(pipeReader); close(done) }()

	run.stdout = io.MultiWriter(evidence.stdout, pipeWriter)
	run.stderr = io.MultiWriter(evidence.stderr, log.Writer())
	runErr := run.run(ctx)

	closeLogged(pipeWriter)
	<-done
	closeLogged(pipeReader)

	return outcome, runErr
}

func splitList(value string) []string {
	var items []string

	for item := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}

	return items
}

func judgeTests(outcome *testOutcome, withTests []string, options testOptions, runErr error) error {
	problems := slices.Clone(outcome.streamErrors)
	for key := range outcome.running {
		problems = append(problems, "test event stream ended before completion: "+key)
	}

	selection, total := testSelectionProblems(outcome, withTests, options)
	problems = append(problems, selection...)

	if runErr != nil && len(problems) == 0 {
		problems = append(problems, runErr.Error())
	}

	log.Printf("test: %d packages with tests, %d top-level tests passed, %d subtests passed, %d skipped",
		len(withTests), total, outcome.subtests, len(outcome.skipped))

	for _, name := range slices.Sorted(slices.Values(outcome.skipped)) {
		log.Printf("test: skipped %s: %s", name, outcome.reasons[name])
	}

	return report(testVerb, problems, fmt.Sprintf("all %d tests passed", total))
}

func testSelectionProblems(outcome *testOutcome, withTests []string, options testOptions) ([]string, int) {
	var problems []string

	total := 0

	for _, pkg := range withTests {
		total += len(outcome.passed[pkg])

		if outcome.packages[pkg] != actionPass {
			problems = append(problems, fmt.Sprintf("package %s did not pass (%q)", pkg, outcome.packages[pkg]))
		}

		if options.run == "" && len(outcome.passed[pkg]) == 0 {
			problems = append(problems, "package "+pkg+" has test files but no test passed: discovery failed or every test skipped")
		}
	}

	for _, name := range options.require {
		if !requiredPassed(outcome, name) {
			problems = append(problems, "required test "+name+" did not pass; it was not discovered or it skipped")
		}
	}

	for _, name := range outcome.failed {
		problems = append(problems, "test failed: "+name)
	}

	if total == 0 {
		problems = append(problems, "zero tests passed; selection was empty or every selected test skipped")
	}

	if len(withTests) == 0 {
		problems = append(problems, "no package with test files was found")
	}

	return problems, total
}

func requiredPassed(outcome *testOutcome, name string) bool {
	matches := 0
	passed := false

	for key, status := range outcome.completed {
		pkg, test, _ := strings.Cut(key, " ")
		if test == name || pkg+"::"+test == name {
			matches++
			passed = status == actionPass
		}
	}

	return matches == 1 && passed
}

// discoverFuzzTargets lists every Fuzz function with `go test -list`, which also compiles the test
// binaries, so a package that does not build fails discovery instead of vanishing from it.
func discoverFuzzTargets(ctx context.Context, root string, patterns []string) ([]fuzzTarget, error) {
	output, err := goCommand(root, append([]string{testVerb, "-list", "^Fuzz"}, patterns...)...).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("go test -list: %w", err)
	}

	var (
		targets []fuzzTarget
		pending []string
	)

	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)

		switch {
		case len(fields) == 1 && strings.HasPrefix(fields[0], "Fuzz"):
			pending = append(pending, fields[0])
		case len(fields) >= minOKFields && fields[0] == "ok":
			for _, name := range pending {
				targets = append(targets, fuzzTarget{pkg: fields[1], name: name})
			}

			pending = nil
		default:
		}
	}

	return targets, nil
}

// runFuzz discovers every fuzz target and fuzzes each for a bounded time. It fails when none is
// found, so a rename or a build-tag mistake cannot turn the job into a silent pass.
func runFuzz(ctx context.Context, args []string) error {
	set := newFlags(fuzzCommand)
	duration := set.String("time", "30s", "fuzzing time per target, as accepted by go test -fuzztime")

	err := set.Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	if budgetErr := validateFuzzBudget(*duration); budgetErr != nil {
		return budgetErr
	}

	if platformErr := requireFuzzPlatform(); platformErr != nil {
		return platformErr
	}

	patterns := set.Args()
	if len(patterns) == 0 {
		patterns = []string{allPackages}
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	targets, err := discoverFuzzTargets(ctx, root, patterns)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		return fmt.Errorf("%w: no Fuzz function found in %v; refusing to pass with zero targets", errGate, patterns)
	}

	log.Printf("fuzz: %d targets, %s each", len(targets), *duration)

	var problems []string

	for _, target := range targets {
		if contextErr := ctx.Err(); contextErr != nil {
			problems = append(problems, contextErr.Error())
			break
		}

		log.Printf("fuzz: %s %s", target.pkg, target.name)

		if fuzzErr := runFuzzTarget(ctx, root, target, *duration); fuzzErr != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", target.pkg, target.name, fuzzErr))
		}
	}

	return report(fuzzCommand, problems, fmt.Sprintf("%d targets fuzzed for %s each", len(targets), *duration))
}

// validateFuzzBudget rejects invalid budgets before compiling target discovery.
func validateFuzzBudget(value string) error {
	if before, ok := strings.CutSuffix(value, "x"); ok {
		count, err := strconv.ParseInt(before, 10, 0)
		if err == nil && count > 0 {
			return nil
		}
	} else if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return nil
	}

	return fmt.Errorf("%w: fuzz time must be a positive duration or iteration count, got %q", errGate, value)
}

// runFuzzTarget requires the selected target's real lifecycle to pass; skipped or missing targets are failures.
func runFuzzTarget(ctx context.Context, root string, target fuzzTarget, duration string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fuzz target canceled: %w", err)
	}

	evidence, err := openTestEvidence("")
	if err != nil {
		return err
	}

	run := &command{
		dir:           root,
		name:          goTool,
		fuzzLifecycle: true,
		args: []string{
			testVerb, jsonFlag, runSelectionFlag, "^$", "-fuzz", "^" + target.name + "$", "-fuzztime", duration, target.pkg,
		},
	}
	outcome, runErr := collectTestEvents(ctx, run, evidence)
	judged := judgeTests(
		outcome,
		[]string{target.pkg},
		testOptions{run: "^$", require: []string{target.name}},
		errors.Join(runErr, evidence.closeFiles()),
	)
	evidence.finish(judged)

	return errors.Join(judged, runErr)
}

// recordLifecycle verifies each terminal event against its preceding start/run event.
// record holds the mutex while calling it.
func (o *testOutcome) recordLifecycle(event *testEvent, key string) {
	if event.Action == actionStart {
		o.started[event.Package] = true
	}

	if event.Action == actionRun {
		o.running[key] = true
		o.seen[key] = true
	}

	if slices.Contains([]string{actionPass, actionFail, actionSkip}, event.Action) {
		if !o.started[event.Package] {
			o.streamErrors = append(o.streamErrors, "test package result lacks start event: "+event.Package)
		}

		if event.Test != "" && !o.seen[key] {
			o.streamErrors = append(o.streamErrors, "test result lacks run event: "+key)
		}

		delete(o.running, key)

		if event.Test != "" {
			o.completed[key] = event.Action
		}
	}
}
