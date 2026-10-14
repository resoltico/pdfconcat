// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
)

func runPlatformFuzz(ctx context.Context, patterns []string, duration string) (result error) {
	if len(patterns) == 0 {
		patterns = []string{allPackages}
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	storage, err := newFuzzStorage(root)
	if err != nil {
		return err
	}
	defer func() { result = storage.finish(result) }()
	workers := runtime.GOMAXPROCS(0)
	watch := newFuzzStorageWatch(ctx, storage, workers)
	defer func() { result = errors.Join(result, watch.finish(result)) }()
	ctx = watch.ctx

	targets, err := discoverFuzzTargets(ctx, root, patterns, storage.environment())
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		return fmt.Errorf("%w: no Fuzz function found in %v; refusing to pass with zero targets", errGate, patterns)
	}

	module, err := modulePath(root)
	if err != nil {
		return err
	}
	if err = watch.watchCorpus(root, module, targets); err != nil {
		return fmt.Errorf("%w: inspect source fuzz corpus: %w", errGate, err)
	}
	if err = watch.observeJoined(); err != nil {
		return err
	}
	log.Printf("fuzz: %d targets, %s each; %d workers, owned storage threshold %d bytes/%d entries, new source corpus growth %d bytes", len(targets), duration, workers, watch.limit.bytes, watch.limit.entries, fuzzCorpusGrowthBytes)

	var problems []string
	var cleanupUncertainty error

	for _, target := range targets {
		if contextErr := ctx.Err(); contextErr != nil {
			problems = append(problems, contextErr.Error())
			break
		}

		log.Printf("fuzz: %s %s", target.pkg, target.name)

		if fuzzErr := runFuzzTarget(ctx, root, target, duration, storage.environment(), workers, watch.observeJoined); fuzzErr != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", target.pkg, target.name, fuzzErr))
			if errors.Is(fuzzErr, errFuzzCleanupUncertain) {
				cleanupUncertainty = errFuzzCleanupUncertain
				break
			}
		}
	}

	return errors.Join(report(fuzzCommand, problems, fmt.Sprintf("%d targets fuzzed for %s each", len(targets), duration)), cleanupUncertainty)
}

// discoverFuzzTargets lists every Fuzz function with `go test -list`, which also compiles the test
// binaries, so a package that does not build fails discovery instead of vanishing from it.
func discoverFuzzTargets(ctx context.Context, root string, patterns, environment []string) ([]fuzzTarget, error) {
	compiled, err := compilerFuzzManifest(ctx, root, patterns, environment)
	if err != nil {
		return nil, err
	}
	run := goCommand(root, append([]string{testVerb, jsonFlag, "-list", "^Fuzz"}, patterns...)...)
	// Listing runs package init and TestMain, so discovery owns their descendants too.
	run.fuzzLifecycle = true
	run.env = environment
	output, err := run.output(ctx)
	if err != nil {
		return nil, fmt.Errorf("go test -list: %w", err)
	}

	listed, err := parseFuzzDiscovery(strings.NewReader(output))
	if err != nil {
		return nil, err
	}
	if err = compareFuzzManifest(listed, compiled); err != nil {
		return nil, err
	}
	return listed, nil
}

// parseFuzzDiscovery uses Go's package attribution rather than status text printed
// by a test binary. Listed names still require an actual passing fuzz lifecycle.
func parseFuzzDiscovery(reader io.Reader) ([]fuzzTarget, error) {
	decoder := json.NewDecoder(reader)
	outcome := newTestOutcome()
	seen := map[fuzzTarget]bool{}
	var targets []fuzzTarget

	for {
		var event testEvent
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("%w: invalid fuzz discovery event: %w", errGate, err)
		}
		outcome.record(&event)
		if event.Action != "output" {
			continue
		}

		for line := range strings.SplitSeq(event.Output, "\n") {
			fields := strings.Fields(line)
			if len(fields) != 1 || !strings.HasPrefix(fields[0], "Fuzz") {
				continue
			}
			if event.Package == "" {
				return nil, fmt.Errorf("%w: fuzz discovery target lacks package attribution", errGate)
			}
			target := fuzzTarget{pkg: event.Package, name: fields[0]}
			if !seen[target] {
				targets = append(targets, target)
				seen[target] = true
			}
		}
	}

	if len(outcome.streamErrors) != 0 {
		return nil, fmt.Errorf("%w: fuzz discovery lifecycle: %s", errGate, strings.Join(outcome.streamErrors, "; "))
	}
	for pkg := range outcome.started {
		if outcome.packages[pkg] != actionPass && outcome.packages[pkg] != actionSkip {
			return nil, fmt.Errorf("%w: fuzz discovery package %s did not complete successfully", errGate, pkg)
		}
	}
	for _, target := range targets {
		if !outcome.started[target.pkg] || outcome.packages[target.pkg] != actionPass {
			return nil, fmt.Errorf("%w: fuzz discovery target %s %s lacks a passing package lifecycle", errGate, target.pkg, target.name)
		}
	}

	return targets, nil
}
