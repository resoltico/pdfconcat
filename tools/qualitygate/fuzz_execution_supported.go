// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// minOKFields is the fewest fields of a `go test` package result line: "ok" and the package.
const minOKFields = 2

func runPlatformFuzz(ctx context.Context, patterns []string, duration string) error {
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

	log.Printf("fuzz: %d targets, %s each", len(targets), duration)

	var problems []string

	for _, target := range targets {
		if contextErr := ctx.Err(); contextErr != nil {
			problems = append(problems, contextErr.Error())
			break
		}

		log.Printf("fuzz: %s %s", target.pkg, target.name)

		if fuzzErr := runFuzzTarget(ctx, root, target, duration); fuzzErr != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", target.pkg, target.name, fuzzErr))
		}
	}

	return report(fuzzCommand, problems, fmt.Sprintf("%d targets fuzzed for %s each", len(targets), duration))
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
